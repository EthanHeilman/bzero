package agenttype

// Our agent type specifically refers to the agent manager aka what environment are we
// setup in? and is there anything we need to do differently in it? Minimally, it requires
// a different setup which is implemented in separate "NewXAgent()" functions
// TODO: this isn't quite as true as it once was, since windows and linux share a NewServerAgent() function
type AgentType string

const (
	Kubernetes AgentType = "cluster"
	Linux      AgentType = "bzero" // maintain compatibility with bastion
	Windows    AgentType = "windows"
)
