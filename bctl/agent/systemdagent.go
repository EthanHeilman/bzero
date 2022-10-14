package main

// import (
// 	"fmt"
// 	"io"
// 	"net/http"
// 	"net/url"
// 	"os"
// 	"runtime"
// 	"time"

// 	"bastionzero.com/bctl/v1/bctl/agent/controlchannel"
// 	"bastionzero.com/bctl/v1/bctl/agent/controlchannel/agentidentity"
// 	"bastionzero.com/bctl/v1/bctl/agent/controlconnection"
// 	"bastionzero.com/bctl/v1/bctl/agent/registration"
// 	"bastionzero.com/bctl/v1/bzerolib/connection"
// 	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
// 	"bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
// 	"bastionzero.com/bctl/v1/bzerolib/keypair"
// 	"bastionzero.com/bctl/v1/bzerolib/logger"
// 	"bastionzero.com/bctl/v1/bzerolib/report"
// 	"github.com/google/uuid"
// 	"gopkg.in/tomb.v2"
// )

// const (
// 	defaultLogFilePath = "/var/log/bzero/bzero-agent.log"
// )

// type SystemDConfig interface {
// 	GetTargetId() string
// 	GetPrivateKey() *keypair.PrivateKey
// 	GetPublicKey() *keypair.PublicKey
// 	GetIdpOrgId() string
// 	GetIdpProvider() string
// 	GetShutdownInfo() (string, map[string]string)
// 	GetAgentIdentityToken() string

// 	SetVersion(version string) error
// 	SetShutdownInfo(reason string, state map[string]string) error
// 	SetAgentIdentityToken(string) error

// 	WaitForRegistration(cancel <-chan struct{}) error
// 	Reload() error
// }

// type SystemDAgent struct {
// 	logger *logger.Logger
// 	tmb    tomb.Tomb

// 	serviceUrl string
// 	version    string

// 	registration   registration.IRegistration
// 	config         SystemDConfig
// 	controlConn    connection.Connection
// 	controlChannel *controlchannel.ControlChannel
// 	shutdownChan   <-chan os.Signal
// }

// func NewSystemDAgent(
// 	version string,
// 	serviceUrl string,
// 	config SystemDConfig,
// 	registration registration.IRegistration,
// 	shutdownChan <-chan os.Signal,
// ) (*SystemDAgent, error) {
// 	// Make sure our agent version is correct
// 	if err := config.SetVersion(version); err != nil {
// 		return nil, err
// 	}

// 	// Create our logger
// 	log, err := logger.New(&logger.Config{
// 		ConsoleWriters: []io.Writer{os.Stdout},
// 		FilePath:       defaultLogFilePath,
// 	})
// 	if err != nil {
// 		return nil, err
// 	}
// 	log.AddAgentVersion(version)
// 	log.AddAgentType(Bzero)

// 	return &SystemDAgent{
// 		logger:       log,
// 		serviceUrl:   serviceUrl,
// 		version:      version,
// 		config:       config,
// 		shutdownChan: shutdownChan,
// 	}, nil
// }

// func (s *SystemDAgent) Run(forceReRegistration bool) error {
// 	// Report shutdown if qualified
// 	s.reportRestart()

// 	s.tmb.Go(func() error {
// 		select {
// 		case <-s.tmb.Dying():
// 			return nil
// 		case signal := <-s.shutdownChan:
// 			return fmt.Errorf("received shutdown signal: %s", signal.String())
// 		}
// 	})

// 	// If this is an agent run by systemd, we add the -w (wait) flag
// 	// which means that this process will wait until it detects a new
// 	// registration and then it we load it before proceeding
// 	isRegistered := !s.config.GetPublicKey().IsEmpty()
// 	if !isRegistered && wait {
// 		s.config.WaitForRegistration(s.tmb.Dying())

// 		// Now that we're registered, we need to reload our config to make sure it's up-to-date
// 		if err := s.config.Reload(); err != nil {
// 			return err
// 		}
// 	} else if !isRegistered || forceReRegistration {
// 		if err := s.registration.Register(s.logger); err != nil {
// 			return err
// 		}

