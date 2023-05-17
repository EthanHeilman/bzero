package pwdb

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/plugin/db/actions/pwdb"
)

func TestGCPConnectorsSQL(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agent GCP Cloud SQL Connection")
}

var _ = Describe("Test Simple Google Cloud SQL Connection", func() {
	logger := logger.MockLogger(GinkgoWriter)

	Context("Starting a GCP Dial", func() {
		When("Connecting to a GCP instance via the GCP dialer", func() {
			// the host is prefixed by gcp, which flags that a gcp dialer should be used
			host := "gcp://fakedb:us-central1:fakedb"
			remotePort := 99999
			targetUser := "alice@fakeproject.iam.gserviceaccount.fake"
			targetId := "faketargetId"
			action := string(pwdb.Connect)
			p := &Pwdb{
				logger:           logger,
				doneChan:         nil,
				keyshardConfig:   nil,
				bastionClient:    nil,
				streamOutputChan: nil,
				remoteHost:       host,
				remotePort:       remotePort,
			}
			err := p.start(targetId, targetUser, action)

			// This isn't the satifying way to test this functionality, but it works. We determine that a GCP
			// connector has been called because it throws an error that idenifies it has been called.
			Expect(err.Error()).To(ContainSubstring("google: could not find default credentials."))
		})
	})
})
