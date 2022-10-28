package config

import (
	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	"github.com/stretchr/testify/mock"
)

type MockClient struct {
	mock.Mock
}

func (m *MockClient) Fetch() (data.DataV2, error) {
	args := m.Called()
	return args.Get(0).(data.DataV2), args.Error(1)
}

func (m *MockClient) Save(d data.DataV2) error {
	args := m.Called()
	return args.Error(0)
}
