package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"bastionzero.com/bctl/v1/bctl/agent/config"
	"bastionzero.com/bctl/v1/bctl/agent/config/client"
	"bastionzero.com/bctl/v1/bctl/agent/rbac"
	"bastionzero.com/bctl/v1/bctl/agent/registration"
	"bastionzero.com/bctl/v1/bzerolib/bzos"
	"bastionzero.com/bctl/v1/bzerolib/logger"
)

var (
	serviceUrl, orgId                string
	environmentId, environmentName   string
	activationToken, registrationKey string
	idpProvider, namespace, idpOrgId string
	targetId, targetName             string
	logLevel, configDir              string
	forceReregistration              bool
	wait                             bool
	printVersion                     bool
	listLogFile                      bool
	attemptedRegistration            bool
)

const (
	prodServiceUrl         = "https://cloud.bastionzero.com/"
	defaultLogFilePath     = "/var/log/bzero/bzero-agent.log"
	defaultConfigDirectory = "/etc/bzero"

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
		case Systemd:
			fmt.Println(defaultLogFilePath)
		case Kubernetes:
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
	case Systemd:
		agent, err = NewSystemdAgent(version, reg)
	case Kubernetes:
		agent, err = NewKubeAgent(version, reg)
	}

	if err != nil {
		fmt.Printf("ERROR: failed to start agent: %s\n", err)
		os.Exit(1)
	}

	if exitError := agent.Run(); exitError == nil {
		os.Exit(0)
	}

	os.Exit(1)
}

