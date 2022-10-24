package config

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Suite")
}

var _ = Describe("Config", func() {
	sampleLegacyConfig := `{"PublicKey":"7gL7LxbqnIEztKVFM2Fo422H9qMjBqSxU4UDwaQA33U=","PrivateKey":"rCGYlJmxw8MZxXDGR6/nYfeoNvFnsZRcXiJHsm/BOPzuAvsvFuqcgTO0pUUzYWjjbYf2oyMGpLFThQPBpADfdQ==","ServiceUrl":"https://lucie.bastionzero.com","TargetName":"","Namespace":"","IdpProvider":"google","IdpOrgId":"bastionzero.com","TargetId":"5b8f9f7c-2ac5-4143-9db4-fa013c438725","EnvironmentId":"","EnvironmentName":"","AgentType":"","Version":"$AGENT_VERSION","ShutdownReason":"control channel stopped processing pongs","ShutdownState":"map[goarch:amd64 goos:linux targetHostName:ip-172-31-81-178.ec2.internal targetId:5b8f9f7c-2ac5-4143-9db4-fa013c438725 targetType:bzero version:$AGENT_VERSION]","AgentIdentityToken":"eyJhbGciOiJFUzI1NiIsImtpZCI6IjViOTM1OTcxLWI3NTctNGZlYS05MzM0LWY2MDg0MTk3MTA0YyIsInR5cCI6IkpXVCJ9.eyJzdWIiOiI1YjhmOWY3Yy0yYWM1LTQxNDMtOWRiNC1mYTAxM2M0Mzg3MjUiLCJhZ2VudFB1YmxpY0tleSI6IjgyLzBEOG1HOUlHR09EWG0vTEp1ZFZaWUZBNnpoUDhkcm10OG9HbEFyUGM9IiwiYXVkIjoiY29ubmVjdGlvbi1zZXJ2aWNlIiwiZXhwIjoxNjY3MjMyMDUyLCJpc3MiOiJodHRwczovL2x1Y2llLmJhc3Rpb256ZXJvLmNvbSIsImlhdCI6MTY2NjYyNzI1MiwibmJmIjoxNjY2NjI3MjUyfQ.h4rqOP-v7bWd3pytmZBgrvtSL5Tlm3URqLMKmwejPtqSZSkqKUFJn_5Ajsyr1lh1cmOfCbI8Q0YmX3pESQY-NA"}`
	sampleNewConfig := `{"Version":"$AGENT_VERSION","AgentType":"","PublicKey":"7gL7LxbqnIEztKVFM2Fo422H9qMjBqSxU4UDwaQA33U=","PrivateKey":"rCGYlJmxw8MZxXDGR6/nYfeoNvFnsZRcXiJHsm/BOPzuAvsvFuqcgTO0pUUzYWjjbYf2oyMGpLFThQPBpADfdQ==","AgentIdentityToken":"eyJhbGciOiJFUzI1NiIsImtpZCI6IjViOTM1OTcxLWI3NTctNGZlYS05MzM0LWY2MDg0MTk3MTA0YyIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJmMmVkN2MyMC1iZjNhLTQyNzItYWFlOC1jZTA3YTc0YmUzNzIiLCJhZ2VudFB1YmxpY0tleSI6IjdnTDdMeGJxbklFenRLVkZNMkZvNDIySDlxTWpCcVN4VTRVRHdhUUEzM1U9IiwiYXVkIjoiY29ubmVjdGlvbi1zZXJ2aWNlIiwiZXhwIjoxNjY2OTk2MDYxLCJpc3MiOiJodHRwczovL2x1Y2llLmJhc3Rpb256ZXJvLmNvbSIsImlhdCI6MTY2NjM5MTI2MSwibmJmIjoxNjY2MzkxMjYxfQ.NvBhpCLb6rF78b6KpVrVosqRmpo5HwxrVaoiLdFA2SNn72NdAr7zUBGTkllyPUTDgYxU0UDzyDrMjrH63YWFuw","TargetId":"f2ed7c20-bf3a-4272-aae8-ce07a74be372","IdpProvider":"google","IdpOrgId":"bastionzero.com","ServiceUrl":"https://lucie.bastionzero.com","ShutdownReason":"control channel stopped processing pongs","ShutdownState":{"goarch":"amd64","goos":"linux","targetHostName":"ip-172-31-81-178.ec2.internal","targetId":"f2ed7c20-bf3a-4272-aae8-ce07a74be372","targetType":"bzero","version":"$AGENT_VERSION"}}`

	Context("Config Data", func() {
		When("Reading from old config", func() {
			var err error
			var configData data

			BeforeEach(func() {
				err = json.Unmarshal([]byte(sampleLegacyConfig), &configData)
			})

			It("unmarshals without error", func() {
				Expect(err).ToNot(HaveOccurred())
			})

			It("reads legacy-formatted state as an empty map", func() {
				Expect(configData.ShutdownState).To(Equal(map[string]string{}))
			})
		})

		When("Reading from new config", func() {
			var err error
			var configData data

			BeforeEach(func() {
				err = json.Unmarshal([]byte(sampleNewConfig), &configData)
			})

			It("unmarshals without error", func() {
				Expect(err).ToNot(HaveOccurred())
			})

			It("populates the shutdown state", func() {
				Expect(configData.ShutdownState["goos"]).To(Equal("linux"))
			})
		})
	})
})
