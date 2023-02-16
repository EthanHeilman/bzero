// Code generated by mockery v2.15.0. DO NOT EDIT.

package mocks

import (
	data "bastionzero.com/bctl/v1/bctl/agent/config/keyshardconfig/data"
	mock "github.com/stretchr/testify/mock"
)

// PWDBConfig is an autogenerated mock type for the PWDBConfig type
type PWDBConfig struct {
	mock.Mock
}

// LastKey provides a mock function with given fields: targetId
func (_m *PWDBConfig) LastKey(targetId string) (data.KeyEntry, error) {
	ret := _m.Called(targetId)

	var r0 data.KeyEntry
	if rf, ok := ret.Get(0).(func(string) data.KeyEntry); ok {
		r0 = rf(targetId)
	} else {
		r0 = ret.Get(0).(data.KeyEntry)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(targetId)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

type mockConstructorTestingTNewPWDBConfig interface {
	mock.TestingT
	Cleanup(func())
}

// NewPWDBConfig creates a new instance of PWDBConfig. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewPWDBConfig(t mockConstructorTestingTNewPWDBConfig) *PWDBConfig {
	mock := &PWDBConfig{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
