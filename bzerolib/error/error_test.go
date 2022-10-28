package error

import (
	"encoding/json"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMrtapError(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Mrtap Error Suite")
}

var _ = Describe("Mrtap Error", func() {
	var testInputMessage, testOutputMessage ErrorMessage
	var err error

	Context("Unmarshalling", func() {

		When("Given a legacy MrTAP validation error", func() {

			BeforeEach(func() {
				testInputMessage = ErrorMessage{
					Type: MrtapLegacyValidationError,
				}

				jsonBytes, _ := json.Marshal(testInputMessage)
				err = json.Unmarshal(jsonBytes, &testOutputMessage)
			})

			It("Unmarshals into a MrTAP validation error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to unmarshal agent message: %s", err))
				Expect(testOutputMessage.Type).To(Equal(MrtapValidationError))
			})
		})

		When("Given a MrTAP validation error", func() {

			BeforeEach(func() {
				testInputMessage = ErrorMessage{
					Type: MrtapValidationError,
				}

				jsonBytes, _ := json.Marshal(testInputMessage)
				err = json.Unmarshal(jsonBytes, &testOutputMessage)
			})

			It("Unmarshals into a MrTAP validation error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to unmarshal agent message: %s", err))
				Expect(testOutputMessage.Type).To(Equal(MrtapValidationError))
			})
		})
	})
})
