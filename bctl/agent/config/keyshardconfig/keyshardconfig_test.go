package keyshardconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bastionzero.com/agent/config"
	"bastionzero.com/agent/config/client"
	"bastionzero.com/agent/config/keyshardconfig/data"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func initializeConfigFile(path string, contents string) {
	file, _ := os.Create(path)
	file.WriteString(contents)
}

func expectFileKeySetTo(path string, key data.KeyEntry, expectedEntry data.MappedKeyEntry) {
	rawData, err := os.ReadFile(path)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to read ksConfig file %s: %s", path, err))

	var actual data.KeyShardData
	err = json.Unmarshal(rawData, &actual)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to parse JSON: %s -- raw data: '%s'", err, rawData))

	idx, err := findEntry(actual, key)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to find entry: %s", err))

	expectEntryToEqual(actual.Keys[idx], expectedEntry)
}

func expectEntryToEqual(actual data.MappedKeyEntry, expected data.MappedKeyEntry) {
	Expect(actual.KeyData.KeyShardPem).To(Equal(expected.KeyData.KeyShardPem), "Key PEMs do not match:\nActual: %+v\nExpected: %+v", actual.KeyData.KeyShardPem, expected.KeyData.KeyShardPem)
	Expect(actual.KeyData.CaCertPem).To(Equal(expected.KeyData.CaCertPem), "CA cert PEMs do not match:\nActual: %+v\nExpected: %+v", actual.KeyData.CaCertPem, expected.KeyData.CaCertPem)
	Expect(actual.TargetIds).To(ContainElements(expected.TargetIds), "TargetIds do not match:\nActual: %+v\nExpected: %+v", actual.TargetIds, expected.TargetIds)
}

