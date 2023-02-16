package client

import (
	agentdata "bastionzero.com/bctl/v1/bctl/agent/config/agentconfig/data"
	ksdata "bastionzero.com/bctl/v1/bctl/agent/config/keyshardconfig/data"
	"github.com/stretchr/testify/mock"
)

type MockClient struct {
	mock.Mock
}

func (m *MockClient) FetchAgentData() (agentdata.AgentDataV2, error) {
	args := m.Called()
	return args.Get(0).(agentdata.AgentDataV2), args.Error(1)
}

func (m *MockClient) FetchKeyShardData() (ksdata.KeyShardData, error) {
	args := m.Called()
	return args.Get(0).(ksdata.KeyShardData), args.Error(1)
}

func (m *MockClient) Save(d interface{}) error {
	args := m.Called(d)
	return args.Error(0)
}
