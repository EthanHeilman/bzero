package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"

	"bastionzero.com/bctl/v1/bctl/agent/controlchannel"
	"bastionzero.com/bctl/v1/bctl/agent/controlchannelconnection"
	"bastionzero.com/bctl/v1/bctl/agent/rbac"
	"bastionzero.com/bctl/v1/bctl/agent/registration"
	"bastionzero.com/bctl/v1/bctl/agent/vault"
	"bastionzero.com/bctl/v1/bzerolib/bzhttp"
	"bastionzero.com/bctl/v1/bzerolib/bzio"
	"bastionzero.com/bctl/v1/bzerolib/bzos"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/report"
)

var (
	serviceUrl, orgId                string
	environmentId, environmentName   string
	activationToken, registrationKey string
	idpProvider, namespace, idpOrgId string
	targetId, targetName, agentType  string
	logLevel                         string
	forceReRegistration              bool
	wait                             bool
	printVersion                     bool
	listLogFile                      bool
)

const (
	Cluster = "cluster"
	Bzero   = "bzero"

	prodServiceUrl = "https://cloud.bastionzero.com/"

	bzeroLogFilePath = "/var/log/bzero/bzero-agent.log"

	// based on convention from backend -- there's nothing magical about the number 6 but we need to guarantee
	// that the timeout is significantly larger than than the heartrate to avoid a race between receiving and reporting a pong
	// as of this writing, this means an expected pong every six seconds, with a "disconnect" reported after 2 minutes
	bastionDisconnectTimeout  = 6 * controlchannel.HeartRate
	stoppedProcessingPongsMsg = "control channel stopped processing pongs"
)

func main() {
	var err error
	var logger *logger.Logger
	fileIo := bzio.OsFileIo{}

	setAgentType()
	parseErr := parseFlags()

	if logger, err = setupLogger(); err != nil {
		reportError(logger, err)
	} else if parseErr != nil {
		// catch our parser errors now that we have a logger to print them
		reportError(logger, err)
	} else if printVersion {
		fmt.Printf("%s\n", getAgentVersion())
		return
	} else if listLogFile {
		switch agentType {
		case Bzero:
			fmt.Printf("%s\n", bzeroLogFilePath)
		case Cluster:
			fmt.Printf("BZero Agent logs can be accessed via the Kube API server by tailing the pods logs\n")
		}
		return
	} else {

		logger.Infof("BastionZero Agent version %s starting up...", getAgentVersion())

		var agent *Agent

		// Check if the agent is registered or not.  If not, generate signing keys,
		// check kube permissions and setup, and register with the Bastion.
		if err = handleRegistration(logger); err != nil {

			// our systemd agent waits for a successful new registration
			if wait {
				vault.WaitForNewRegistration(logger)
				logger.Infof("New registration detected. Loading registration information!")

				// double check and set our local variables
				var registered bool
				if registered, err = isRegistered(); err != nil {
					logger.Error(err)
				} else if registered {
					if agent, err = New(logger, fileIo); err != nil {
						reportError(logger, fmt.Errorf("failed to start agent: %s", err))
					} else {
						agent.Run()
					}
				}
			}
		} else {
			if agent, err = New(logger, fileIo); err != nil {
				reportError(logger, fmt.Errorf("failed to start agent: %s", err))
			} else {
				agent.Run()
			}
		}
	}

	switch agentType {
	case Cluster:

		// Sleep forever because otherwise kube will endlessly try restarting
		// Ref: https://stackoverflow.com/questions/36419054/go-projects-main-goroutine-sleep-forever
		// TODO: as soon as we have a "bastion breakup" message, we can safely get all agents to stop calling us
		// and so should have all agents exit
		select {}
	case Bzero:
		os.Exit(1)
	}
}

type Agent struct {
	config         *vault.Vault
	logger         *logger.Logger
	fileIo         bzio.BzFileIo
	conn           *controlchannelconnection.ControlChannelConnection
	controlChannel *controlchannel.ControlChannel

	agentShutdownChan chan error

	// prevents us from trying to close the CC after it has told us it's done
	isControlChannelAlive bool
}

