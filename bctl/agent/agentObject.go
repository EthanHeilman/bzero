package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/controlchannel"
	"bastionzero.com/bctl/v1/bctl/agent/controlchannel/agentidentity"
	"bastionzero.com/bctl/v1/bctl/agent/controlconnection"
	"bastionzero.com/bctl/v1/bctl/agent/keysplitting"
	"bastionzero.com/bctl/v1/bctl/agent/registration"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/messagesigner"
	"bastionzero.com/bctl/v1/bzerolib/report"
	"github.com/google/uuid"
	"gopkg.in/tomb.v2"
)

const (
	// there's nothing magical about the number 3 but we need to guarantee
	// that the timeout is significantly larger than than the heartrate to avoid a race between receiving and reporting a pong
	// as of this writing, this means an expected pong every minute, with a "disconnect" reported after 3 minutes
	bastionDisconnectTimeout  = 3 * controlchannel.HeartRate
	stoppedProcessingPongsMsg = "control channel stopped processing pongs"
)

type IRegistration interface {
	Register(logger *logger.Logger, config registration.RegistrationConfig) error
}

type Config interface {
	keysplitting.IKeysplittingConfig
	registration.RegistrationConfig
	agentidentity.IAgentIdentityTokenStore

	GetTargetId() string
	GetShutdownInfo() (string, map[string]string)
	GetServiceUrl() string
	GetMessageSigner() (*messagesigner.MessageSigner, error)

	SetVersion(version string) error
	SetShutdownInfo(reason string, state map[string]string) error
}

type Agent struct {
	tmb    tomb.Tomb
	ctx    context.Context
	cancel context.CancelFunc

	logger *logger.Logger
	config Config

	agentType string

	osSignalChan <-chan os.Signal
	registration IRegistration

	controlConn    connection.Connection
	controlChannel *controlchannel.ControlChannel
	version        string
}

func (a *Agent) Run(forceReRegistration bool) (err error) {
	defer func() {
		if err != nil {
			a.reportError(err)
		}
	}()

	a.logger.Info("Agent initialization complete")

	// Report any qualified restarts
	go a.reportQualifiedShutdown()

	// Register if we aren't already
	isRegistered := a.config.GetPublicKey() != ""
	if !isRegistered || forceReRegistration {
		a.logger.Info("Starting registration")

		// Regardless of the response, we're done here. Registration is designed to
		// essentially be a cli command and not fully start up the agent
		err = a.registration.Register(a.logger, a.config)
		return err
	} else {
		a.logger.Infof("BastionZero Agent is already registered with %s", a.config.GetServiceUrl())
	}

	// Make sure our agent version is correct
	if err = a.config.SetVersion(a.version); err != nil {
		return err
	}

	// Connect the control channel to BastionZero
	a.logger.Info("Creating connection to BastionZero...")
	if err = a.startControlChannel(); err != nil {
		return err
	}

	a.tmb.Go(a.monitorControlChannel)

	for {
		select {
		case <-a.tmb.Dead():
			return a.tmb.Err()

		// wait until we recieve a kill signal or other runtime shutdown
		case signal := <-a.osSignalChan:
			return fmt.Errorf("received shutdown signal: %s", signal.String())

		// we should report significant-but-non-fatal errors to bastion.
		// this action must be separated from monitorControlChannel so that persistent runtime errors do not
		// prevent the agent from restarting when it stops detecting pings from bastion
		case runtimeErr := <-a.controlChannel.RuntimeErr():
			a.reportError(runtimeErr)
		}
	}
}

func (a *Agent) startControlChannel() error {
	targetId := a.config.GetTargetId()
	privateKey := a.config.GetPrivateKey()
	serviceUrl := a.config.GetServiceUrl()

	aipLogger := a.logger.GetComponentLogger("AgentIdentityProvider")
	ms, err := a.config.GetMessageSigner()
	if err != nil {
		return err
	}

	agentIdentityProvider := agentidentity.New(
		aipLogger,
		serviceUrl,
		targetId,
		a.config,
		ms,
	)

	ccId := uuid.New().String()
	ccLogger := a.logger.GetControlChannelLogger(ccId)
	wsLogger := ccLogger.GetComponentLogger("Websocket")
	srLogger := ccLogger.GetComponentLogger("SignalR")

	// Make our connection
	client := signalr.New(srLogger, websocket.New(wsLogger))

	headers := http.Header{}
	params := url.Values{
		"public_key": {a.config.GetPublicKey()},
		"version":    {a.version},
		"target_id":  {targetId},
		"agent_type": {Bzero},
	}

	// Create our control channel's connection to BastionZero
	if conn, err := controlconnection.New(ccLogger, serviceUrl, privateKey, params, headers, client, agentIdentityProvider, ms); err != nil {
		return err
	} else {
		// Start up our control channel
		a.controlChannel, err = controlchannel.Start(ccLogger, ccId, conn, serviceUrl, Bzero, agentIdentityProvider, ms, a.config)
		a.controlConn = conn

		return err
	}
}