// 		// If we registered, then we're done here. Registration is designed to
// 		// be essentially a command and not fully start up the agent
// 		return nil
// 	} else {
// 		s.logger.Infof("BastionZero Agent is already registered with %s", serviceUrl)
// 	}

// 	// Connect the control channel to BastionZero
// 	s.logger.Info("Creating connection to BastionZero...")
// 	if err := s.startControlChannel(); err != nil {
// 		return err
// 	}

// 	return nil
// }

// func (a *SystemDAgent) startControlChannel() error {
// 	targetId := a.config.GetTargetId()
// 	privateKey := a.config.GetPrivateKey()

// 	aipLogger := a.logger.GetComponentLogger("AgentIdentityProvider")
// 	agentIdentityProvider := agentidentity.New(
// 		aipLogger,
// 		a.serviceUrl,
// 		targetId,
// 		a.config,
// 		privateKey,
// 	)

// 	headers := http.Header{}
// 	params := url.Values{
// 		"public_key": {a.config.GetPublicKey().String()},
// 		"version":    {a.version},
// 		"target_id":  {targetId},
// 		"agent_type": {Bzero},
// 	}

// 	// Setup our loggers
// 	ccId := uuid.New().String()
// 	ccLogger := a.logger.GetControlChannelLogger(ccId)
// 	connLogger := ccLogger.GetConnectionLogger("controlchannel")
// 	wsLogger := ccLogger.GetComponentLogger("Websocket")
// 	srLogger := ccLogger.GetComponentLogger("SignalR")

// 	// Make our connection
// 	client := signalr.New(srLogger, websocket.New(wsLogger))

// 	// Create our control channel's connection to BastionZero
// 	if conn, err := controlconnection.New(connLogger, serviceUrl, a.config.GetPrivateKey(), params, headers, client, agentIdentityProvider); err != nil {
// 		return err
// 	} else {
// 		// Start up our control channel
// 		a.controlChannel, err = controlchannel.Start(ccLogger, ccId, conn, serviceUrl, Bzero, agentIdentityProvider, privateKey, a.config)
// 		return err
// 	}
// }

// func (s *SystemDAgent) reportRestart() {
// 	shutdownReason, shutdownState := s.config.GetShutdownInfo()

// 	if !report.IsReportable(shutdownReason) {
// 		return
// 	}

// 	s.logger.Infof("Notifying BastionZero that we restarted because: %s", shutdownReason)
// 	report.ReportRestart(
// 		s.logger,
// 		serviceUrl,
// 		report.RestartReport{
// 			TargetId:       targetId,
// 			AgentPublicKey: s.config.GetPublicKey().String(),
// 			Timestamp:      fmt.Sprint(time.Now().UTC().Unix()),
// 			Message:        shutdownReason,
// 			State:          shutdownState,
// 		})
// }

// func (s *SystemDAgent) Close(reason error) {
// 	s.logger.Infof("Agent closing because: %s", reason)

// 	if s.tmb.Alive() {
// 		s.tmb.Kill(reason)
// 	}

// 	// TODO: do this as part of monitorcontrolchannel
// 	// this is guaranteed to return within 10 seconds (see controlchannel.go:closeTimeout)
// 	// if a.controlChannel != nil && a.isControlChannelAlive {
// 	// 	a.controlChannel.Close(reason)
// 	// }

// 	if s.controlConn != nil {
// 		s.controlConn.Close(reason, 10*time.Second)
// 	}

// 	s.config.SetShutdownInfo(reason.Error(), s.State())

// 	if reason == nil {
// 		os.Exit(0)
// 	}

// 	os.Exit(1)
// }

// func (s *SystemDAgent) State() map[string]string {
// 	hostname, err := os.Hostname()
// 	if err != nil {
// 		hostname = ""
// 	}

// 	return map[string]string{
// 		"activationToken":       activationToken,
// 		"registrationKeyLength": fmt.Sprintf("%v", len(registrationKey)),
// 		"targetName":            targetName,
// 		"targetHostName":        hostname,
// 		"goos":                  runtime.GOOS,
// 		"goarch":                runtime.GOARCH,
// 	}
// }
