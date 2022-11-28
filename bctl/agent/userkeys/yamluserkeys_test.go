package userkeys

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bastionzero.com/bctl/v1/bzerolib/filelock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

func initializeConfigFile(path string, contents string) {
	file, _ := os.Create(path)
	file.WriteString(contents)
}

func expectFileKeyUnset(path string, key SplitPrivateKey) {
	data, err := os.ReadFile(path)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to read config file %s: %s", path, err))

	var el entryList
	err = yaml.Unmarshal(data, &el)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to parse YAML: %s", err))

	_, err = findEntry(el, key)
	Expect(errors.Is(err, &KeyError{}), fmt.Sprintf("expected entry to be unset but got err: %s", err))
}

func expectFileKeySetTo(path string, key SplitPrivateKey, expectedEntry KeyEntry) {
	data, err := os.ReadFile(path)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to read config file %s: %s", path, err))

	var el entryList
	err = yaml.Unmarshal(data, &el)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to parse YAML: %s", err))

	idx, err := findEntry(el, key)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to find entry: %s", err))

	expectEntryToEqual(el[idx], expectedEntry)
}

func expectEntryToEqual(actual KeyEntry, expected KeyEntry) {
	Expect(expected.Key).To(Equal(actual.Key), "Keys do not match:\nActual: %+v\nExpected: %+v", actual.Key, expected.Key)
	Expect(expected.TargetIds).To(ContainElements(actual.TargetIds), "TargetIds do not match:\nActual: %+v\nExpected: %+v", actual.TargetIds, expected.TargetIds)
}

func TestYamlUserKeys(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Yaml UserKeys Suite")
}

