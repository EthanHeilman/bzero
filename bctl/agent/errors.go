package main

// this is an error returned when a the service manager stops the agent service
type ServiceStopError struct{}

func (e *ServiceStopError) Error() string {
	return "service manager is stopping the agent"
}
