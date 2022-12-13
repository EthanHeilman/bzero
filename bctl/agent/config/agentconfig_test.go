package config

import (
	"testing"

	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

func TestAgentConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Suite")
}

var _ = Describe("Agent Config", func() {

	Context("Load and Reload", func() {
		When("Loading a config", func() {
			var config *AgentConfig
			var err error

			mockV2 := data.NewMockDataV2()

			BeforeEach(func() {
				mockClient := &MockClient{}
				mockClient.On("FetchAgentData").Return(mockV2, nil)
				mockClient.On("Save", mock.Anything).Return(nil)

				config, err = LoadAgentConfig(mockClient)
			})

			It("initializes without error", func() {
				Expect(err).ToNot(HaveOccurred())
			})

			It("populates the config's data field correctly", func() {
				mockV2.AssertMatchesV2(config.data)
			})
		})

		When("Reloading a config which is different than the initial one", func() {
			var config *AgentConfig
			var err error

			newVersion := "different_version"

			mockV2 := data.NewMockDataV2()
			alteredMockV2 := data.NewMockDataV2()
			alteredMockV2.Version = newVersion

			BeforeEach(func() {
				mockClient := &MockClient{}
				mockClient.On("FetchAgentData").Return(mockV2, nil).Once()
				mockClient.On("FetchAgentData").Return(alteredMockV2, nil).Once()
				mockClient.On("Save", mock.Anything).Return(nil)

				By("Loading a config with the given data object")
				config, err = LoadAgentConfig(mockClient)
				Expect(err).ToNot(HaveOccurred())
				mockV2.AssertMatchesV2(config.data)

				By("Reloading the config with a different data object")
				err = config.Reload()
			})

			It("updates without error", func() {
				Expect(err).ToNot(HaveOccurred())
			})

			It("updates the config's data field correctly", func() {
				alteredMockV2.AssertMatchesV2(config.data)
				Expect(config.data.Version).To(Equal(newVersion))
			})
		})
	})
})
