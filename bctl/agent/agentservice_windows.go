//go:build windows

package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kardianos/service"
)

type AgentService struct {
	agent            *Agent
	kardianosService service.Service
	serviceLogger    service.Logger
}

const agentServiceName = "BastionZeroAgent"
const serviceDisplayName = "BastionZero Agent Service"

// This configures the service that is responsible for the agent lifecycle.
// The resulting service has Start/Stop handler methods which are invoked by the service manager
// and handle the respective Start/Stop service signals.
// The user can trigger manually a Start/Stop signal using the service management flags. These
// flags are passed in the service library which in turn triggers the respective signal
// which then gets handled by tge AgentService. Alternatively, the Agent Service
// handles also Start/Stop signals sent by the service manager during an OS startup/shutdown.
// To create a new Agent Service we pass the "install" command to the service library, to start
// it we pass the "start" command and respectively to uninstall, we firstly "stop" and then "uninstall"
func NewAgentService(agent *Agent) (agentService *AgentService, err error) {
	agentService = &AgentService{
		agent: agent,
	}

	// Potential values are: aix-ssrc | darwin-launchd | freebsd | solaris-smf | windows-service | linux-systemd | linux-upstart | linux-openrc | linux-rcs | unix-systemv
	agent.logger.Debugf("System service is %s", service.Platform())

	options := make(service.KeyValue)
	options["StartType"] = "automatic"
	options["OnFailure"] = "restart"
	svcConfig := &service.Config{
		Name:        agentServiceName,
		DisplayName: serviceDisplayName,
		Description: "This is a service responsible for the lifecycle of the BastionZero Agent.",
		Option:      options,
	}

	if agentService.kardianosService, err = service.New(agentService, svcConfig); err != nil {
		return nil, fmt.Errorf("failed to start agent service: %s", err)
	}

	errs := make(chan error, 5)
	if agentService.serviceLogger, err = agentService.kardianosService.Logger(errs); err != nil {
		return nil, fmt.Errorf("failed to start agent service logger: %s", err)
	}

	go func() {
		for {
			err := <-errs
			if err != nil {
				agent.logger.Errorf("service runtime error: %s", err)
			}
		}
	}()

	if len(svcFlag) != 0 {
		err = service.Control(agentService.kardianosService, svcFlag)
		if err != nil {
			return nil, err
		}
		// Return a nil service here because when a service command is passed we still need to exit
		return nil, nil
	}

	if successfulRegistration {
		agent.logger.Info("Setting up agent system service")

		err = service.Control(agentService.kardianosService, "install")
		serviceExistsErrMessage := fmt.Sprintf("service %s already exists", agentServiceName)

		// In case of a re-registration or other corner case situations, the service is probably already installed and running. If so, we should remove it first.
		if err != nil && strings.Contains(err.Error(), serviceExistsErrMessage) {
			agentService.agent.logger.Debug("removing existing service and installing a new one")

			err = service.Control(agentService.kardianosService, "stop")
			serviceStoppedErrMessage := fmt.Sprintf("Failed to stop %s: The service has not been started.", serviceDisplayName)
			// It's possible that the user stopped the service before re-registering. If so, we can just continue
			if err != nil && !strings.Contains(err.Error(), serviceStoppedErrMessage) {
				return nil, err
			}

			err = service.Control(agentService.kardianosService, "uninstall")
			if err != nil {
				return nil, err
			}

			// Uninstalling takes a second, by waiting here we ensure the service has been uninstalled before installing it again
			time.Sleep(2 * time.Second)

			err = service.Control(agentService.kardianosService, "install")
			if err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}

		err = service.Control(agentService.kardianosService, "start")
		if err != nil {
			return nil, err
		}
		/* Return a nil service here because when a successful registration took place we need
		to exit and let the service manager take over */
		return nil, nil
	}

	return agentService, nil
}

// Starts the agent service, required to implement the [service interface]: https://github.com/kardianos/service/blob/9832e01049dd49bb66dd8fc447c72eefba7fc2cd/service.go#L338
func (as *AgentService) Start(s service.Service) error {
	as.agent.logger.Info("Agent Service is Starting")
	if service.Interactive() {
		as.agent.logger.Debug("Running in terminal.")
	} else {
		as.agent.logger.Debug("Running under service manager.")
	}

	// Start should not block. Do the actual work async.
	go as.agent.Run()
	as.agent.logger.Info("Agent Service Started")
	return nil
}

// Stops the agent service, required to implement the [service interface]: https://github.com/kardianos/service/blob/9832e01049dd49bb66dd8fc447c72eefba7fc2cd/service.go#L338
func (as *AgentService) Stop(s service.Service) error {
	as.agent.logger.Infof("Agent Service is Stopping")
	as.agent.Close(&ServiceStopError{})
	return nil
}

func (as *AgentService) Run() error {

	errChan := make(chan error)

	// Listen to agent restarts and exit so the service manager can pick up from here and restart
	go func() {
		<-as.agent.Done()
		var serviceStopErr *ServiceStopError
		// If this is the service manager stopping the agent, just return and let the manager exit properly
		if errors.As(as.agent.Err(), &serviceStopErr) {
			return
		}
		errChan <- as.agent.Err()
	}()

	go func() {
		errChan <- as.kardianosService.Run()
	}()

	return <-errChan
}
