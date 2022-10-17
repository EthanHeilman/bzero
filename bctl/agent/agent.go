package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime/debug"
	"strings"

	"bastionzero.com/bctl/v1/bctl/agent/rbac"
	"bastionzero.com/bctl/v1/bctl/agent/registration"
	"bastionzero.com/bctl/v1/bctl/agent/vault"
	"bastionzero.com/bctl/v1/bzerolib/bzos"
	"bastionzero.com/bctl/v1/bzerolib/logger"
)

var (
	serviceUrl, orgId                string
	environmentId, environmentName   string
	activationToken, registrationKey string
	idpProvider, namespace, idpOrgId string
	targetId, targetName             string
	logLevel, vaultPath              string
	forceReRegistration              bool
	wait                             bool
	printVersion                     bool
	listLogFile                      bool
)

const (
	Cluster = "cluster"
	Bzero   = "bzero"

	prodServiceUrl     = "https://cloud.bastionzero.com/"
	defaultLogFilePath = "/var/log/bzero/bzero-agent.log"

	// Env var to flag if we are in a kube cluster
	inClusterEnvVar = "BASTIONZERO_IN_CLUSTER"
)

func main() {
	parseFlags()

	agentType := getAgentType()
	version := getAgentVersion()

	// Check if we need to output any info
	if printVersion {
		fmt.Println(version)
		return
	}

	if listLogFile {
		switch agentType {
		case Bzero:
			fmt.Println(defaultLogFilePath)
		case Cluster:
			fmt.Println("BastionZero Agent logs can be accessed via the Kube API server by tailing the pods logs")
		}
		return
	}

	// Make sure our service url is correctly formatted
	// This is just a kindness to our devs so that the agent can be more forgiving to malformatted urls
	if !strings.HasPrefix(serviceUrl, "https") {
		combo, err := url.Parse(serviceUrl)
		if err != nil {
			fmt.Printf("error adding scheme to serviceUrl %s: %s\n", serviceUrl, err)
		}
		combo.Scheme = "https"
		serviceUrl = combo.String()
	}

	// This sets up our registration object with all relevant information in case we need to register
	reg := registration.New(serviceUrl, activationToken, registrationKey, targetId, version, environmentId, environmentName, targetName, idpProvider, idpOrgId)

	var agent *Agent
	var err error
	switch agentType {
	case Bzero:
		agent, err = NewSystemDAgent(vaultPath, version, reg, bzos.OsShutdownChan())
	case Cluster:
		agent, err = NewKubeAgent(version, reg, bzos.OsShutdownChan())
	}

	if err != nil {
		fmt.Printf("ERROR: failed to start agent: %s\n %+v", err, debug.Stack())
	} else {
		agent.Run(forceReRegistration)
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

func NewSystemDAgent(
	configDir string,
	version string,
	registration *registration.Registration,
	signalChan <-chan os.Signal,
) (*Agent, error) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &Agent{
		ctx:          ctx,
		cancel:       cancel,
		osSignalChan: signalChan,
		version:      version,
		agentType:    Bzero,
		registration: registration,
	}

	// This context will allow us to cancel everything concisely
	go func() {
		select {
		case <-a.tmb.Dying():
			cancel()
			return
		case <-a.osSignalChan:
			cancel()
			return
		case <-ctx.Done():
			return
		}
	}()

	config, err := vault.LoadSystemDVault(configDir)
	if err != nil {
		return nil, err
	}
	a.config = config

	// Create our logger
	log, err := logger.New(&logger.Config{
		ConsoleWriters: []io.Writer{os.Stdout},
		FilePath:       defaultLogFilePath,
	})
	if err != nil {
		return nil, err
	}
	log.AddAgentVersion(version)
	log.AddAgentType(Bzero)

	log.Info("Starting up the BastionZero Agent")
	a.logger = log

	// If this is an agent run by systemd, we add the -w (wait) flag
	// which means that this process will wait until it detects a new
	// registration and then it we load it before proceeding
	isRegistered := config.GetPublicKey() != ""
	if !isRegistered && wait {
		a.logger.Info("This Agent is waiting for a new registration to start up. Please see documentation for more information: https://docs.bastionzero.com/docs/deployment/installing-the-agent#step-2-2-agent-registration")
		config.WaitForRegistration(signalChan)

		// Now that we're registered, we need to reload our config to make sure it's up-to-date
		if err := config.Reload(); err != nil {
			a.logger.Error(err)
			return nil, err
		}
	}

	return a, nil
}

func NewKubeAgent(
	version string,
	registration *registration.Registration,
	signalChan <-chan os.Signal,
) (*Agent, error) {

	ctx, cancel := context.WithCancel(context.Background())
	a := &Agent{
		ctx:          ctx,
		cancel:       cancel,
		version:      version,
		osSignalChan: signalChan,
		agentType:    Cluster,
		registration: registration,
	}

	// This context will allow us to cancel everything concisely
	go func() {
		select {
		case <-a.tmb.Dying():
			cancel()
			return
		case <-a.osSignalChan:
			cancel()
			return
		case <-ctx.Done():
			return
		}
	}()

	// Load our vault
	config, err := vault.LoadKubernetesVault(ctx, namespace, targetName)
	if err != nil {
		return nil, err
	}
	a.config = config

	// Create our logger
	log, err := logger.New(&logger.Config{
		ConsoleWriters: []io.Writer{os.Stdout},
	})
	if err != nil {
		return nil, err
	}
	log.AddAgentVersion(version)
	log.AddAgentType(Cluster)

	log.Infof("Starting up the BastionZero Agent")

	// Verify we have the correct RBAC permissions
	if err := rbac.CheckPermissions(log, namespace); err != nil {
		err = fmt.Errorf("error verifying agent kubernetes setup: %w", err)
		log.Error(err)
		return nil, err
	} else {
		log.Info("Namespace and service account permissions verified")
	}
	a.logger = log

	return a, nil
}

func parseFlags() {
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
	flag.BoolVar(&wait, "w", false, "Mode for systemd processes to wait for successful registration")

	// All optional flags
	flag.StringVar(&serviceUrl, "serviceUrl", prodServiceUrl, "Service URL to use")
	flag.StringVar(&orgId, "orgId", "", "OrgID to use")
	flag.StringVar(&targetName, "targetName", "", "Target name to use")
	flag.StringVar(&targetId, "targetId", "", "Target ID to use")
	flag.StringVar(&logLevel, "logLevel", logger.Debug.String(), "The log level to use")

	flag.StringVar(&environmentId, "environmentId", "", "Policy environment ID to associate with agent")
	flag.StringVar(&environmentName, "environmentName", "", "(Deprecated) Policy environment Name to associate with agent")

	// Use a different config path for running different agents on the same box
	flag.StringVar(&vaultPath, "vaultPath", vault.DefaultVaultDirectory, "Path to agent's config")

	// Parse any flag
	flag.Parse()

	// The environment will overwrite any flags passed
	if getAgentType() == Cluster {
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
}

func getAgentVersion() string {
	return "$AGENT_VERSION"
}

func getAgentType() string {
	// determine agent type
	if val := os.Getenv(inClusterEnvVar); val == "bzero" {
		return Cluster
	} else {
		return Bzero
	}
}