func New(logger *logger.Logger, fileIo bzio.BzFileIo) (*Agent, error) {
	if config, err := vault.LoadVault(); err != nil {
		return nil, fmt.Errorf("failed to retrieve vault: %s", err)
	} else {

		// Check if the agent version has changed since the last time we saved
		// to the vault and update it if necessary
		currentVersion := getAgentVersion()
		if config.Data.Version != currentVersion {
			config.Data.Version = currentVersion

			if err := config.Save(); err != nil {
				return nil, fmt.Errorf("error saving updated version to vault: %w", err)
			}
		}

		agent := &Agent{
			config:            config,
			logger:            logger,
			fileIo:            fileIo,
			agentShutdownChan: make(chan error),
		}
		return agent, nil
	}
}

// check whether we're restarting after a qualifying event, and thus need to tell Bastion about it
func (a *Agent) checkShutdownReason() {
	if a.config.Data.ShutdownReason == stoppedProcessingPongsMsg || strings.Contains(a.config.Data.ShutdownReason, controlchannel.ManualRestartMsg) {
		a.logger.Infof("Notifying Bastion that we restarted because: %s", a.config.Data.ShutdownReason)
		report.ReportRestart(
			a.logger,
			serviceUrl,
			report.RestartReport{
				TargetId:       targetId,
				AgentPublicKey: a.config.Data.PublicKey,
				Timestamp:      fmt.Sprint(time.Now().Unix()),
				Message:        a.config.Data.ShutdownReason,
				State:          a.config.Data.ShutdownState,
			})
	}
}

func (a *Agent) Run() {

	go a.checkShutdownReason()

	var err error
	defer func() {
		// recover in case the agent panics
		if msg := recover(); msg != nil {
			reportError(a.logger, fmt.Errorf("bzero agent crashed with panic: %+v", msg))
			err = fmt.Errorf("crashed with panic: %+v", msg)
		}

		a.Close(err)
	}()

	// Connect the control channel to BastionZero
	a.logger.Info("Creating connection to BastionZero...")
	if err = a.startControlChannel(); err != nil {
		reportError(a.logger, err)
	} else {
		a.isControlChannelAlive = true
		go a.monitorControlChannel()

	mainLoop:
		for {
			select {
			// wait until we recieve a kill signal or other runtime shutdown
			case signal := <-bzos.OsShutdownChan():
				err = fmt.Errorf("received shutdown signal: %s", signal.String())
				break mainLoop
			// we should report significant-but-non-fatal errors to bastion.
			// this action must be separated from monitorControlChannel so that persistent runtime errors do not
			// prevent the agent from restarting when it stops detecting pings from bastion
			case runtimeErr := <-a.controlChannel.RuntimeErr():
				reportError(a.logger, runtimeErr)
			case err = <-a.agentShutdownChan:
				break mainLoop
			}
		}
	}
}

func setupLogger() (*logger.Logger, error) {
	config := logger.Config{
		ConsoleWriters: []io.Writer{os.Stdout},
	}

	// if this is systemd, output log to file
	if agentType == Bzero {
		config.FilePath = bzeroLogFilePath
	}

	log, err := logger.New(&config)
	if err == nil {
		log.AddAgentVersion(getAgentVersion())
	}

	return log, err
}

// report early errors to the bastion so we have greater visibility
func reportError(logger *logger.Logger, errorReport error) {
	if logger != nil {
		logger.Error(errorReport)
	} else {
		fmt.Println(errorReport.Error())
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}

	errReport := report.ErrorReport{
		Reporter:  "agent-" + getAgentVersion(),
		Timestamp: fmt.Sprint(time.Now().Unix()),
		Message:   errorReport.Error(),
		State: map[string]string{
			"activationToken":       activationToken,
			"registrationKeyLength": fmt.Sprintf("%v", len(registrationKey)),
			"targetName":            targetName,
			"targetHostName":        hostname,
			"goos":                  runtime.GOOS,
			"goarch":                runtime.GOARCH,
		},
	}

	report.ReportError(logger, serviceUrl, errReport)
}

