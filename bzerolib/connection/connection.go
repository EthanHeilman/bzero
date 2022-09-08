package connection

import (
	"time"

	"bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/broker"
)

type Connection interface {
	Subscribe(id string, channel broker.IChannel)
	Close(reason error, timeout time.Duration)
	Send(agentMessage agentmessage.AgentMessage)
	Done() <-chan struct{}
	Err() error
	Ready() bool
}
