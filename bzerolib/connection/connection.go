/*
The connection architecture is comprised of 3 layers:

1. Transporter - responsible for ferrying bytes on the underlying connection
2. Messenger - responsible for processing bytes into protocol messages, and
	unwrapping and wrapping AgentMessages
3. Connection Manager - responsible for all connection-specific logic e.g
	reconnects and processing any control messages

There are also a few helper packages created either to contain logic bleed or
isolate tricky locks.
1. httpclient - this was created to abstract away the specifics of making an
	http call while also allowing for the option to retry with exponential
	backoff. Eventually, this package will replace bzhttp which has a lot of
	undesireable logic bleed.
2. broker - this package was created to abstract away a lock surrounding a map
	used to keep track of the consumers subscribing to the connection managers,
	it is responsible for forwarding messages received to the appropriate consumer
	either via DirectMessage or, eventually, Broadcast.
*/

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
