package mrtap

import (
	"bastionzero.com/bctl/v1/bzerolib/keypair"
	mock "github.com/stretchr/testify/mock"
)

// MockMrtapConfig is an autogenerated mock type for the MrtapConfig type
type MockMrtapConfig struct {
	mock.Mock
}

// GetIdpOrgId provides a mock function with given fields:
func (_m *MockMrtapConfig) GetIdpOrgId() string {
	ret := _m.Called()

	var r0 string
	if rf, ok := ret.Get(0).(func() string); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(string)
	}

	return r0
}

// GetIdpProvider provides a mock function with given fields:
func (_m *MockMrtapConfig) GetIdpProvider() string {
	ret := _m.Called()

	var r0 string
	if rf, ok := ret.Get(0).(func() string); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(string)
	}

	return r0
}

// GetPrivateKey provides a mock function with given fields:
func (_m *MockMrtapConfig) GetPrivateKey() *keypair.PrivateKey {
	ret := _m.Called()

	var r0 *keypair.PrivateKey
	if rf, ok := ret.Get(0).(func() *keypair.PrivateKey); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(*keypair.PrivateKey)
	}

	return r0
}

// GetPublicKey provides a mock function with given fields:
func (_m *MockMrtapConfig) GetPublicKey() *keypair.PublicKey {
	ret := _m.Called()

	var r0 *keypair.PublicKey
	if rf, ok := ret.Get(0).(func() *keypair.PublicKey); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(*keypair.PublicKey)
	}

	return r0
}

// GetServiceAccountJwksUrls provides a mock function with given fields:
func (_m *MockMrtapConfig) GetServiceAccountJwksUrls() []string {
	ret := _m.Called()

	var r0 []string
	if rf, ok := ret.Get(0).(func() []string); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]string)
		}
	}

	return r0
}