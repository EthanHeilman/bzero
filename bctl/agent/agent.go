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

	"github.com/google/uuid"
	"gopkg.in/tomb.v2"

	"bastionzero.com/agent/agenttype"
	"bastionzero.com/agent/bastion"
	"bastionzero.com/agent/bastion/agentidentity"
	"bastionzero.com/agent/controlchannel"
	"bastionzero.com/agent/controlconnection"
	"bastionzero.com/agent/registration"
	"bastionzero.com/bzerolib/connection"
	"bastionzero.com/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bzerolib/logger"
)

const (
	// there's nothing magical about the number 3 but we need to guarantee
	// that the timeout is significantly larger than than the heartrate to avoid a race between receiving and reporting a pong
	// as of this writing, this means an expected pong every minute, with a "disconnect" reported after 3 minutes
	bastionDisconnectTimeout  = 3 * controlchannel.HeartRate
	stoppedProcessingPongsMsg = "control channel stopped processing pongs"
)

type AgentConfig interface {
	controlchannel.ControlChannelConfig
	registration.RegistrationConfig
	agentidentity.AgentIdentityTokenConfig

	GetTargetId() string
	GetShutdownInfo() (string, map[string]string)
	GetServiceUrl() string

	SetVersion(version string) error
	SetShutdownInfo(reason string, state map[string]string) error

	Reload() error
}

type Agent struct {
	tmb    tomb.Tomb
	logger *logger.Logger

	agentConfig    AgentConfig
	keyShardConfig controlchannel.KeyShardConfig

	agentType agenttype.AgentType
	version   string

	osSignalChan <-chan os.Signal

	ctx    context.Context
	cancel context.CancelFunc

	controlConn    connection.Connection
	controlChannel *controlchannel.ControlChannel

	bastionClient bastion.ApiClient
}

func (a *Agent) Done() <-chan struct{} {
	return a.tmb.Dead()
}

func (a *Agent) Err() error {
	return a.tmb.Err()
}

func (a *Agent) Run() (err error) {
	defer func() {
		if err != nil {
			a.logger.Error(err)
			a.reportError(err)
		}
	}()

	a.logger.Info("Agent initialization complete")

	// Report any qualified restarts
	go a.reportQualifiedShutdown()

	// Make sure our agent version is up-to-date
	if err = a.agentConfig.SetVersion(a.version); err != nil {
		return
	}

	// Connect the control channel to BastionZero
	a.logger.Info("Creating connection to BastionZero...")
	if err = a.startControlChannel(); err != nil {
		return err
	}

	a.tmb.Go(a.monitorControlChannel)

	// We want to elegantly die from any return statement below
	defer func() {
		// Keep this in this func so that the below err isn't evaluated until
		// the defer statement is called
		a.Close(err)
	}()

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
	targetId := a.agentConfig.GetTargetId()
	privateKey := a.agentConfig.GetPrivateKey()
	serviceUrl := a.agentConfig.GetServiceUrl()

	aipLogger := a.logger.GetComponentLogger("AgentIdentityToken")
	agentIdProvider := agentidentity.New(
		aipLogger,
		serviceUrl,
		a.agentConfig,
	)

	bcLogger := a.logger.GetComponentLogger("Bastion")
	a.bastionClient = bastion.New(bcLogger, serviceUrl, agentIdProvider, a.version)

	ccId := uuid.New().String()
	ccLogger := a.logger.GetControlChannelLogger(ccId)
	wsLogger := ccLogger.GetComponentLogger("Websocket")
	srLogger := ccLogger.GetComponentLogger("SignalR")

	// Make our connection
	client := signalr.New(srLogger, websocket.New(wsLogger))

	headers := http.Header{}
	params := url.Values{
		"public_key": {a.agentConfig.GetPublicKey().String()},
		"version":    {a.version},
		"target_id":  {targetId},
		"agent_type": {string(a.agentType)},
	}

	// Create our control channel's connection to BastionZero
	conn, err := controlconnection.New(ccLogger, serviceUrl, privateKey, params, headers, client, agentIdProvider)
	if err != nil {
		return err
	}

	// Start up our control channel
	a.controlChannel, err = controlchannel.Start(ccLogger, a.bastionClient, ccId, conn, a.agentType, agentIdProvider, privateKey, a.agentConfig, a.keyShardConfig, defaultLogPath)
	a.controlConn = conn

	return err
}

func (a *Agent) Close(reason error) {
	a.logger.Infof("Agent is shutting down: %s", reason)

	if a.tmb.Alive() {
		a.tmb.Kill(reason)
		a.tmb.Wait()
	}

	if a.controlConn != nil {
		a.controlConn.Close(reason, 10*time.Second)
	}

	if reason == nil {
		return
	}

	a.agentConfig.SetShutdownInfo(reason.Error(), a.state())
}

// report early errors to the bastion so we have greater visibility
func (a *Agent) reportError(reason error) {
	// If we passed in the Agent's context here, we would have to instantly cancel this.
	// We want to give this code a fair chance of reporting our error
	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()

	if err := a.bastionClient.ReportError(ctx, fmt.Sprintf("%s-agent", a.agentType), reason, a.state()); err != nil {
		a.logger.Error(err)
	}
}

// check whether we're restarting after a qualifying event, and thus need to tell Bastion about it
func (a *Agent) reportQualifiedShutdown() {
	shutdownReason, shutdownState := a.agentConfig.GetShutdownInfo()

	if shutdownReason == stoppedProcessingPongsMsg || strings.Contains(shutdownReason, controlchannel.ManualRestartMsg) {
		a.logger.Infof("Notifying Bastion that we restarted because: %s", shutdownReason)

		if err := a.bastionClient.ReportRestart(a.ctx, a.agentConfig.GetTargetId(), a.agentConfig.GetPublicKey().String(), shutdownReason, shutdownState); err != nil {
			a.logger.Errorf("failed to report restart: %s", err)
		}
	}
}

func (a *Agent) monitorControlChannel() error {
	maximumMissedPongSets := int(controlconnection.MaximumReconnectWaitTime / bastionDisconnectTimeout)
	missedPongSets := 0

	for {
		select {
		case <-a.tmb.Dying():
			a.controlChannel.Close(a.tmb.Err())
			return nil
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
				a.controlChannel.Close(fmt.Errorf(stoppedProcessingPongsMsg))
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
		"version":        a.version,
		"targetType":     string(a.agentType),
		"targetId":       a.agentConfig.GetTargetId(),
		"targetHostName": hostname,
		"goos":           runtime.GOOS,
		"goarch":         runtime.GOARCH,
	}
}
