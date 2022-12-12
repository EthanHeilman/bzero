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

func expectFileKeyUnset(path string, key data.KeyEntry) {
	rawData, err := os.ReadFile(path)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to read config file %s: %s", path, err))

	var actual data.KeyShardData
	err = json.Unmarshal(rawData, &actual)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to parse JSON: %s -- raw data: '%s'", err, rawData))

	_, err = findEntry(actual, key)
	Expect(errors.Is(err, &KeyError{}), fmt.Sprintf("expected entry to be unset but got err: %s", err))
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
				KeyData: data.KeyEntry{KeyShardPem: `-----BEGIN RSA SPLIT PRIVATE KEY-----
MIIJszCCBNoEggTRODEzNzYwMzU1MDc5ODk1NDkwMzQzNTY4MDU4NDA5NDgzODEz
MjMzNTEwMDM0Mzc1MTIyMDc2MzIwNzY2NTA4NTUzNDAwNDA0NDU3MTkwNzM5NDk0
MTUyMDE2ODk3MzQyMjQ4OTQzNjI1MzE1NjI5NjU0NDkzMDQzMjQ2NDI4MTAzNTEw
MzgwNDUxNzU1MDEwNTg0MzAyMzY2MDAzNjEzOTc5NDAyNDg5ODE1OTA2MjU0MTIz
NzQ5MTA2OTE2NDcwOTQwNTA3MjMwMTAxMDkwNzg4MTE1OTE1Njk3OTgyNTg1Nzg2
NTc3MDYzNTA3MDI5NzA3NzY1NDczMDIxNjQyNzY2MDUzODU3NTg0NDIzNDk3Mjg1
MDE2MzEyNzc5ODg0ODgxNzM0OTk1OTU2NjM2MjYyNDI4NzQwNjU4NDc2MDM2ODQy
ODUzNDIxMTExODM3MDk4Mjg1MzAwMjc1NzY1MDMwNjI2MDYxMzM1NzU5MDQ3OTM1
NTU5NDMwNzczMzMyOTk4OTI4MjcyNzY0NDI3MDIzNTE2MzUxMDY1ODM1MDU4NzI2
NTgwMTAzNDgyODQ1Mzg3NjIxNjUyMDM3NDE2MTE2MzM3NjA0Mjk0NzMxOTcwMTU1
MzY3ODgzNjAzNzk2MjIyNzIxODM4NzA5MjIxNTc4Mjc3Mjk4MDc5MDY4MjU1NDI4
OTkyODc0MTE3NTQxMzM3NzM1NTE0NDQzNzAzOTU5MjI5MjYyNTc2NDQ1MTU4MTU0
NDAzMDgzNDg4Njg5MzUzODg1MDA2MTI4ODE0MjU1NjIyNjAwMzg1ODU1MzY2NjI5
NjI1NTIwNDE5MzA0NTE4NzYyODkxMzc4MzYzMzk0MTgwMzMxNTIyMTYwMjQ4NzI1
MjkzNDIyNDMzMDU4NDgwNDMwMjIyOTgyMDY3Nzc1NzgyMzU4NTU4OTA2ODYxODYx
MTc1NTA4NTI0MDE0NjIxMDc2MzgzOTMzNzE2NTg2MjAxMzAxMDAxODM4NTA4NTc2
NzU2MTQzMDk1NzI5NDA4MjUzMzU0NTkxNjc3NDQ0MDEwMjcxMTE2NjMwMDUwODg0
MTI2NDEwMjA5NTE1OTQ3NDIzMDIyMTczMzMzNDI5MTgwNjA0NzgxNjA4MDg4MzI4
NjIyMDQzMjkwMzU0NjUxMTY1NTE0MDY0MzYxNTY0NjgzMjA3NTU5MzE0NTYwNjcz
OTE0MjM4MzIzNjAxMjI5ODgwODQ2NTY1MDc5NjI2NjE3NDA1MzE4MjI1MTgwMTA5
NDkxNjQxOTY0NjI3MjQ5NzE4MTAzNTQ0MjE4MzM1MTU5MzQ5NzE4NjQzMTkyOTky
MjkzMTY0MTI3MDc0MTQ0NzM5ODAwNzkzNTczNTEyODc2ODgwMTc0MzQ0NTMwMjI4
NDk4OTQwODczNTIxMjc1MDE3OTgwNDc5NTI0NjI4NzY0MjIxMzcxNzE2NTIwODIy
ODU1NTkxMzE0NDkzMzE5NzY3MzY3NDMxMzk4OTMxMTE5ODUxOTc3NTA2NzI5NTQz
MzEyMDQ3OTUxODk5NjA0NDM5MDMwMDE5NDkxMjYwMDgxMDQwOTkwOTM4OTY3NzE2
MTU5MzU4MzkyMjI3MzM0MzY1MDM5MzcyMTk4OTg2MjY4MDk2MjA5NDU5ODQ5AgMB
AAEEggTRNDYxNTc5OTY4OTk1MzAyMjA2OTAzNjY4NzY3NzA3MjEwOTY0NzM1MDg1
NDkyNTIyNjM1NjI4OTU3MTk4OTU3NTgzMTExNDU0MjQ0OTQwMTgzNzcxNjc0NjAx
NzI1MzY0MTgwOTEyOTQyMjMxMDY1NDU0NjY4MzA3Mzc4MjU1ODQzMTU0Mjk3MDUx
NTU1NzU4MzIwOTk0NzU2ODE2MDIwNTM5MTU4NzM1OTA5MjQ3NDgyNzU5ODA2MjY4
MDkzNzUxODk1NzMxMjkxMTk3NTQ1NzkxMTY1MDkwOTM5OTMyMDE1NjQwMDMxNjU0
NjkyMjM5NjU1ODQ0NzQwNjA1NzM2ODIxMDkyOTY3MTc1MjczMDM5ODc3MjI2MTQ0
NDMyMzQ3MjA2NTMwMjg0NTk0NzA4NzAyOTkxNTg1MDg1MTg4NTc1NDA0MDcyNjQ1
Mzg2NzcxMDkyMjQ1OTI2OTIxMDUzNDgxNzY3NDAyODM4ODI2MDYyOTU3OTgzOTY1
NTM4NDkyMDYzNjk4NzMzNjU5Mzg2MDk3NTM5NjE3ODczMzc0MjU2MTc2Mjc2ODI2
OTM2MTA5MzY5NzUwNzY1Nzc2ODIxNjQxNjkzMTgwOTgwMzczNzczNzUyMDcyMTYy
NjkxNDIzMzIyMTU3NDI4NDQ5NjA3MDU3Njc5MzcwODYxNTIwMzcwNzU5Mjk2MDk2
NzcyMjgyMjg1NDIyOTkyNDE1NDk0OTEzOTQ1NzAwNjkxOTU0MTUzNzU4OTgwOTA5
MDc4MzY0NDEyNzc4OTM4NDAyMTY1NjY2NjMwMTAxNTg4ODk0NTIwNzM5MzEwMTU2
MDU4MTYwNjU0MzU2NjA2ODMzMTAxMzg0MDgxNjY1MjI1OTM2MTg3ODU3ODYxODky
MzI3MzE3OTI4ODA5NDEyNzQyODkwMjgwNTk0Nzc0ODYwNDg1MDM1ODE2MDg3MTEw
NTA5ODU0NTg2NTQwMDU0NjkwMDMwNTAwNTUwNjMwNjQ0NDU3MjE1MjkwODI3MjAw
MTE1MTg4MjI0NzAxNTc0MDY2ODMxMjc3NTI3Mzk2NjE1NDk5MjUwMTk4MzEwOTE3
NzAwMzcyNTYyNzQ0MzU0NjQzOTgyMjc3OTgxMTk5MTMyNTU5OTE2NTA4MDU5NjA0
NTc3NTQ3NDUxMzc5OTk1NDQxNDMzOTg3NTUwMTQ1NTY5MTkyOTc2NTMzODE1MjY0
NTUxNTAxNTI5ODI3MTEzNTgzMTAyNDYyMTAyMTk1MTE5NTA3MjAwMzkyNDczMTgw
NDI0NTU2MzA3OTgxMTYwODAwMDkzNTAzNTQ1MjMzMTU2NzIzNTMwMTg1MTE3MjY1
MTc1MTI4Njc0NDk3NDc1NzU3NDQxMTE3MDM1OTc3OTM4OTg0MjMxMTM4NzYzMjA0
NzkzMjAzNjU3NDMwMDI2MzAzODE3MTk4MjQzMjE4MjYyMzYxOTA0OTc4ODA2ODQz
MjI4ODY2OTIxOTM3MjI4MDYxMTk3OTE0MzIyODYyNTA5NzE3Mjk0NDAwNzQzNjY3
NTg1NzQ5MTMzNTc4MTUzMTMxNjQ3NTIwMDYyNTc5ODQ1NTIwMTQ2ODgwNDk2OTYy
MzkxMTI0MTYwNzA2NDg0NDczODYxMTg5MzkyMjE2NjUyNDI5NDA3
-----END RSA SPLIT PRIVATE KEY-----
`},
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

	Context("Adding targets", func() {
		When("Entry does not exist", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with an empty config")
				mockClient.On("FetchKeyShardData").Return(data.KeyShardData{}, nil)

				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddTarget(data.KeyEntry{}, "targetId")
			})

			It("Returns a KeyError without saving", func() {
				Expect(errors.Is(err, &KeyError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Entry exists / new target", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with one entry")
				mockClient.On("FetchKeyShardData").Return(data.DefaultMockKeyShardDataSmall(), nil)
				mockClient.On("Save", data.KeyShardData{data.DefaultMockKeyEntry3Target()}).Return(nil)

				By("adding a new target to that entry")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddTarget(data.DefaultMockSplitPrivateKey(), "targetId3")
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add target: %s", err))
			})

			It("adds the target to the entry", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("Entry exists / target exists", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with one entry")
				mockClient.On("FetchKeyShardData").Return(data.DefaultMockKeyShardDataSmall(), nil)

				By("adding an existing target to that entry")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddTarget(data.DefaultMockSplitPrivateKey(), "targetId2")
			})

			It("Returns a NoOpError without saving", func() {
				Expect(errors.Is(err, &NoOpError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Adding many targets at once / different entries", Ordered, func() {
			BeforeAll(func() {
				tempDir = GinkgoT().TempDir()
				checkPath = filepath.Join(tempDir, tmpConfigFile)

				By("starting with a config with many entries")
				initializeConfigFile(checkPath, data.MockKeyShardLargeNoTargetsRaw())

				client, _ := client.NewSystemdClient(tempDir, client.KeyShard)
				config, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())

				By("adding a few targets to each entry")
				for i := 1; i <= 4; i++ {
					for j := 1; j <= 12; j++ {
						if j%4 == i {
							go config.AddTarget(data.KeyEntry{KeyShardPem: fmt.Sprintf("%d", i)}, fmt.Sprintf("targetId%d", j))
						}
					}
				}

				// let the adds happeen
				time.Sleep(1 * time.Second)
			})

			It("adds the targets", func() {
				for i := 1; i <= 4; i++ {
					expectedTargetIds := []string{}
					for j := 1; j <= 12; j++ {
						if j%4 == i {
							expectedTargetIds = append(expectedTargetIds, fmt.Sprintf("targetId%d", j))
						}
					}

					By("checking that each entry has the data we wrote")
					newKey := data.DefaultMockSplitPrivateKey()
					newKey.KeyShardPem = fmt.Sprintf("%d", i)
					expectFileKeySetTo(checkPath, newKey, data.MappedKeyEntry{
						KeyData:   newKey,
						TargetIds: expectedTargetIds,
					})
				}
			})
		})

		When("Adding many targets at once / same entry, some existing", Ordered, func() {
			BeforeAll(func() {
				tempDir = GinkgoT().TempDir()
				checkPath = filepath.Join(tempDir, tmpConfigFile)

				By("starting with a config with one entry")
				mockData := data.DefaultMockKeyShardDataSmall()
				dataBytes, _ := json.Marshal(mockData)
				initializeConfigFile(checkPath, string(dataBytes))

				client, _ := client.NewSystemdClient(tempDir, client.KeyShard)
				config, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())

				By("adding a mix of existing and new targetIds to the entry")
				for i := 1; i <= 12; i++ {
					go config.AddTarget(data.DefaultMockSplitPrivateKey(), fmt.Sprintf("targetId%d", i))
				}

				// let the adds happeen
				time.Sleep(1 * time.Second)
			})

			It("adds the targets", func() {
				expectedTargetIds := []string{}
				for i := 1; i <= 12; i++ {
					expectedTargetIds = append(expectedTargetIds, fmt.Sprintf("targetId%d", i))
				}

				By("checking that all targets are present in the entry")
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

	Context("Deleting entries", func() {
		When("Entry does not exist", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with an empty config")
				mockClient.On("FetchKeyShardData").Return(data.KeyShardData{}, nil)

				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.DeleteKey(data.KeyEntry{})
			})

			It("Returns a KeyError without saving", func() {
				Expect(errors.Is(err, &KeyError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Entry exists", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with two entries")
				mockClient.On("FetchKeyShardData").Return(data.MockKeyShardDataMedium(), nil)
				mockClient.On("Save", data.AltMockKeyShardDataSmall()).Return(nil)

				By("deleting one of them")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.DeleteKey(data.DefaultMockSplitPrivateKey())
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to delete entry: %s", err))
			})

			It("deletes the entry", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("Deleting many entries at once", Ordered, func() {
			keyOne := data.AltMockKeyShardDataSmall()[0].KeyData
			keyOne.KeyShardPem = "1"

			BeforeAll(func() {
				By("starting with a config with many entries")
				tempDir = GinkgoT().TempDir()
				checkPath = filepath.Join(tempDir, tmpConfigFile)
				initializeConfigFile(checkPath, data.MockKeyShardLargeWithTargetsRaw())

				client, _ := client.NewSystemdClient(tempDir, client.KeyShard)
				config, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())
				By("deleting all but one entry")
				for i := 1; i <= 3; i++ {
					go config.DeleteKey(data.KeyEntry{KeyShardPem: fmt.Sprintf("%d", i)})
				}

				// let the deletes happeen
				time.Sleep(1 * time.Second)
			})

			It("deletes the entries", func() {
				By("checking that the deleted entries are absent")
				expectFileKeyUnset(checkPath, keyOne)
				for i := 2; i <= 3; i++ {
					newKey := data.DefaultMockSplitPrivateKey()
					newKey.KeyShardPem = fmt.Sprintf("%d", i)
					expectFileKeyUnset(checkPath, newKey)
				}
			})

			It("leaves the un-deleted entries", func() {
				By("checking that the un-deleted entry has not been modified")
				newKey := data.DefaultMockSplitPrivateKey()
				newKey.KeyShardPem = "4"
				expectFileKeySetTo(checkPath, newKey, data.MappedKeyEntry{
					KeyData:   newKey,
					TargetIds: []string{"targetId6", "targetId7"},
				})
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
