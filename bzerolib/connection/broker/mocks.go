package broker

import (
	"bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"github.com/stretchr/testify/mock"
)

type MockChannel struct {
	IChannel
	mock.Mock
}

func (m *MockChannel) Receive(agentMessage agentmessage.AgentMessage) {
	m.Called()
}

func (m *MockChannel) Close(reason error) {
	m.Called()
}
