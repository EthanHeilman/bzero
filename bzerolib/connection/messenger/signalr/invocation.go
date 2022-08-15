package signalr

import (
	"fmt"
	"sync"
)

type Invocator interface {
	IsEmpty() bool
	Match(id string) (SignalRMessage, bool)
	Track(message SignalRMessage) SignalRMessage
}

type InvocationTracker struct {
	// Map of sent messages for which we're awaiting CompletionMessages
	// keyed by InvocationId
	trackedMessages     map[string]SignalRMessage
	trackedMessagesLock sync.Mutex

	// Counter for generating invocationIds
	counter int
}

func NewInvocationTracker() *InvocationTracker {
	return &InvocationTracker{
		trackedMessages: make(map[string]SignalRMessage),
	}
}

func (i *InvocationTracker) IsEmpty() bool {
	i.trackedMessagesLock.Lock()
	defer i.trackedMessagesLock.Unlock()

	return len(i.trackedMessages) == 0
}

func (i *InvocationTracker) Match(id string) (SignalRMessage, bool) {
	i.trackedMessagesLock.Lock()
	defer i.trackedMessagesLock.Unlock()

	message, ok := i.trackedMessages[id]
	if ok {
		delete(i.trackedMessages, id)
	}

	return message, ok
}

// Invocation does not promise strictly increasing Invocation IDs
// becuase messages can fail between getting the ID and Tracking
func (i *InvocationTracker) Track(message SignalRMessage) SignalRMessage {
	i.trackedMessagesLock.Lock()
	defer i.trackedMessagesLock.Unlock()

	invocationId := fmt.Sprint(i.counter)
	i.counter++

	message.InvocationId = invocationId

	i.trackedMessages[invocationId] = message
	return message
}