var _ = Describe("Yaml UserKeys", Ordered, func() {
	tmpConfigFile := "tmp-config.yaml"
	fileLock := filelock.NewFileLock(".test.lock")

	var path string

	AfterAll(func() {
		fileLock.Cleanup()
	})

	Context("Setup", func() {
		When("Happy path", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				_, err = NewYamlUserKeys(path, fileLock)
			})

			It("Initializes successfully", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to set up: %s", err))
			})
		})

		When("Invalid initialization", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				_, err = NewYamlUserKeys(path, nil)
			})

			It("Initializes successfully", func() {
				Expect(err).NotTo(BeNil(), "nil fileLock should cause setup to fail")
			})
		})
	})

	Context("Adding entries", func() {

		When("File does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.AddKey(mockEntry)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add entry: %s", err))
			})

			It("Creates a file with the provided value", func() {
				expectFileKeySetTo(path, mockEntry.Key, mockEntry)
			})
		})

		When("File exists but is invalid", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleInvalid)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.AddKey(mockEntry)
			})

			It("returns a ValidationError", func() {
				Expect(errors.Is(err, &NoOpError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("File exists / entry does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleSmallOneTarget)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.AddKey(mockEntry)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add entry: %s", err))
			})

			It("Adds the provided entry to the file", func() {
				expectFileKeySetTo(path, mockEntry.Key, mockEntry)
			})
		})

		When("File exists / entry exists / new targets", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleMediumSomeTargets)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.AddKey(mockEntry)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add entry: %s", err))
			})

			It("Adds the new targets to the existing entry", func() {
				expectFileKeySetTo(path, mockEntry.Key, mockEntryAllTargets)
			})
		})

		When("File exists / entry exists / no new targets", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleMediumAllTargets)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.AddKey(mockEntry)
			})

			It("Returns a NoOpError", func() {
				Expect(errors.Is(err, &NoOpError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Adding many entries at once / different keys", Ordered, func() {
			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)

				uk, _ := NewYamlUserKeys(path, fileLock)
				for i := 1; i <= 12; i++ {
					newKey := mockSplitPrivateKey
					newKey.D = int64(i)
					go uk.AddKey(KeyEntry{
						Key:       newKey,
						TargetIds: mockTargetIds,
					})
				}

				// let the adds happeen
				time.Sleep(1 * time.Second)
			})

			It("Sets all values in the file", func() {
				for i := 1; i <= 12; i++ {
					newKey := mockSplitPrivateKey
					newKey.D = int64(i)
					expectFileKeySetTo(path, newKey, KeyEntry{
						Key:       newKey,
						TargetIds: mockTargetIds,
					})
				}
			})
		})

		When("Adding many entries at once / same key", Ordered, func() {
			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)

				uk, _ := NewYamlUserKeys(path, fileLock)
				for i := 1; i <= 12; i++ {
					go uk.AddKey(KeyEntry{
						Key:       mockSplitPrivateKey,
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

				expectFileKeySetTo(path, mockSplitPrivateKey, KeyEntry{
					Key:       mockSplitPrivateKey,
					TargetIds: expectedTargetIds,
				})
			})
		})
	})

	Context("Adding targets", func() {
		When("File does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.AddTarget(SplitPrivateKey{}, "targetId")
			})

			It("Returns a FileError", func() {
				Expect(errors.Is(err, &FileError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Entry does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleSmall)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.AddTarget(SplitPrivateKey{}, "targetId")
			})

			It("Returns a KeyError", func() {
				Expect(errors.Is(err, &KeyError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Entry exists / new target", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleSmall)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.AddTarget(mockSplitPrivateKey, "targetId0")
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add target: %s", err))
			})

			It("adds the target to the entry", func() {
				expectFileKeySetTo(path, mockEntry.Key, mockEntryAllTargets)
			})
		})

		When("Entry exists / target exists", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleSmall)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.AddTarget(mockSplitPrivateKey, "targetId1")
			})

			It("Returns a NoOpError", func() {
				Expect(errors.Is(err, &NoOpError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Adding many targets at once / different entries", Ordered, func() {
			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleLargeNoTargets)

				uk, _ := NewYamlUserKeys(path, fileLock)
				for i := 1; i <= 4; i++ {
					for j := 1; j <= 12; j++ {
						if j%4 == i {
							go uk.AddTarget(SplitPrivateKey{D: int64(i)}, fmt.Sprintf("targetId%d", i))
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

					newKey := mockSplitPrivateKey
					newKey.D = int64(i)
					expectFileKeySetTo(path, newKey, KeyEntry{
						Key:       newKey,
						TargetIds: expectedTargetIds,
					})
				}
			})
		})

		When("Adding many targets at once / same entry, some existing", Ordered, func() {
			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleSmall)

				uk, _ := NewYamlUserKeys(path, fileLock)
				for i := 1; i <= 12; i++ {
					go uk.AddTarget(mockSplitPrivateKey, fmt.Sprintf("targetId%d", i))
				}

				// let the adds happeen
				time.Sleep(1 * time.Second)
			})

			It("adds the targets", func() {
				expectedTargetIds := []string{}
				for i := 1; i <= 12; i++ {
					expectedTargetIds = append(expectedTargetIds, fmt.Sprintf("targetId%d", i))
				}

				expectFileKeySetTo(path, mockSplitPrivateKey, KeyEntry{
					Key:       mockSplitPrivateKey,
					TargetIds: expectedTargetIds,
				})
			})
		})
	})

	Context("LastKey", func() {
		When("File does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)

				uk, _ := NewYamlUserKeys(path, fileLock)
				_, err = uk.LastKey("targetId1")
			})

			It("Returns a FileError", func() {
				Expect(errors.Is(err, &FileError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Target does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleSmall)

				uk, _ := NewYamlUserKeys(path, fileLock)
				_, err = uk.LastKey("targetId1000")
			})

			It("Returns a TargetError", func() {
				Expect(errors.Is(err, &TargetError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Target exists in multiple entries", Ordered, func() {
			var err error
			var key SplitPrivateKey

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleMediumSomeTargets)

				uk, _ := NewYamlUserKeys(path, fileLock)
				key, err = uk.LastKey("targetId1")
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to get key: %s", err))
			})

			It("returns the correct key", func() {
				Expect(key.D).To(Equal(int64(123)))
			})
		})

		When("Target only exists in earliest entry", Ordered, func() {
			var err error
			var key SplitPrivateKey

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleLargeWithTargets)

				uk, _ := NewYamlUserKeys(path, fileLock)
				key, err = uk.LastKey("targetId0")
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to get key: %s", err))
			})

			It("returns the correct key", func() {
				Expect(key.D).To(Equal(int64(1)))
			})
		})
	})

	Context("Deleting entries", func() {
		When("File does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.DeleteKey(SplitPrivateKey{})
			})

			It("Returns a FileError", func() {
				Expect(errors.Is(err, &FileError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Entry does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleSmall)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.DeleteKey(SplitPrivateKey{})
			})

			It("Returns a KeyError", func() {
				Expect(errors.Is(err, &KeyError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Entry exists", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleMediumSomeTargets)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.DeleteKey(mockSplitPrivateKey)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to delete entry: %s", err))
			})

			It("deletes the entry", func() {
				expectFileKeyUnset(path, mockSplitPrivateKey)
			})
		})

		When("Deleting many entries at once", Ordered, func() {

			keyOne := oldSplitPrivateKey
			keyOne.D = 1

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleLargeWithTargets)

				uk, _ := NewYamlUserKeys(path, fileLock)
				for i := 1; i <= 3; i++ {
					go uk.DeleteKey(SplitPrivateKey{D: int64(i)})
				}

				// let the deletes happeen
				time.Sleep(1 * time.Second)
			})

			It("deletes the entries", func() {
				expectFileKeyUnset(path, keyOne)
				for i := 2; i <= 3; i++ {
					newKey := mockSplitPrivateKey
					newKey.D = int64(i)
					expectFileKeyUnset(path, newKey)
				}
			})

			It("leaves the un-deleted entries", func() {
				newKey := mockSplitPrivateKey
				newKey.D = 4
				expectFileKeySetTo(path, newKey, KeyEntry{
					Key:       newKey,
					TargetIds: []string{"targetId6", "targetId7"},
				})
			})
		})
	})

	Context("Deleting targets", func() {
		When("File does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.DeleteTarget("target", false)
			})

			It("Returns a FileError", func() {
				Expect(errors.Is(err, &FileError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Target does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleSmall)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.DeleteTarget("target", false)
			})

			It("Returns a TargetError", func() {
				Expect(errors.Is(err, &TargetError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Target exists / soft delete", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleMediumSomeTargets)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.DeleteTarget("targetId1", false)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to delete target: %s", err))
			})

			It("removes the target from the most recent entry", func() {
				expectFileKeySetTo(path, mockSplitPrivateKey, KeyEntry{
					Key:       mockSplitPrivateKey,
					TargetIds: []string{"targetId0"},
				})
			})

			It("does not remove the target from other entries", func() {
				expectFileKeySetTo(path, oldSplitPrivateKey, KeyEntry{
					Key:       oldSplitPrivateKey,
					TargetIds: []string{"targetId0", "targetId1"},
				})
			})
		})

		When("Target exists / hard delete", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleMediumSomeTargets)

				uk, _ := NewYamlUserKeys(path, fileLock)
				err = uk.DeleteTarget("targetId1", true)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to delete target: %s", err))
			})

			It("removes the target from all entries", func() {
				expectFileKeySetTo(path, mockSplitPrivateKey, KeyEntry{
					Key:       mockSplitPrivateKey,
					TargetIds: []string{"targetId0"},
				})

				expectFileKeySetTo(path, oldSplitPrivateKey, KeyEntry{
					Key:       oldSplitPrivateKey,
					TargetIds: []string{"targetId0"},
				})
			})
		})

		When("Deleting many targets at once", Ordered, func() {
			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), tmpConfigFile)
				initializeConfigFile(path, exampleLargeWithTargets)

				uk, _ := NewYamlUserKeys(path, fileLock)
				for i := 1; i <= 8; i++ {
					go uk.DeleteTarget(fmt.Sprintf("targetId%d", i), false)
				}

				// let the deletes happeen
				time.Sleep(1 * time.Second)

				It("deletes all the targets", func() {
					newKey := mockSplitPrivateKey
					newKey.D = 2
					expectFileKeySetTo(path, newKey, KeyEntry{
						Key:       oldSplitPrivateKey,
						TargetIds: []string{},
					})

					newKey.D = 3
					expectFileKeySetTo(path, newKey, KeyEntry{
						Key:       oldSplitPrivateKey,
						TargetIds: []string{},
					})

					newKey.D = 4
					expectFileKeySetTo(path, newKey, KeyEntry{
						Key:       oldSplitPrivateKey,
						TargetIds: []string{},
					})
				})

				It("leaves the un-deleted target", func() {
					keyOne := oldSplitPrivateKey
					keyOne.D = 1
					expectFileKeySetTo(path, keyOne, KeyEntry{
						Key:       oldSplitPrivateKey,
						TargetIds: []string{"targetId0"},
					})
				})
			})
		})
	})
})
