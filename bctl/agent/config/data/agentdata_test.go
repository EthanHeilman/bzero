package data

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConfigData(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Data Suite")
}

var _ = Describe("Config Data", func() {
	Context("json-encoded", func() {
		When("Stored config data is json-encoded DataV1", func() {
			var jsonErr error
			var v2Data AgentDataV2

			mockV1 := NewMockDataV1()

			BeforeEach(func() {
				v1Bytes, err := json.Marshal(mockV1)
				Expect(err).ToNot(HaveOccurred())

				jsonErr = json.Unmarshal(v1Bytes, &v2Data)
			})

			It("unmarshals without error", func() {
				Expect(jsonErr).ToNot(HaveOccurred())
			})

			It("correctly parses all fields into a V2 object", func() {
				mockV1.AssertMatchesV2(v2Data)
			})
		})

		When("Stored config data is json-encoded dataV2", func() {
			var jsonErr error
			var v2Data AgentDataV2

			mockV2 := NewMockDataV2()

			BeforeEach(func() {
				v2Bytes, err := json.Marshal(mockV2)
				Expect(err).ToNot(HaveOccurred())

				jsonErr = json.Unmarshal(v2Bytes, &v2Data)
			})

			It("unmarshals without error", func() {
				Expect(jsonErr).ToNot(HaveOccurred())
			})

			It("correctly parses all fields into a V2 object", func() {
				mockV2.AssertMatchesV2(v2Data)
			})
		})
	})
})
