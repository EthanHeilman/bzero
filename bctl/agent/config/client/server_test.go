package client

import (
	"os"
	"path"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentdata "bastionzero.com/agent/config/agentconfig/data"
	ksdata "bastionzero.com/agent/config/keyshardconfig/data"
	"bastionzero.com/bzerolib/filelock"
)

var _ = Describe("Server Client", Ordered, func() {
	var agentConfigFile *os.File
	var keyShardConfigFile *os.File
	var fileLock *filelock.FileLock
	var tmpDir string

	BeforeAll(func() {
		// Gingko will give us a temp dir and then cleanup after itself so we don't have
		// to worry about dangling files or test parallelization issues
		tmpDir = GinkgoT().TempDir()
	})

	populateAgentConfigFiile := func(client *ServerClient, mockV2 agentdata.AgentDataV2) error {
		By("Fetching our file to set our last mod correctly")
		_, err := client.FetchAgentData()
		Expect(err).ToNot(HaveOccurred())

		By("Saving data to our config file")
		return client.Save(mockV2)
	}

	populateKeyShardConfigFiile := func(client *ServerClient, mockData ksdata.KeyShardData) error {
		By("Fetching our file to set our last mod correctly")
		_, err := client.FetchKeyShardData()
		Expect(err).ToNot(HaveOccurred())

		By("Saving data to our config file")
		return client.Save(mockData)
	}

	BeforeEach(func() {
		var err error

		// Create our temp directory
		By("Creating a temporary config file")
		agentConfigFile, err = os.Create(path.Join(tmpDir, agentConfigFileName))
		Expect(err).ToNot(HaveOccurred())
		keyShardConfigFile, err = os.Create(path.Join(tmpDir, keyShardConfigFileName))
		Expect(err).ToNot(HaveOccurred())
		By("Creating a new temp config file: " + agentConfigFile.Name())

		By("Instantiating our file lock")
		dir := path.Dir(agentConfigFile.Name())
		fileLock = filelock.NewFileLock(path.Join(dir, configFileLockName))
	})

	Context("New", func() {
		When("The config file does not exist / Agent config", func() {
			var client *ServerClient
			var err error

			testDir := path.Join(os.TempDir(), "bzero")

			BeforeEach(func() {
				client, err = NewServerClient(testDir, Agent)
				Expect(err).ToNot(HaveOccurred())
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
				dataV2, err := client.FetchAgentData()
				Expect(err).ToNot(HaveOccurred())

				By("Making sure our data object is an empty agent config")
				Expect(dataV2).To(Equal(agentdata.AgentDataV2{}))
			})
		})

		When("The config file does not exist / KeyShard config", func() {
			var client *ServerClient
			var err error

			testDir := path.Join(os.TempDir(), "bzero")

			BeforeEach(func() {
				client, err = NewServerClient(testDir, KeyShard)
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
				ksData, err := client.FetchKeyShardData()
				Expect(err).ToNot(HaveOccurred())

				By("Making sure our data object is an empty keyshard config")
				var emptyData ksdata.KeyShardData
				Expect(ksData).To(Equal(emptyData))
			})
		})

		When("The config file exists / Agent config", func() {
			var err error
			var client *ServerClient

			BeforeEach(func() {
				sysdClient := &ServerClient{
					configPath: agentConfigFile.Name(),
					fileLock:   fileLock,
					configType: Agent,
				}

				err = populateAgentConfigFiile(sysdClient, agentdata.NewMockDataV2())
				Expect(err).ToNot(HaveOccurred())
				client, err = NewServerClient(path.Dir(sysdClient.configPath), Agent)
			})

			It("instantiates without error", func() {
				Expect(err).ToNot(HaveOccurred())
			})

			It("returns a properly instantiated client object", func() {
				By("Fetching our newly created config file data")
				dataV2, err := client.FetchAgentData()
				Expect(err).ToNot(HaveOccurred())

				By("Making sure our data object is populated")
				Expect(dataV2).To(Equal(agentdata.NewMockDataV2()))
			})
		})

		When("The config file exists / Keyshard config", func() {
			var err error
			var client *ServerClient

			BeforeEach(func() {
				sysdClient := &ServerClient{
					configPath: keyShardConfigFile.Name(),
					fileLock:   fileLock,
					configType: KeyShard,
				}

				err = populateKeyShardConfigFiile(sysdClient, ksdata.DefaultMockKeyShardDataSmall())
				Expect(err).ToNot(HaveOccurred())

				client, err = NewServerClient(path.Dir(sysdClient.configPath), KeyShard)
			})

			It("instantiates without error", func() {
				Expect(err).ToNot(HaveOccurred())
			})

			It("returns a properly instantiated client object", func() {
				By("Fetching our newly created config file data")
				dataKs, err := client.FetchKeyShardData()
				Expect(err).ToNot(HaveOccurred())

				By("Making sure our data object is populated")
				Expect(dataKs).To(Equal(ksdata.DefaultMockKeyShardDataSmall()))
			})
		})
	})

	Context("Save", func() {
		When("Saving a new agent config", func() {
			var saveErr error
			var v2Data agentdata.AgentDataV2

			mockV2 := agentdata.NewMockDataV2()

			BeforeEach(func() {
				sysdClient := &ServerClient{
					configPath: agentConfigFile.Name(),
					fileLock:   fileLock,
					configType: Agent,
				}

				saveErr = populateAgentConfigFiile(sysdClient, mockV2)

				var fetchErr error
				v2Data, fetchErr = sysdClient.FetchAgentData()
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
			var v2Data agentdata.AgentDataV2
			var err error

			BeforeEach(func() {
				sysdClient := &ServerClient{
					configPath: agentConfigFile.Name(),
					fileLock:   fileLock,
					configType: Agent,
				}

				v2Data, err = sysdClient.FetchAgentData()
			})

			It("fetches without error", func() {
				Expect(err).ToNot(HaveOccurred())
			})

			It("returns an empty data object", func() {
				Expect(v2Data).To(Equal(agentdata.AgentDataV2{}))
			})
		})

		When("Config file contains V2 data", func() {
			var v2Data agentdata.AgentDataV2
			var err error

			mockV2 := agentdata.NewMockDataV2()

			BeforeEach(func() {
				sysdClient := &ServerClient{
					configPath: agentConfigFile.Name(),
					fileLock:   fileLock,
					configType: Agent,
				}

				err = populateAgentConfigFiile(sysdClient, mockV2)
				Expect(err).ToNot(HaveOccurred())

				v2Data, err = sysdClient.FetchAgentData()
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
				sysdClient := &ServerClient{
					configPath: agentConfigFile.Name(),
					fileLock:   fileLock,
					configType: Agent,
				}

				By("Saving data to our config file")
				err = sysdClient.Save(agentdata.AgentDataV2{})
			})

			It("returns an error", func() {
				Expect(err).To(HaveOccurred())
			})
		})
	})
})
