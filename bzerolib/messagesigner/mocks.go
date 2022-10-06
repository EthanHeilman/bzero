package messagesigner

import (
	mock "github.com/stretchr/testify/mock"
)

type MockMessageSigner struct {
	mock.Mock
}

// SignMessage provides a mock function with given fields: content
func (_m *MockMessageSigner) SignMessage(content []byte) (string, error) {
	ret := _m.Called(content)

	var r0 string
	if rf, ok := ret.Get(0).(func([]byte) string); ok {
		r0 = rf(content)
	} else {
		r0 = ret.Get(0).(string)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func([]byte) error); ok {
		r1 = rf(content)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}