func parseFlags() {
	// Helpful flags
	flag.BoolVar(&printVersion, "version", false, "Print current version of the agent")
	flag.BoolVar(&listLogFile, "logs", false, "Print the agent log file path")

	// Our required registration flags
	flag.StringVar(&activationToken, "activationToken", "", "Single-use token used to register the agent")
	flag.StringVar(&registrationKey, "registrationKey", "", "API Key used to register the agent")

	// forced re-registration flags
	flag.BoolVar(&forceReregistration, "y", false, "Boolean flag if you want to force the agent to re-register")
	flag.BoolVar(&forceReregistration, "f", false, "Same as -y")

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

	// new env flags
	flag.StringVar(&environmentId, "envId", "", "(Deprecated) Please use environmentId")
	flag.StringVar(&environmentName, "envName", "", "(Deprecated) Policy environment Name to associate with agent")

	flag.StringVar(&configDir, "configDir", defaultConfigDirectory, "Specify a unique config path for running multiple agents on the same box")

	// Parse any flag
	flag.Parse()

	attemptedRegistration = activationToken != "" || registrationKey != ""

	// The environment will overwrite any flags passed
	if getAgentType() == Kubernetes {
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

func NewSystemdAgent(
	version string,
	registration *registration.Registration,
) (a *Agent, err error) {
	ctx, cancel := context.WithCancel(context.Background())
	a = &Agent{
		ctx:          ctx,
		cancel:       cancel,
		osSignalChan: bzos.OsShutdownChan(),
		version:      version,
		agentType:    Systemd,
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

	// Create our logger
	if a.logger, err = logger.New(&logger.Config{
		ConsoleWriters: []io.Writer{os.Stdout},
		FilePath:       defaultLogFilePath,
	}); err != nil {
		return
	}
	a.logger.AddAgentVersion(version)
	a.logger.AddAgentType(string(Systemd))

	// Now that we have our logger, make sure that any error from statements below
	// gets logged
	defer func() {
		if err != nil {
			a.logger.Error(err)
		}
	}()

	agentClient, err := client.NewSystemdClient(configDir, client.Agent)
	if err != nil {
		return a, fmt.Errorf("failed to initialize agent config client: %s", err)
	} else if a.agentConfig, err = config.LoadAgentConfig(agentClient); err != nil {
		return a, fmt.Errorf("failed to load agent config: %s", err)
	}

	if keyShardClient, err := client.NewSystemdClient(configDir, client.KeyShard); err != nil {
		return a, fmt.Errorf("failed to initialize key shard config client: %w", err)
	} else if a.keyShardConfig, err = config.LoadKeyShardConfig(keyShardClient); err != nil {
		return a, fmt.Errorf("failed to load key shard config: %w", err)
	}

	a.logger.Info("Starting up the BastionZero Agent")

	// If this is an agent run by systemd, we add the -w (wait) flag
	// which means that this process will wait until it detects a new
	// registration and then it we load it before proceeding
	isRegistered := !a.agentConfig.GetPublicKey().IsEmpty()
	if !isRegistered && wait {
		a.logger.Info("This Agent is waiting for a new registration to start up. Please see documentation for more information: https://docs.bastionzero.com/docs/deployment/installing-the-agent#step-2-2-agent-registration")
		agentClient.WaitForRegistration(a.ctx)

		// Now that we're registered, we need to reload our config to make sure it's up-to-date
		if err := a.agentConfig.Reload(); err != nil {
			return a, fmt.Errorf("failed to reload config after new registration detected: %w", err)
		}
	}

	// Register if we aren't already
	if !isRegistered || forceReregistration {
		a.logger.Info("Agent is starting new registration")

		// Regardless of the response, we're done here. Registration for the Systemd agent
		// is designed to essentially be a cli command and not fully start up the agent
		if err = registration.Register(a.ctx, a.logger, a.agentConfig); err != nil {
			return
		}
		os.Exit(0)
	} else {
		// we're already registered. If another attempt was made to register, exit
		if attemptedRegistration {
			err = fmt.Errorf("BastionZero Agent is already registered. To force re-register, use the -y flag")
			return
		}

		a.logger.Infof("BastionZero Agent is registered with %s", a.config.GetServiceUrl())
	}

	return
}

func NewKubeAgent(
	version string,
	registration *registration.Registration,
) (a *Agent, err error) {

	ctx, cancel := context.WithCancel(context.Background())
	a = &Agent{
		ctx:          ctx,
		cancel:       cancel,
		version:      version,
		osSignalChan: bzos.OsShutdownChan(),
		agentType:    Kubernetes,
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

	// Create our logger
	if a.logger, err = logger.New(&logger.Config{
		ConsoleWriters: []io.Writer{os.Stdout},
	}); err != nil {
		return nil, err
	}
	a.logger.AddAgentVersion(version)
	a.logger.AddAgentType(string(Kubernetes))

	// Now that we have our logger, make sure that any error from statements below
	// gets logged
	defer func() {
		if err != nil {
			a.logger.Error(err)
		}
	}()

	// Initialize our config
	if agentClient, err := client.NewKubernetesClient(ctx, namespace, targetName, client.Agent); err != nil {
		return a, fmt.Errorf("failed to initialize agent config client: %w", err)
	} else if a.agentConfig, err = config.LoadAgentConfig(agentClient); err != nil {
		return a, fmt.Errorf("failed to load agent config: %w", err)
	}

	if keyShardClient, err := client.NewKubernetesClient(ctx, namespace, targetName, client.KeyShard); err != nil {
		return a, fmt.Errorf("failed to initialize key shard config client: %w", err)
	} else if a.keyShardConfig, err = config.LoadKeyShardConfig(keyShardClient); err != nil {
		return a, fmt.Errorf("failed to load key shard config: %w", err)
	}

	a.logger.Infof("Starting up the BastionZero Agent")

	// Verify we have the correct RBAC permissions
	if err = rbac.CheckPermissions(a.logger, namespace); err != nil {
		return a, fmt.Errorf("error verifying agent kubernetes setup: %w", err)
	} else {
		a.logger.Info("Namespace and service account permissions verified")
	}

	// The kube agent registers itself (if requested) and then reloads the config
	// to continue running. There is no restart after registration.
	isRegistered := !a.agentConfig.GetPublicKey().IsEmpty()
	if !isRegistered || forceReregistration {
		a.logger.Info("Agent is starting new registration")

		if err = registration.Register(a.ctx, a.logger, a.agentConfig); err != nil {
			return a, fmt.Errorf("failed to register kube agent: %w", err)
		}

		// Now that we're registered, we need to reload our config to make sure it's up-to-date
		if err = a.agentConfig.Reload(); err != nil {
			return
		}
	} else {
		// we're already registered. If another attempt was made to register, exit
		if attemptedRegistration {
			err = fmt.Errorf("BastionZero Agent is already registered. To force re-register, use the -y flag")
			return
		}

		a.logger.Infof("BastionZero Agent is registered with %s", a.config.GetServiceUrl())
	}

	return
}

func getAgentVersion() string {
	return "$AGENT_VERSION"
}

func getAgentType() AgentType {
	// determine agent type
	if val := os.Getenv(inClusterEnvVar); val != "" {
		return Kubernetes
	} else {
		return Systemd
	}
}
