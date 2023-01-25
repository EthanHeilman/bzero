package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/config/client"
	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func initializeConfigFile(path string, contents string) {
	file, _ := os.Create(path)
	file.WriteString(contents)
}

func expectFileKeySetTo(path string, key data.KeyEntry, expectedEntry data.MappedKeyEntry) {
	rawData, err := os.ReadFile(path)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to read config file %s: %s", path, err))

	var actual data.KeyShardData
	err = json.Unmarshal(rawData, &actual)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to parse JSON: %s -- raw data: '%s'", err, rawData))

	idx, err := findEntry(actual, key)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to find entry: %s", err))

	expectEntryToEqual(actual[idx], expectedEntry)
}

func expectEntryToEqual(actual data.MappedKeyEntry, expected data.MappedKeyEntry) {
	Expect(actual.KeyData.KeyShardPem).To(Equal(expected.KeyData.KeyShardPem), "Key PEMs do not match:\nActual: %+v\nExpected: %+v", actual.KeyData.KeyShardPem, expected.KeyData.KeyShardPem)
	Expect(actual.KeyData.CaCertPem).To(Equal(expected.KeyData.CaCertPem), "CA cert PEMs do not match:\nActual: %+v\nExpected: %+v", actual.KeyData.CaCertPem, expected.KeyData.CaCertPem)
	Expect(actual.TargetIds).To(ContainElements(expected.TargetIds), "TargetIds do not match:\nActual: %+v\nExpected: %+v", actual.TargetIds, expected.TargetIds)
}

// note that this suite employs two methods of testing the config object's behavior. The first is by mocking the underlying client and
// checking that load and save operations occur with the correct data. This is simpler and sufficient for many tests.
// However, for concurrency tests we cannot make guarantees about what data might be loaded and saved in what order, so for these we actually
// create a temporary config file with a real systemd client. We populate and check its contents directly.
var _ = Describe("Key Shard Config", Ordered, func() {
	tmpConfigFile := "keyshards.json"

	var checkPath, tempDir string

	Context("Adding entries", func() {
		When("Entry does not exist", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with an empty config")
				mockClient.On("FetchKeyShardData").Return(data.KeyShardData{}, nil)
				mockClient.On("Save", data.DefaultMockKeyShardDataSmall()).Return(nil)

				By("adding an entry")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddKey(data.DefaultMockKeyShardDataSmall()[0])
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
			var config *KeyShardConfig
			mockClient := &MockClient{}

			hugeKey := data.MappedKeyEntry{
				KeyData: data.KeyEntry{KeyShardPem: data.HugeKeyPem()},
			}

			BeforeAll(func() {
				By("starting with an empty config")
				mockClient.On("FetchKeyShardData").Return(data.KeyShardData{}, nil)
				mockClient.On("Save", data.KeyShardData{hugeKey}).Return(nil)

				By("adding a production-size entry")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddKey(hugeKey)
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
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with one entry")
				currentData := data.DefaultMockKeyShardDataSmall()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)

				newData := data.DefaultMockKeyShardDataSmall()
				newData[0].TargetIds = append(newData[0].TargetIds, "targetId3")
				mockClient.On("Save", newData).Return(nil)

				By("adding an entry with a matching key but a new target")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddKey(data.DefaultMockKeyEntry3Target())
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
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with one entry")
				currentData := data.DefaultMockKeyShardDataSmall()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)

				By("adding an entry that matches exactly")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddKey(currentData[0])
			})

			It("Returns a NoOpError without saving", func() {
				Expect(errors.Is(err, &NoOpError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Adding many entries at once / different keys", Ordered, func() {
			BeforeAll(func() {
				By("starting with an empty config")
				tempDir = GinkgoT().TempDir()
				checkPath = filepath.Join(tempDir, tmpConfigFile)

				client, _ := client.NewSystemdClient(tempDir, client.KeyShard)
				config, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())

				By("adding 12 distinct entries")
				for i := 1; i <= 12; i++ {
					newKey := data.DefaultMockSplitPrivateKey()
					newKey.KeyShardPem = fmt.Sprintf("%d", i)
					go config.AddKey(data.MappedKeyEntry{
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

				client, _ := client.NewSystemdClient(tempDir, client.KeyShard)
				config, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())

				By("adding 12 targets to the same entry")
				for i := 1; i <= 12; i++ {
					go config.AddKey(data.MappedKeyEntry{
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

	Context("LastKey", func() {
		When("Target does not exist", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with one entry")
				mockClient.On("FetchKeyShardData").Return(data.DefaultMockKeyShardDataSmall(), nil)

				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				_, err = config.LastKey("targetId1000")
			})

			It("Returns a TargetError without saving", func() {
				Expect(errors.Is(err, &TargetError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Target exists in multiple entries", Ordered, func() {
			var err error
			var config *KeyShardConfig
			var key data.KeyEntry
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with two entries")
				mockClient.On("FetchKeyShardData").Return(data.MockKeyShardDataMedium(), nil)

				By("requesting a target present in both")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				key, err = config.LastKey("targetId1")
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
			var config *KeyShardConfig
			var key data.KeyEntry
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with two entries")
				specialTarget := "targetId-special"
				mockData := data.MockKeyShardDataMedium()
				mockData[0].TargetIds = append(mockData[0].TargetIds, specialTarget)
				mockClient.On("FetchKeyShardData").Return(mockData, nil)

				By("requesting a target only present in the earlier one")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				key, err = config.LastKey(specialTarget)
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
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with one entry")
				mockClient.On("FetchKeyShardData").Return(data.DefaultMockKeyShardDataSmall(), nil)

				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.DeleteTarget("target", false)
			})

			It("Returns a TargetError", func() {
				Expect(errors.Is(err, &TargetError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Target exists / soft delete", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with two entries")
				currentData := data.MockKeyShardDataMedium()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)

				newData := data.MockKeyShardDataMedium()
				newData[1].TargetIds = []string{"targetId1"}
				mockClient.On("Save", newData).Return(nil)

				By("deleting the most recent instance of one target")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.DeleteTarget("targetId2", false)
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
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with two entries")
				currentData := data.MockKeyShardDataMedium()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)

				newData := data.MockKeyShardDataMedium()
				newData[0].TargetIds = []string{"targetId1"}
				newData[1].TargetIds = []string{"targetId1"}
				mockClient.On("Save", newData).Return(nil)

				By("deleting a target present in both entries")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.DeleteTarget("targetId2", true)
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

				By("starting with a config with many entries")
				initializeConfigFile(checkPath, data.MockKeyShardLargeWithTargetsRaw())

				client, _ := client.NewSystemdClient(tempDir, client.KeyShard)
				config, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())

				By("deleting all but one of the targets")
				for i := 1; i <= 8; i++ {
					go config.DeleteTarget(fmt.Sprintf("targetId%d", i), false)
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
})