func (a *Agent) startControlChannel() error {
	headers := http.Header{}
	params := url.Values{
		"public_key": {a.config.Data.PublicKey},
		"version":    {a.config.Data.Version},
		"target_id":  {a.config.Data.TargetId},
		"agent_type": {agentType},
	}

	// Setup our loggers
	ccId := uuid.New().String()
	ccLogger := a.logger.GetControlChannelLogger(ccId)
	connId := uuid.New().String()
	connLogger := ccLogger.GetConnectionLogger(connId)
	wsLogger := ccLogger.GetComponentLogger("Websocket")
	srLogger := ccLogger.GetComponentLogger("SignalR")

	// Make our connection
	client := signalr.New(srLogger, websocket.New(wsLogger))
	conn, err := controlchannelconnection.New(connLogger, serviceUrl, a.config.GetPrivateKey(), params, headers, client)
	if err != nil {
		return err
	}

	// create logger for control channel
	a.controlChannel, err = controlchannel.Start(ccLogger, ccId, conn, serviceUrl, agentType, a.config)

	return err
}

func (a *Agent) monitorControlChannel() {
	maximumMissedPongSets := int(controlchannelconnection.MaximumReconnectWaitTime / bastionDisconnectTimeout)
	missedPongSets := 0

	for {
		select {
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
			a.isControlChannelAlive = false
			a.agentShutdownChan <- fmt.Errorf("control channel closed with error: %s", a.controlChannel.Err())
			return
		}
	}
}

func (a *Agent) Close(reason error) {
	a.logger.Infof("Agent closing because: %s", reason)
	// this is guaranteed to return within 10 seconds (see controlchannel.go:closeTimeout)
	if a.controlChannel != nil && a.isControlChannelAlive {
		a.controlChannel.Close(reason)
	}

	if a.conn != nil {
		a.conn.Close(reason, 10*time.Second)
	}

	a.config.Data.ShutdownState = fmt.Sprintf("%+v", getState())
	if reason == nil {
		a.config.Data.ShutdownReason = ""
	} else {
		a.config.Data.ShutdownReason = reason.Error()
	}

	if err := a.config.Save(); err != nil {
		a.logger.Errorf("failed to save shutdown reason: %s", err)
	}

	if reason == nil {
		os.Exit(0)
	} else {
		os.Exit(1)
	}
}

func parseFlags() error {
	// Helpful flags
	flag.BoolVar(&printVersion, "version", false, "Print current version of the agent")
	flag.BoolVar(&listLogFile, "logs", false, "Print the agent log file path")

	// Our required registration flags
	flag.StringVar(&activationToken, "activationToken", "", "Single-use token used to register the agent")
	flag.StringVar(&registrationKey, "registrationKey", "", "API Key used to register the agent")

	// forced re-registration flags
	flag.BoolVar(&forceReRegistration, "y", false, "Boolean flag if you want to force the agent to re-register")
	flag.BoolVar(&forceReRegistration, "f", false, "Same as -y")

	// Our flag to determine if this is systemd and will therefore wait for successful registration
	flag.BoolVar(&wait, "w", false, "Mode for background processes to wait for successful registration")

	// All optional flags
	flag.StringVar(&serviceUrl, "serviceUrl", prodServiceUrl, "Service URL to use")
	flag.StringVar(&orgId, "orgId", "", "OrgID to use")
	flag.StringVar(&targetName, "targetName", "", "Target name to use")
	flag.StringVar(&targetId, "targetId", "", "Target ID to use")
	flag.StringVar(&logLevel, "logLevel", logger.Debug.String(), "The log level to use")

	flag.StringVar(&environmentId, "environmentId", "", "Policy environment ID to associate with agent")
	flag.StringVar(&environmentName, "environmentName", "", "(Deprecated) Policy environment Name to associate with agent")

	// new env flags
	flag.StringVar(&environmentId, "envId", "", "(Deprecated) Please use environmentId")
	flag.StringVar(&environmentName, "envName", "", "(Deprecated) Policy environment Name to associate with agent")

	// Parse any flag
	flag.Parse()

	// The environment will overwrite any flags passed
	if agentType == Cluster {
		serviceUrl = os.Getenv("SERVICE_URL")
		activationToken = os.Getenv("ACTIVATION_TOKEN")
		targetName = os.Getenv("TARGET_NAME")
		targetId = os.Getenv("TARGET_ID")
		environmentId = os.Getenv("ENVIRONMENT")
		idpProvider = os.Getenv("IDP_PROVIDER")
		idpOrgId = os.Getenv("IDP_ORG_ID")
		namespace = os.Getenv("NAMESPACE")
		registrationKey = os.Getenv("API_KEY")
	}

	// Make sure our service url is correctly formatted
	if !strings.HasPrefix(serviceUrl, "http") {
		if url, err := bzhttp.BuildEndpoint("https://", serviceUrl); err != nil {
			return fmt.Errorf("error adding scheme to serviceUrl %s: %s", serviceUrl, err)
		} else {
			serviceUrl = url
		}
	}
	return nil
}

