package client

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"testing"

	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConfigClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Client Suite")
}

var _ = Describe("Kubernetes Client", func() {
	Context("Decoding", func() {
		When("Reading from a V1 gob-encoded config", func() {
			var v2Data data.DataV2
			var decodeErr error

			mockV1 := data.NewMockDataV1()

			BeforeEach(func() {
				v1Bytes, err := gobEncode(mockV1)
				Expect(err).ToNot(HaveOccurred())

				v2Data, decodeErr = decode(v1Bytes)
			})

			It("decodes without error", func() {
				Expect(decodeErr).ToNot(HaveOccurred())
			})

			It("parses all fields correctly into a V2 data object", func() {
				mockV1.AssertMatchesV2(v2Data)
			})
		})

		When("Reading from a V2 json-encoded config", func() {
			var v2Data data.DataV2
			var decodeErr error

			mockV2 := data.NewMockDataV2()

			BeforeEach(func() {
				v2Bytes, err := json.Marshal(mockV2)
				Expect(err).ToNot(HaveOccurred())

				v2Data, decodeErr = decode(v2Bytes)
			})

			It("decodes without error", func() {
				Expect(decodeErr).ToNot(HaveOccurred())
			})

			It("parses all fields verbatim into a V2 data object", func() {
				mockV2.AssertMatchesV2(v2Data)
			})
		})
	})
})

func gobEncode(p interface{}) ([]byte, error) {
	buf := bytes.Buffer{}
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(p)
	return buf.Bytes(), err
}