// note that this suite employs two methods of testing the ksConfig object's behavior. The first is by mocking the underlying client and
// checking that load and save operations occur with the correct data. This is simpler and sufficient for many tests.
// However, for concurrency tests we cannot make guarantees about what data might be loaded and saved in what order, so for these we actually
// create a temporary ksConfig file with a real server client. We populate and check its contents directly.
var _ = Describe("Key Shard Config", Ordered, func() {
	tmpConfigFile := "keyshards.json"
	specialTarget := "targetId-special"

	var checkPath, tempDir string

	Context("Adding entries from an object", func() {
		When("Entry does not exist", Ordered, func() {
			var err error
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with an empty config")
				mockClient.On("FetchKeyShardData").Return(data.KeyShardData{}, nil)
				mockClient.On("Save", data.DefaultMockKeyShardDataSmall()).Return(nil)

				By("adding an entry")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = ksConfig.AddKey(data.DefaultMockKeyShardDataSmall().Keys[0])
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add key shard: %s", err))
			})

			It("Adds the provided entry to config", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("Entry does not exist / huge key", Ordered, func() {
			var err error
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			hugeKey := data.MappedKeyEntry{
				KeyData: data.KeyEntry{KeyShardPem: data.HugeKeyPem()},
			}

			BeforeAll(func() {
				By("starting with an empty config")
				mockClient.On("FetchKeyShardData").Return(data.KeyShardData{}, nil)
				mockClient.On("Save", data.KeyShardData{Keys: []data.MappedKeyEntry{hugeKey}}).Return(nil)

				By("adding a production-size entry")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = ksConfig.AddKey(hugeKey)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add key shard: %s", err))
			})

			It("Adds the provided entry to config", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("entry exists / new target", Ordered, func() {
			var err error
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with one entry")
				currentData := data.DefaultMockKeyShardDataSmall()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)

				newData := data.DefaultMockKeyShardDataSmall()
				newData.Keys[0].TargetIds = append(newData.Keys[0].TargetIds, "targetId3")
				mockClient.On("Save", newData).Return(nil)

				By("adding an entry with a matching key but a new target")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = ksConfig.AddKey(data.DefaultMockKeyEntry3Target())
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add entry: %s", err))
			})

			It("Adds the new targets to the existing entry", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("entry exists / no new targets", Ordered, func() {
			var err error
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with one entry")
				currentData := data.DefaultMockKeyShardDataSmall()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)

				By("adding an entry that matches exactly")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = ksConfig.AddKey(currentData.Keys[0])
			})

			It("Returns a NoOpError without saving", func() {
				Expect(errors.Is(err, &config.NoOpError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Adding many entries at once / different keys", Ordered, func() {
			BeforeAll(func() {
				By("starting with an empty config")
				tempDir = GinkgoT().TempDir()
				checkPath = filepath.Join(tempDir, tmpConfigFile)

				client, _ := client.NewServerClient(tempDir, client.KeyShard)
				ksConfig, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())

				By("adding 12 distinct entries")
				for i := 1; i <= 12; i++ {
					newKey := data.DefaultMockSplitPrivateKey()
					newKey.KeyShardPem = fmt.Sprintf("%d", i)
					go ksConfig.AddKey(data.MappedKeyEntry{
						KeyData:   newKey,
						TargetIds: data.DefaultMockTargetIds(),
					})
				}

				// let the adds happeen
				time.Sleep(1 * time.Second)
			})

			It("Sets all values in the file", func() {
				By("checking that each entry has the data we wrote")
				for i := 1; i <= 12; i++ {
					newKey := data.DefaultMockSplitPrivateKey()
					newKey.KeyShardPem = fmt.Sprintf("%d", i)
					expectFileKeySetTo(checkPath, newKey, data.MappedKeyEntry{
						KeyData:   newKey,
						TargetIds: data.DefaultMockTargetIds(),
					})
				}
			})
		})

		When("Adding many entries at once / same key", Ordered, func() {
			BeforeAll(func() {
				By("starting with an empty config")
				tempDir = GinkgoT().TempDir()
				checkPath = filepath.Join(tempDir, tmpConfigFile)

				client, _ := client.NewServerClient(tempDir, client.KeyShard)
				ksConfig, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())

				By("adding 12 targets to the same entry")
				for i := 1; i <= 12; i++ {
					go ksConfig.AddKey(data.MappedKeyEntry{
						KeyData:   data.DefaultMockSplitPrivateKey(),
						TargetIds: []string{fmt.Sprintf("targetId%d", i)},
					})
				}

				// let the sets happeen
				time.Sleep(1 * time.Second)
			})

			It("Sets all values in the file", func() {
				expectedTargetIds := []string{}
				for i := 1; i <= 12; i++ {
					expectedTargetIds = append(expectedTargetIds, fmt.Sprintf("targetId%d", i))
				}

				By("checking that each entry has the data we wrote")
				expectFileKeySetTo(checkPath, data.DefaultMockSplitPrivateKey(), data.MappedKeyEntry{
					KeyData:   data.DefaultMockSplitPrivateKey(),
					TargetIds: expectedTargetIds,
				})
			})
		})
	})

	Context("Adding targets", func() {
		When("Target is not present in any entry", func() {
			var err error
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with two entries")
				mockData := data.MockKeyShardDataMedium()
				mockClient.On("FetchKeyShardData").Return(mockData, nil)

				newData := data.KeyShardData{}
				newData.Keys = append(newData.Keys, mockData.Keys...)
				for idx := range newData.Keys {
					newData.Keys[idx].TargetIds = append(newData.Keys[idx].TargetIds, specialTarget)
				}
				mockClient.On("Save", newData).Return(nil)

				By("adding a targetid not present in any entry")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = ksConfig.AddTarget(specialTarget)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add target: %s", err))
			})

			It("adds the target to all entries", func() {
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("Target is already present in all entries", func() {
			var err error
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with two entries")
				mockData := data.MockKeyShardDataMedium()
				mockClient.On("FetchKeyShardData").Return(mockData, nil)

				By("adding a targetid not present in any entry")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = ksConfig.AddTarget("targetId1")
			})

			It("returns a NoOpError without saving", func() {
				Expect(errors.Is(err, &config.NoOpError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Target is present in some entires", func() {
			var err error
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with two entries")
				mockData := data.MockKeyShardDataMedium()
				mockData.Keys[0].TargetIds = append(mockData.Keys[0].TargetIds, specialTarget)
				mockClient.On("FetchKeyShardData").Return(mockData, nil)

				newData := data.KeyShardData{}
				newData.Keys = append(newData.Keys, mockData.Keys...)
				newData.Keys[1].TargetIds = append(mockData.Keys[1].TargetIds, specialTarget)
				mockClient.On("Save", newData).Return(nil)

				By("adding a targetid only present in one entry")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = ksConfig.AddTarget(specialTarget)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add target: %s", err))
			})

			It("adds the target to the remaining entries", func() {
				mockClient.AssertExpectations(GinkgoT())
			})
		})
	})

	Context("LastKey", func() {
		When("Target does not exist", Ordered, func() {
			var err error
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with one entry")
				mockClient.On("FetchKeyShardData").Return(data.DefaultMockKeyShardDataSmall(), nil)

				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				_, err = ksConfig.LastKey("targetId1000")
			})

			It("Returns a TargetError without saving", func() {
				Expect(errors.Is(err, &config.TargetError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Target exists in multiple entries", Ordered, func() {
			var err error
			var ksConfig *KeyShardConfig
			var key data.KeyEntry
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with two entries")
				mockClient.On("FetchKeyShardData").Return(data.MockKeyShardDataMedium(), nil)

				By("requesting a target present in both")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				key, err = ksConfig.LastKey("targetId1")
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to get key: %s", err))
			})

			It("returns the most recent key", func() {
				Expect(key.KeyShardPem).To(Equal("101"))
			})
		})

		When("Target only exists in earliest entry", Ordered, func() {
			var err error
			var ksConfig *KeyShardConfig
			var key data.KeyEntry
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with two entries")
				mockData := data.MockKeyShardDataMedium()
				mockData.Keys[0].TargetIds = append(mockData.Keys[0].TargetIds, specialTarget)
				mockClient.On("FetchKeyShardData").Return(mockData, nil)

				By("requesting a target only present in the earlier one")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				key, err = ksConfig.LastKey(specialTarget)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to get key: %s", err))
			})

			It("returns the earlier key", func() {
				Expect(key.KeyShardPem).To(Equal("123"))
			})
		})
	})

	Context("Deleting targets", func() {
		When("Target does not exist", Ordered, func() {
			var err error
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with one entry")
				mockClient.On("FetchKeyShardData").Return(data.DefaultMockKeyShardDataSmall(), nil)

				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = ksConfig.DeleteTarget("target", false)
			})

			It("Returns a TargetError", func() {
				Expect(errors.Is(err, &config.TargetError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Target exists / soft delete", Ordered, func() {
			var err error
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with two entries")
				currentData := data.MockKeyShardDataMedium()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)

				newData := data.MockKeyShardDataMedium()
				newData.Keys[1].TargetIds = []string{"targetId1"}
				mockClient.On("Save", newData).Return(nil)

				By("deleting the most recent instance of one target")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = ksConfig.DeleteTarget("targetId2", false)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to delete target: %s", err))
			})

			It("removes the target from the most recent entry without affecting the other entries", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("Target exists / hard delete", Ordered, func() {
			var err error
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with two entries")
				currentData := data.MockKeyShardDataMedium()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)

				newData := data.MockKeyShardDataMedium()
				newData.Keys[0].TargetIds = []string{"targetId1"}
				newData.Keys[1].TargetIds = []string{"targetId1"}
				mockClient.On("Save", newData).Return(nil)

				By("deleting a target present in both entries")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = ksConfig.DeleteTarget("targetId2", true)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to delete target: %s", err))
			})

			It("removes the target from all entries", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("Deleting many targets at once", Ordered, func() {
			BeforeAll(func() {
				tempDir = GinkgoT().TempDir()
				checkPath = filepath.Join(tempDir, tmpConfigFile)

				By("starting with a ksConfig with many entries")
				initializeConfigFile(checkPath, data.MockKeyShardLargeWithTargetsRaw())

				client, _ := client.NewServerClient(tempDir, client.KeyShard)
				ksConfig, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())

				By("deleting all but one of the targets")
				for i := 1; i <= 8; i++ {
					go ksConfig.DeleteTarget(fmt.Sprintf("targetId%d", i), false)
				}

				// let the deletes happeen
				time.Sleep(1 * time.Second)
			})

			It("deletes all the correct targets", func() {
				By("checking that all of the deleted targets are absent from their original entry")
				newKey := data.DefaultMockSplitPrivateKey()
				newKey.KeyShardPem = "2"
				expectFileKeySetTo(checkPath, newKey, data.MappedKeyEntry{
					KeyData:   newKey,
					TargetIds: []string{},
				})

				newKey.KeyShardPem = "3"
				expectFileKeySetTo(checkPath, newKey, data.MappedKeyEntry{
					KeyData:   newKey,
					TargetIds: []string{},
				})

				newKey.KeyShardPem = "4"
				expectFileKeySetTo(checkPath, newKey, data.MappedKeyEntry{
					KeyData:   newKey,
					TargetIds: []string{},
				})
			})

			It("leaves the un-deleted target", func() {
				keyOne := data.AltMockSplitPrivateKey()
				keyOne.KeyShardPem = "1"
				By("checking that the remaining target is still in its original entry")
				expectFileKeySetTo(checkPath, keyOne, data.MappedKeyEntry{
					KeyData:   keyOne,
					TargetIds: []string{"targetId0"},
				})
			})
		})
	})

	Context("Clearing the config", func() {
		When("There is no data", func() {
			var err error
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with two entries")
				mockClient.On("FetchKeyShardData").Return(data.KeyShardData{}, nil)

				By("clearing the config")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = ksConfig.Clear()
			})

			It("Returns a NoOpError without saving", func() {
				Expect(errors.Is(err, &config.NoOpError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("There is data", func() {
			var err error
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with two entries")
				currentData := data.MockKeyShardDataMedium()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)
				mockClient.On("Save", data.KeyShardData{}).Return(nil)

				By("clearing the config")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = ksConfig.Clear()
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to clear config: %s", err))
			})

			It("removes all entries from the config", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})
	})

	Context("JSON", func() {
		When("Config is populated", func() {
			var err error
			var bytes []byte
			var ksConfig *KeyShardConfig
			mockClient := &client.MockClient{}

			BeforeAll(func() {
				By("starting with a ksConfig with one entry")
				currentData := data.DefaultMockKeyShardDataSmall()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)

				By("printing the config")
				ksConfig, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				bytes, err = json.Marshal(ksConfig)
			})

			It("Marshals a JSON string", func() {
				Expect(string(bytes)).To(Equal(`{"keys":[{"key":{"keyShardPem":"123","caCertPem":""},"targetIds":["targetId1","targetId2"]}]}`))
			})
		})
	})
})
