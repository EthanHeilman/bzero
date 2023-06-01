package agenttype

// AgentType serves two purposes. One is to inform certain decisions on the agent side (e.g., if running in a cluster,
// logging and config work differently). The other is to let the backend know what kind of agent we are. This affects
// what we show to the user, as well as what kind of connetions we serve them.
type AgentType string

const (
	Kubernetes AgentType = "cluster"
	Linux      AgentType = "linux"
	Windows    AgentType = "windows"
)
