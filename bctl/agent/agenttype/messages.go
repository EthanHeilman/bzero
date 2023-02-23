package agenttype

// Our agent type specifically refers to the agent manager aka what environment are we
// setup in? and is there anything we need to do differently in it? Minimally, it requires
// a different setup which is implemented in separate "NewXAgent()" functions
type AgentType string

const (
	Kubernetes AgentType = "cluster"
	Systemd    AgentType = "bzero"
)
