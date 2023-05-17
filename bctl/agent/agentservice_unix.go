//go:build unix

package main

type AgentService struct {
	agent *Agent
}

func NewAgentService(agent *Agent) (as *AgentService, err error) {
	agentService := &AgentService{
		agent: agent,
	}

	return agentService, nil
}

func (as *AgentService) Run() error {
	return as.agent.Run()
}
