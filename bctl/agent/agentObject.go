package main

import (
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
	"bastionzero.com/bctl/v1/bctl/agent/registration"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bctl/v1/bzerolib/keypair"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/report"
	"github.com/google/uuid"
	"gopkg.in/tomb.v2"
)

type Config interface {
	GetTargetId() string
	GetPrivateKey() *keypair.PrivateKey
	GetPublicKey() *keypair.PublicKey
	GetIdpOrgId() string
	GetIdpProvider() string
	GetShutdownInfo() (string, map[string]string)
	GetAgentIdentityToken() string

	SetVersion(version string) error
	SetShutdownInfo(reason string, state map[string]string) error
	SetAgentIdentityToken(string) error
	SetRegistrationData(serviceUrl string, publickey keypair.PublicKey, privateKey keypair.PrivateKey, idpProvider string, idpOrgId string, targetId string) error
}

type Agent struct {
	tmb    tomb.Tomb
	logger *logger.Logger
	config Config

	agentType string

	signalChan        <-chan os.Signal
	agentShutdownChan chan error
	registration      registration.IRegistration

	controlConn    connection.Connection
	controlChannel *controlchannel.ControlChannel
	serviceUrl     string
	version        string
}

func (a *Agent) Run(forceReRegistration bool) error {
	// Make sure our agent version is correct
	if err := a.config.SetVersion(a.version); err != nil {
		return err
	}

	// Register if we aren't already
	isRegistered := !a.config.GetPublicKey().IsEmpty()
	if !isRegistered || forceReRegistration {
		if err := a.registration.Register(a.logger, a.config); err != nil {
			return err
		}

		// If we registered, then we're done here. Registration is designed to
		// be essentially a command and not fully start up the agent
		return nil
	} else {
		a.logger.Infof("BastionZero Agent is already registered with %s", serviceUrl)
	}

	// Connect the control channel to BastionZero
	a.logger.Info("Creating connection to BastionZero...")
	if err := a.startControlChannel(); err != nil {
		return err
	}

	go a.monitorControlChannel()

	for {
		select {
		// wait until we recieve a kill signal or other runtime shutdown
		case signal := <-a.signalChan:
			return fmt.Errorf("received shutdown signal: %s", signal.String())
		// we should report significant-but-non-fatal errors to bastion.
		// this action must be separated from monitorControlChannel so that persistent runtime errors do not
		// prevent the agent from restarting when it stops detecting pings from bastion
		case runtimeErr := <-a.controlChannel.RuntimeErr():
			a.reportError(runtimeErr)
		case err := <-a.agentShutdownChan:
			return err
		}
	}
}

func (a *Agent) startControlChannel() error {
	targetId := a.config.GetTargetId()
	privateKey := a.config.GetPrivateKey()

	aipLogger := a.logger.GetComponentLogger("AgentIdentityProvider")
	agentIdentityProvider := agentidentity.New(
		aipLogger,
		a.serviceUrl,
		targetId,
		a.config,
		privateKey,
	)

	headers := http.Header{}
	params := url.Values{
		"public_key": {a.config.GetPublicKey().String()},
		"version":    {a.version},
		"target_id":  {targetId},
		"agent_type": {Bzero},
	}

	// Setup our loggers
	ccId := uuid.New().String()
	ccLogger := a.logger.GetControlChannelLogger(ccId)
	connLogger := ccLogger.GetConnectionLogger("controlchannel")
	wsLogger := ccLogger.GetComponentLogger("Websocket")
	srLogger := ccLogger.GetComponentLogger("SignalR")

	// Make our connection
	client := signalr.New(srLogger, websocket.New(wsLogger))

	// Create our control channel's connection to BastionZero
	if conn, err := controlconnection.New(connLogger, serviceUrl, privateKey, params, headers, client, agentIdentityProvider); err != nil {
		return err
	} else {
		// Start up our control channel
		a.controlChannel, err = controlchannel.Start(ccLogger, ccId, conn, serviceUrl, Bzero, agentIdentityProvider, privateKey, a.config)
		a.controlConn = conn

		return err
	}
}

func (a *Agent) Close(reason error) {
	a.logger.Infof("Agent closing because: %s", reason)

	if a.tmb.Alive() {
		a.tmb.Kill(reason)
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
func (a *Agent) reportError(errorReport error) {
	a.logger.Error(errorReport)

	errReport := report.ErrorReport{
		Reporter:  fmt.Sprintf("%s-agent-%s", getAgentType(), getAgentVersion()),
		Timestamp: fmt.Sprint(time.Now().UTC().Unix()),
		Message:   errorReport.Error(),
		State:     a.state(),
	}

	report.ReportError(a.logger, serviceUrl, errReport)
}

// check whether we're restarting after a qualifying event, and thus need to tell Bastion about it
func (a *Agent) checkShutdownReason() {
	shutdownReason, shutdownState := a.config.GetShutdownInfo()

	if shutdownReason == stoppedProcessingPongsMsg || strings.Contains(shutdownReason, controlchannel.ManualRestartMsg) {
		a.logger.Infof("Notifying Bastion that we restarted because: %s", shutdownReason)
		report.ReportRestart(
			a.logger,
			serviceUrl,
			report.RestartReport{
				TargetId:       a.config.GetTargetId(),
				AgentPublicKey: a.config.GetPublicKey().String(),
				Timestamp:      fmt.Sprint(time.Now().UTC().Unix()),
				Message:        shutdownReason,
				State:          shutdownState,
			})
	}
}

func (a *Agent) monitorControlChannel() {
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
				a.agentShutdownChan <- fmt.Errorf(stoppedProcessingPongsMsg)
				return
			}
		case <-a.controlChannel.Done():
			// if the CC is reporting done, its websocket is probably dead, or some other fatal error occurred
			a.logger.Errorf("control channel closed with error: %s -- Initializing restart...", a.controlChannel.Err())
			a.agentShutdownChan <- fmt.Errorf("control channel closed with error: %s", a.controlChannel.Err())
			return
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
