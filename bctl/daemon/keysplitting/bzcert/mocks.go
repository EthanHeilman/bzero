package bzcert

import (
	"bastionzero.com/bctl/v1/bzerolib/keysplitting/bzcert"
	mock "github.com/stretchr/testify/mock"
)

// mocked version of the DaemonBZCert
type MockDaemonBZCert struct {
	mock.Mock
}

func (m *MockDaemonBZCert) Cert() *bzcert.VerifiedBZCert {
	args := m.Called()
	return args.Get(0).(*bzcert.VerifiedBZCert)
}

func (m *MockDaemonBZCert) Verify(idpProvider string, idpOrgId string) error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockDaemonBZCert) Refresh() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockDaemonBZCert) Hash() string {
	args := m.Called()
	cert := args.Get(0).(*bzcert.VerifiedBZCert)
	return cert.Hash()
	// hashBytes, _ := util.HashPayload(cert)
	// return base64.StdEncoding.EncodeToString(hashBytes)
}

func (m *MockDaemonBZCert) PrivateKey() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockDaemonBZCert) Expired() bool {
	args := m.Called()
	return args.Bool(0)
}
