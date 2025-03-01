package broker

import (
	"fmt"
	"sync"

	am "bastionzero.com/bzerolib/connection/agentmessage"
)

type IChannel interface {
	Receive(agentMessage am.AgentMessage)
	Close(reason error)
}

type Broker struct {
	subscribers map[string]IChannel
	lock        sync.RWMutex
}

func New() *Broker {
	return &Broker{
		subscribers: map[string]IChannel{},
	}
}

func (b *Broker) Close(reason error) {
	b.lock.Lock()
	defer b.lock.Unlock()

	for cId, channel := range b.subscribers {
		channel.Close(reason)
		delete(b.subscribers, cId)
	}
}

func (b *Broker) CloseChannel(cId string, reason error) bool {
	b.lock.Lock()
	defer b.lock.Unlock()

	if channel, ok := b.subscribers[cId]; ok {
		channel.Close(reason)
		delete(b.subscribers, cId)
		return true
	}

	return false
}

func (b *Broker) NumChannels() int {
	b.lock.RLock()
	defer b.lock.RUnlock()

	return len(b.subscribers)
}

func (b *Broker) Subscribe(id string, subscriber IChannel) {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.subscribers[id] = subscriber
}

func (b *Broker) Broadcast(message am.AgentMessage) error {
	b.lock.RLock()
	defer b.lock.RUnlock()

	if len(b.subscribers) == 0 {
		return fmt.Errorf("no subscribers are listening")
	}

	for _, channel := range b.subscribers {
		if channel == nil {
			continue
		}

		channel.Receive(message)
	}

	return nil
}

func (b *Broker) DirectMessage(id string, message am.AgentMessage) error {
	b.lock.RLock()
	defer b.lock.RUnlock()

	if channel, ok := b.subscribers[id]; ok {
		channel.Receive(message)
		return nil
	} else {
		return fmt.Errorf("no subscriber with id %s", id)
	}
}
