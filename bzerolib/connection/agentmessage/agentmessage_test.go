package agentmessage

import (
	"encoding/json"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAgentMessage(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AgentMessage Suite")
}

var _ = Describe("AgentMessage", func() {
	testChannelId := "1234"
	testPayload := "message"
	var testInputMessage, testOutputMessage AgentMessage
	var err error

	Context("Unmarshalling", func() {

		When("Given a legacy MrTAP message", func() {

			BeforeEach(func() {
				testInputMessage = AgentMessage{
					ChannelId:      testChannelId,
					MessageType:    MrtapLegacy,
					SchemaVersion:  CurrentVersion,
					MessagePayload: []byte(testPayload),
				}

				jsonBytes, _ := json.Marshal(testInputMessage)
				err = json.Unmarshal(jsonBytes, &testOutputMessage)
			})

			It("Unmarshals into a MrTAP message", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to unmarshal agent message: %s", err))
				Expect(testOutputMessage.MessageType).To(Equal(Mrtap))
			})
		})

		When("Given a MrTAP message", func() {

			BeforeEach(func() {
				testInputMessage = AgentMessage{
					ChannelId:      testChannelId,
					MessageType:    Mrtap,
					SchemaVersion:  CurrentVersion,
					MessagePayload: []byte(testPayload),
				}

				jsonBytes, _ := json.Marshal(testInputMessage)
				err = json.Unmarshal(jsonBytes, &testOutputMessage)
			})

			It("Unmarshals into a MrTAP message", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to unmarshal agent message: %s", err))
				Expect(testOutputMessage.MessageType).To(Equal(Mrtap))
			})
		})
	})
})