func handleRegistration(logger *logger.Logger) error {
	// Check if there is a public key in the vault, if not then agent is not registered
	if registered, err := isRegistered(); err != nil {
		logger.Error(err)
		return err
	} else if !registered && wait {
		logger.Info("Agent waiting for registration...")
		return fmt.Errorf("")
	} else if !registered || forceReRegistration {

		// Only check RBAC permissions if we are inside a cluster
		if vault.InCluster() {
			if err := rbac.CheckPermissions(logger, namespace); err != nil {
				rerr := fmt.Errorf("error verifying agent kubernetes setup: %s", err)
				logger.Error(rerr)
				return rerr
			} else {
				logger.Info("Namespace and service account permissions verified")
			}
		}

		// register the agent with bastion, if not already registered
		if err := registration.Register(logger, serviceUrl, activationToken, registrationKey, targetId); err != nil {
			reportError(logger, err)
			return err
		}

		os.Exit(0)
	} else {
		logger.Infof("Bzero Agent is already registered with %s", serviceUrl)
	}

	return nil
}

func isRegistered() (bool, error) {
	registered := false

	if config, err := vault.LoadVault(); err != nil {
		return registered, fmt.Errorf("could not load vault: %s", err)
	} else if (config.Data.PublicKey == "" || forceReRegistration) && flag.NFlag() > 0 { // no public key means unregistered
		if !wait {

			// we need either an activation token or an registration key to register the agent
			if activationToken == "" && registrationKey == "" {
				return registered, fmt.Errorf("in order to register the agent, user must provide either an activation token or api key")
			}

			// Save flags passed to our config so registration can access them
			config.Data = vault.SecretData{
				ServiceUrl:      serviceUrl,
				Namespace:       namespace,
				IdpProvider:     idpProvider,
				IdpOrgId:        idpOrgId,
				EnvironmentId:   environmentId,
				EnvironmentName: environmentName,
				AgentType:       agentType,
				TargetName:      targetName,
				Version:         getAgentVersion(),
			}
			if err := config.Save(); err != nil {
				return registered, fmt.Errorf("error saving vault: %s", err)
			}
		}
	} else {
		registered = true

		// load any variables we might need
		serviceUrl = config.Data.ServiceUrl
		targetName = config.Data.TargetName
	}

	return registered, nil
}

func getAgentVersion() string {
	if os.Getenv("DEV") == "true" {
		return "6.7.0"
	} else {
		return "$AGENT_VERSION"
	}
}

func getState() map[string]string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}

	return map[string]string{
		"activationToken":       activationToken,
		"registrationKeyLength": fmt.Sprintf("%v", len(registrationKey)),
		"targetName":            targetName,
		"targetHostName":        hostname,
		"goos":                  runtime.GOOS,
		"goarch":                runtime.GOARCH,
	}
}

func setAgentType() {
	// determine agent type
	if vault.InCluster() {
		agentType = Cluster
	} else {
		agentType = Bzero
	}
}
