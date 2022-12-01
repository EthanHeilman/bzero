package config

import (
	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	"github.com/stretchr/testify/mock"
)

type MockClient struct {
	mock.Mock
}

func (m *MockClient) FetchAgentData() (data.AgentDataV2, error) {
	args := m.Called()
	return args.Get(0).(data.AgentDataV2), args.Error(1)
}

func (m *MockClient) FetchKeyShardData() (data.KeyShardData, error) {
	args := m.Called()
	return args.Get(0).(data.KeyShardData), args.Error(1)
}

func (m *MockClient) Save(d interface{}) error {
	args := m.Called(d)
	return args.Error(0)
}
