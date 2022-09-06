package messenger

import (
	"context"
	"net/http"
	"net/url"

	"bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"github.com/stretchr/testify/mock"
)

type MockMessenger struct {
	Messenger
	mock.Mock
}

func (m *MockMessenger) Close(reason error) {
	m.Called()
}

func (m *MockMessenger) Done() <-chan struct{} {
	args := m.Called()
	return args.Get(0).(chan struct{})
}

func (m *MockMessenger) Inbound() <-chan *signalr.SignalRMessage {
	args := m.Called()
	return args.Get(0).(chan *signalr.SignalRMessage)
}

func (m *MockMessenger) Connect(
	ctx context.Context,
	targetUrl string,
	headers http.Header,
	params url.Values,
	targetSelectHandler func(msg agentmessage.AgentMessage,
	) (string, error)) error {

	args := m.Called()
	return args.Error(0)
}

func (m *MockMessenger) Send(message agentmessage.AgentMessage) error {
	args := m.Called()
	return args.Error(0)
}
