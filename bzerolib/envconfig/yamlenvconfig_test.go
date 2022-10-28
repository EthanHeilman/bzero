package error

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestYamlEnvConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Yaml EnvConfig Suite")
}

/*
TODO:
  - Setting...
  - Getting
  - Deleting
  - Concurrency
  - Error handling
*/
var _ = Describe("Yaml EnvConfig", func() {

	Context("Setting", func() {

		When("File does not exist / env var is not set", func() {

			BeforeEach(func() {
			})

			It("Creates a file / env var with the initial entry", func() {
			})
		})
	})
})
