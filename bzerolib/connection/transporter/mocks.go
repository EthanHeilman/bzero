package transporter

import (
	"context"
	"net/http"
	"net/url"

	"bastionzero.com/bctl/v1/bzerolib/telemetry/throughputstats"
	"github.com/stretchr/testify/mock"
)

type MockTransporter struct {
	mock.Mock
}

func (m *MockTransporter) Stats() throughputstats.Digest {
	args := m.Called()
	return args.Get(0).(throughputstats.Digest)
}

func (m *MockTransporter) Done() <-chan struct{} {
	args := m.Called()
	return args.Get(0).(chan struct{})
}

func (m *MockTransporter) Inbound() <-chan *[]byte {
	args := m.Called()
	return args.Get(0).(chan *[]byte)
}

func (m *MockTransporter) Dial(websocketUrl *url.URL, headers http.Header, ctx context.Context) (err error) {
	args := m.Called()
	return args.Error(0)
}

func (m *MockTransporter) Send(message []byte) error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockTransporter) Close(reason error) {
	m.Called()
}

func (m *MockTransporter) Err() error {
	args := m.Called()
	return args.Error(0)
}
