package client

import (
	"os"
	"path"

	"github.com/gofrs/flock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	"bastionzero.com/bctl/v1/bzerolib/filelock"
)

/*
To test our systemd config, we need to create a bunch of config files that we want
to keep separate for the purpose of test isolation. This requires us to have a universal
BeforeEach and a universal AfterEach to make sure that the created file is always deleted.
*/

var _ = Describe("Systemd Client", func() {
	var configFile *os.File
	var fileLock *flock.Flock

	populateConfigFile := func(client *SystemdClient, mockV2 data.DataV2) error {
		By("Fetching our file to set our last mod correctly")
		_, err := client.Fetch()
		Expect(err).ToNot(HaveOccurred())

		By("Saving data to our config file")
		return client.Save(mockV2)
	}

	BeforeEach(func() {
		var err error

		// Create our temp directory
		By("Creating a temporary config file")
		configFile, err = os.CreateTemp("", configFileName)
		Expect(err).ToNot(HaveOccurred())
		By("Creating a new temp config file: " + configFile.Name())

		By("Instantiating our file lock")
		dir := path.Dir(configFile.Name())
		lock := filelock.NewFileLock(path.Join(dir, configFileLockName))
		fileLock, err = lock.NewLock()
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// Make sure we remove the file
		os.Remove(configFile.Name())
		By("Deleting the temp config file: " + configFile.Name())
	})

	Context("New", func() {
		When("The config file does not exist", func() {
			var client *SystemdClient
			var err error

			testDir := path.Join(os.TempDir(), "bzero")

			BeforeEach(func() {
				client, err = NewSystemdClient(testDir)
				By("Creating a new config file: " + client.configPath)
			})

			AfterEach(func() {
				os.RemoveAll(testDir)
				By("Deleting the config file: " + client.configPath)
			})

			It("instantiates without error", func() {
				Expect(err).ToNot(HaveOccurred())
			})

			It("creates a new, empty config file", func() {
				By("Fetching our newly created config file data")
				dataV2, err := client.Fetch()
				Expect(err).ToNot(HaveOccurred())

				By("Making sure our data object is an empty one")
				Expect(dataV2).To(Equal(data.DataV2{}))
			})
		})

		When("The config file exists", func() {
			var err error
			var client *SystemdClient

			mockV2 := data.NewMockDataV2()

			BeforeEach(func() {
				sysdClient := &SystemdClient{
					configPath: configFile.Name(),
					fileLock:   fileLock,
				}

				err = populateConfigFile(sysdClient, mockV2)
				Expect(err).ToNot(HaveOccurred())

				client, err = NewSystemdClient(path.Dir(sysdClient.configPath))
			})

			It("instantiates without error", func() {
				Expect(err).ToNot(HaveOccurred())
			})

			It("returns a properly instantiated client object", func() {
				By("Fetching our newly created config file data")
				dataV2, err := client.Fetch()
				Expect(err).ToNot(HaveOccurred())

				By("Making sure our data object is an empty one")
				Expect(dataV2).To(Equal(data.DataV2{}))
			})
		})
	})

	Context("Save", func() {
		When("Saving a new config", func() {
			var saveErr error
			var v2Data data.DataV2

			mockV2 := data.NewMockDataV2()

			BeforeEach(func() {
				sysdClient := &SystemdClient{
					configPath: configFile.Name(),
					fileLock:   fileLock,
				}

				saveErr = populateConfigFile(sysdClient, mockV2)

				var fetchErr error
				v2Data, fetchErr = sysdClient.Fetch()
				Expect(fetchErr).ToNot(HaveOccurred())
			})

			It("saves without error", func() {
				Expect(saveErr).ToNot(HaveOccurred())
			})

			It("saves the data object to the config file", func() {
				mockV2.AssertMatchesV2(v2Data)
			})
		})
	})

	Context("Fetch", func() {
		When("Config file is empty", func() {
			var v2Data data.DataV2
			var err error

			BeforeEach(func() {
				sysdClient := &SystemdClient{
					configPath: configFile.Name(),
					fileLock:   fileLock,
				}

				v2Data, err = sysdClient.Fetch()
			})

			It("fetches without error", func() {
				Expect(err).ToNot(HaveOccurred())
			})

			It("returns an empty data object", func() {
				Expect(v2Data).To(Equal(data.DataV2{}))
			})
		})

		When("Config file contains V2 data", func() {
			var v2Data data.DataV2
			var err error

			mockV2 := data.NewMockDataV2()

			BeforeEach(func() {
				sysdClient := &SystemdClient{
					configPath: configFile.Name(),
					fileLock:   fileLock,
				}

				err = populateConfigFile(sysdClient, mockV2)
				Expect(err).ToNot(HaveOccurred())

				v2Data, err = sysdClient.Fetch()
			})

			It("fetches without error", func() {
				Expect(err).ToNot(HaveOccurred())
			})

			It("returns a correctly populated V2 object", func() {
				mockV2.AssertMatchesV2(v2Data)
			})
		})
	})

	Context("Concurrency", func() {
		When("Config file is written between fetch and save", func() {
			var err error

			BeforeEach(func() {
				sysdClient := &SystemdClient{
					configPath: configFile.Name(),
					fileLock:   fileLock,
				}

				By("Saving data to our config file")
				err = sysdClient.Save(data.DataV2{})
			})

			It("returns an error", func() {
				Expect(err).To(HaveOccurred())
			})
		})
	})
})
