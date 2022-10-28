package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAgent(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agent Suite")
}

var _ = Describe("Agent", func() {
	// logger := logger.MockLogger(GinkgoWriter)

	// generateMockConfig := func() *mocks.Config {
	// 	fakeKeyPair, _ := tests.GenerateEd25519Key()

	// 	mockConfig := mocks.NewConfig(GinkgoT())
	// 	mockConfig.On("GetPublicKey").Return(fakeKeyPair.Base64EncodedPublicKey)
	// 	mockConfig.On("GetTargetId").Return("targetid")
	// 	mockConfig.On("GetShutdownInfo").Return("reason", "")
	// 	mockConfig.On("SetVersion", mock.AnythingOfType("string")).Return(nil)
	// 	mockConfig.On("SetShutdownInfo").Return(nil)
	// 	mockConfig.On("GetServiceUrl").Return("serviceUrl?")

	// 	return mockConfig
	// }
})
