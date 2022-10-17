package main

import (
	"testing"

	"bastionzero.com/bctl/v1/bctl/agent/mocks"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

func TestAgent(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agent Suite")
}

var _ = Describe("Agent", func() {
	logger := logger.MockLogger(GinkgoWriter)
	// osSignalChan := make(chan os.Signal)

	generateMockRegistration := func() *mocks.IRegistration {
		mockRegistration := mocks.NewIRegistration(GinkgoT())
		mockRegistration.On("Register", mock.AnythingOfType("*logger.Logger"), mock.AnythingOfType("*mocks.Config")).Return(nil)

		return mockRegistration
	}

	generateMockConfig := func() *mocks.Config {
		fakeKeyPair, _ := tests.GenerateEd25519Key()

		mockConfig := mocks.NewConfig(GinkgoT())
		mockConfig.On("GetPublicKey").Return(fakeKeyPair.Base64EncodedPublicKey)
		mockConfig.On("GetTargetId").Return("targetid")
		mockConfig.On("GetShutdownInfo").Return("reason", map[string]string{})
		mockConfig.On("SetVersion", mock.AnythingOfType("string")).Return(nil)
		mockConfig.On("SetShutdownInfo").Return(nil)
		mockConfig.On("GetServiceUrl").Return("serviceUrl?")

		return mockConfig
	}

	Context("Agent Registration", func() {
		When("we're not registered", func() {
			var err error
			var mockEmptyConfig *mocks.Config
			var mockRegistration *mocks.IRegistration

			BeforeEach(func() {
				mockEmptyConfig = mocks.NewConfig(GinkgoT())
				mockEmptyConfig.On("GetPublicKey").Return("")
				mockEmptyConfig.On("GetShutdownInfo").Return("", map[string]string{})
				mockEmptyConfig.On("SetVersion", mock.AnythingOfType("string")).Return(nil)

				mockRegistration = generateMockRegistration()

				agent := &Agent{
					logger:       logger,
					config:       mockEmptyConfig,
					registration: mockRegistration,
				}

				err = agent.Run(false)
			})

			It("Registers", func() {
				Expect(err).ToNot(HaveOccurred())
				mockRegistration.AssertCalled(GinkgoT(), "Register", logger, mockEmptyConfig)
			})
		})

		When("We're already registered, but are being force to re-register", func() {
			var err error
			var mockConfig *mocks.Config
			var mockRegistration *mocks.IRegistration

			BeforeEach(func() {
				mockConfig = generateMockConfig()
				mockRegistration = generateMockRegistration()

				agent := &Agent{
					logger:       logger,
					config:       mockConfig,
					registration: mockRegistration,
				}

				forceReRegistration := true
				err = agent.Run(forceReRegistration)
			})

			It("Registers", func() {
				Expect(err).ToNot(HaveOccurred())
				mockRegistration.AssertCalled(GinkgoT(), "Register", logger, mockConfig)
			})
		})
	})

	// Context("Control Channel Startup", func() {
	// 	When("")
	// })
})
