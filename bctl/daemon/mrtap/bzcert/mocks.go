package bzcert

import (
	"encoding/base64"

	"bastionzero.com/bctl/v1/bzerolib/keypair"
	"bastionzero.com/bctl/v1/bzerolib/mrtap/bzcert"
	"bastionzero.com/bctl/v1/bzerolib/mrtap/util"
	mock "github.com/stretchr/testify/mock"
)

// mocked version of the DaemonBZCert
type MockDaemonBZCert struct {
	mock.Mock
}

func (m *MockDaemonBZCert) Cert() *bzcert.BZCert {
	args := m.Called()
	return args.Get(0).(*bzcert.BZCert)
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
	cert := args.Get(0).(*bzcert.BZCert)
	hashBytes, _ := util.HashPayload(cert)
	return base64.StdEncoding.EncodeToString(hashBytes)
}

func (m *MockDaemonBZCert) PrivateKey() *keypair.PrivateKey {
	args := m.Called()
	return args.Get(0).(*keypair.PrivateKey)
}

func (m *MockDaemonBZCert) Expired() bool {
	args := m.Called()
	return args.Bool(0)
}
