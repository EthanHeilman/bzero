// Code generated by mockery v2.14.0. DO NOT EDIT.

package mocks

import (
	context "context"

	mock "github.com/stretchr/testify/mock"
)

// IAgentIdentityProvider is an autogenerated mock type for the IAgentIdentityProvider type
type IAgentIdentityProvider struct {
	mock.Mock
}

// GetToken provides a mock function with given fields: ctx
func (_m *IAgentIdentityProvider) GetToken(ctx context.Context) (string, error) {
	ret := _m.Called(ctx)

	var r0 string
	if rf, ok := ret.Get(0).(func(context.Context) string); ok {
		r0 = rf(ctx)
	} else {
		r0 = ret.Get(0).(string)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context) error); ok {
		r1 = rf(ctx)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

type mockConstructorTestingTNewIAgentIdentityProvider interface {
	mock.TestingT
	Cleanup(func())
}

// NewIAgentIdentityProvider creates a new instance of IAgentIdentityProvider. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewIAgentIdentityProvider(t mockConstructorTestingTNewIAgentIdentityProvider) *IAgentIdentityProvider {
	mock := &IAgentIdentityProvider{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
