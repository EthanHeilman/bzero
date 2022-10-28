package message

import (
	"encoding/json"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMrtapMessage(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MrtapMessage Suite")
}

var _ = Describe("MrtapMessage", func() {
	testSig := "abc"
	testTargetId := "1234"
	testNonce := "xxx"
	var testOutputMessage MrtapMessage
	var err error

	Context("Marshalling", func() {
		var outputBytes []byte

		When("Given a MrTAP message", func() {
			BeforeEach(func() {
				outputBytes, err = json.Marshal(MrtapMessage{
					Type:      SynAck,
					Signature: testSig,
					Payload: SynAckPayload{
						Nonce: testNonce,
					},
				})
			})

			It("Adds a keysplittingPayload field", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to marshal message: %s", err))
				Expect(outputBytes).To(Equal([]byte(fmt.Sprintf(`{"keysplittingPayload":{"schemaVersion":"","type":"","action":"","actionResponsePayload":null,"timestamp":"","targetPublicKey":"","nonce":"%s","hPointer":""},"type":"SynAck","payload":{"schemaVersion":"","type":"","action":"","actionResponsePayload":null,"timestamp":"","targetPublicKey":"","nonce":"%s","hPointer":""},"signature":"%s"}`, testNonce, testNonce, testSig))))
			})
		})

		When("Given a poniter to a MrTAP message", func() {
			BeforeEach(func() {
				outputBytes, err = json.Marshal(&MrtapMessage{
					Type:      SynAck,
					Signature: testSig,
					Payload: SynAckPayload{
						Nonce: testNonce,
					},
				})
			})

			It("Adds a keysplittingPayload field", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to marshal message: %s", err))
				Expect(outputBytes).To(Equal([]byte(fmt.Sprintf(`{"keysplittingPayload":{"schemaVersion":"","type":"","action":"","actionResponsePayload":null,"timestamp":"","targetPublicKey":"","nonce":"%s","hPointer":""},"type":"SynAck","payload":{"schemaVersion":"","type":"","action":"","actionResponsePayload":null,"timestamp":"","targetPublicKey":"","nonce":"%s","hPointer":""},"signature":"%s"}`, testNonce, testNonce, testSig))))
			})
		})
	})

	Context("Unmarshalling", func() {
		var inputBytes []byte

		When("Given a <7.2.0 MrTAP message with only a keysplittingPayload", func() {
			BeforeEach(func() {
				inputBytes = []byte(fmt.Sprintf(`{"type": "Syn", "signature": "%s", "keysplittingPayload": {"targetId": "%s"}}`, testSig, testTargetId))
				err = json.Unmarshal(inputBytes, &testOutputMessage)
			})

			It("Unmarshals into a valid MrTAP message", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to unmarshal MrTAP message: %s", err))
				synPayload := testOutputMessage.Payload.(SynPayload)
				Expect(synPayload.TargetId).To(Equal(testTargetId))
			})
		})

		When("Given a >=7.2.0 MrTAP message with both a payload and a keysplittingPayload", func() {
			It("Unmarshals into a valid MrTAP message", func() {
				inputBytes = []byte(fmt.Sprintf(`{"type": "Syn", "signature": "%s", "keysplittingPayload": {"targetId": "%s"}, "payload": {"targetId": "%s"}}`, testSig, testTargetId, testTargetId))
				err = json.Unmarshal(inputBytes, &testOutputMessage)
			})

			It("Unmarshals into a valid MrTAP message", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to unmarshal MrTAP message: %s", err))
				synPayload := testOutputMessage.Payload.(SynPayload)
				Expect(synPayload.TargetId).To(Equal(testTargetId))
			})
		})

		When("Given a futuristic MrTAP message with only a payload", func() {
			BeforeEach(func() {
				inputBytes = []byte(fmt.Sprintf(`{"type": "Syn", "signature": "%s", "payload": {"targetId": "%s"}}`, testSig, testTargetId))
				err = json.Unmarshal(inputBytes, &testOutputMessage)
			})

			It("Unmarshals into a valid MrTAP message", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to unmarshal MrTAP message: %s", err))
				synPayload := testOutputMessage.Payload.(SynPayload)
				Expect(synPayload.TargetId).To(Equal(testTargetId))
			})
		})
	})
})