func (a *Agent) Close(reason error) {
	a.logger.Infof("Agent closing because: %s", reason)

	if a.tmb.Alive() {
		a.tmb.Kill(reason)
		a.tmb.Wait()
	}

	if a.controlConn != nil {
		a.controlConn.Close(reason, 10*time.Second)
	}

	a.config.SetShutdownInfo(reason.Error(), a.state())

	if reason == nil {
		os.Exit(0)
	}

	os.Exit(1)
}

// report early errors to the bastion so we have greater visibility
func (a *Agent) reportError(reason error) {
	a.logger.Error(reason)

	errReport := report.ErrorReport{
		Reporter:  fmt.Sprintf("%s-agent-%s", getAgentType(), getAgentVersion()),
		Timestamp: fmt.Sprint(time.Now().UTC().Unix()),
		Message:   reason.Error(),
		State:     a.state(),
	}

	report.ReportError(a.logger, a.ctx, a.config.GetServiceUrl(), errReport)
}

// check whether we're restarting after a qualifying event, and thus need to tell Bastion about it
func (a *Agent) reportQualifiedShutdown() {
	shutdownReason, shutdownState := a.config.GetShutdownInfo()

	if shutdownReason == stoppedProcessingPongsMsg || strings.Contains(shutdownReason, controlchannel.ManualRestartMsg) {
		a.logger.Infof("Notifying Bastion that we restarted because: %s", shutdownReason)

		report.ReportRestart(
			a.logger,
			a.ctx,
			a.config.GetServiceUrl(),
			report.RestartReport{
				TargetId:       a.config.GetTargetId(),
				AgentPublicKey: a.config.GetPublicKey(),
				Timestamp:      fmt.Sprint(time.Now().UTC().Unix()),
				Message:        shutdownReason,
				State:          shutdownState,
			})
	}
}

func (a *Agent) monitorControlChannel() error {
	maximumMissedPongSets := int(controlconnection.MaximumReconnectWaitTime / bastionDisconnectTimeout)
	missedPongSets := 0

	for {
		select {
		case <-a.tmb.Dying():
			a.controlChannel.Close(a.tmb.Err())
		case <-a.controlChannel.Pong():
			// the CC is still alive!
			missedPongSets = 0
		case <-time.After(bastionDisconnectTimeout):
			// If the CC knows it's not sending pongs, we should stop expecting them until it is back online or dead.
			// But if the maximum websocket backoff time has elapsed, assume we're stuck in a broken state and restart
			if !a.controlChannel.ShouldBeSendingPongs() && missedPongSets < maximumMissedPongSets {
				missedPongSets++
				a.logger.Errorf("Waiting for websocket to reconnect. Missed a set of pongs. (%d sets remaining before restarting)", maximumMissedPongSets-missedPongSets)
			} else {
				// if we don't hear from the CC but its websocket is still alive, assume the CC is broken and restart
				a.logger.Errorf("%s -- Initializing restart...", stoppedProcessingPongsMsg)
				a.controlChannel.Close(a.tmb.Err())
				return fmt.Errorf(stoppedProcessingPongsMsg)
			}
		case <-a.controlChannel.Done():
			// if the CC is reporting done, its websocket is probably dead, or some other fatal error occurred
			a.logger.Errorf("control channel closed with error: %s -- Initializing restart...", a.controlChannel.Err())
			return fmt.Errorf("control channel closed with error: %s", a.controlChannel.Err())
		}
	}
}

func (a *Agent) state() map[string]string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}

	return map[string]string{
		"activationToken":       activationToken,
		"registrationKeyLength": fmt.Sprintf("%v", len(registrationKey)),
		"targetName":            a.config.GetTargetId(),
		"targetHostName":        hostname,
		"goos":                  runtime.GOOS,
		"goarch":                runtime.GOARCH,
	}
}
