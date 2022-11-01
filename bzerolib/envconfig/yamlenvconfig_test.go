package envconfig

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

func expectEnvVarUnset(id string, env string) {
	_, set := os.LookupEnv(concat(id, env))
	Expect(set).To(BeFalse(), fmt.Sprintf("env var %s should not be set", env))
}

func expectEnvVarSetTo(id string, env string, value string) {
	ev, set := os.LookupEnv(concat(id, env))
	Expect(set).To(BeTrue(), fmt.Sprintf("env var %s is not set", id))
	Expect(ev).To(Equal(value), fmt.Sprintf("env var %s should be set to '%s' -- it is set to '%s'", id, value, ev))
}

// TODO: might need a different version of this for an id being unset
func expectFileKeyUnset(path string, id string, env string) {
	data, err := os.ReadFile(path)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to read config file %s: %s", path, err))

	var em entryMap
	err = yaml.Unmarshal(data, &em)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to parse YAML: %s", err))

	_, ok := em[id]
	Expect(ok).To(BeTrue(), fmt.Sprintf("config did not contain key %s", id))

	_, ok = em[id][concat(id, env)]
	Expect(ok).To(BeFalse(), fmt.Sprintf("config key %s / env var %s should not be set", id, env))
}

func expectFileKeySetTo(path string, key string, env string, value string) {
	data, err := os.ReadFile(path)
	//fmt.Printf("Checking %s: %s\n", key, data)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to read config file %s: %s", path, err))

	var em entryMap
	err = yaml.Unmarshal(data, &em)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to parse YAML: %s", err))

	kv, ok := em[key][concat(key, env)]
	Expect(ok).To(BeTrue(), fmt.Sprintf("config did not contain key %s", key))
	Expect(kv.Value).To(Equal(value), fmt.Sprintf("config file key %s should be set to '%s' -- it is set to '%s'", key, value, kv.Value))
}

func expectFileTruncated(path string) {
	info, err := os.Stat(path)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to find config file %s: %s", path, err))
	Expect(info.Size()).To(Equal(int64(0)), fmt.Sprintf("file was not truncated: size = %d", info.Size()))
}

func initializeConfigFile(path string, contents string) {
	file, _ := os.Create(path)
	file.WriteString(contents)
}

func setManyEnvVars(id string) {
	for i := 1; i <= 12; i++ {
		os.Setenv(concat(id, fmt.Sprintf("TEST_ENV_VAR_%d", i)), fmt.Sprintf("VALUE_%d", i))
	}
}

func unsetManyEnvVars(id string) {
	for i := 1; i <= 12; i++ {
		os.Unsetenv(concat(id, fmt.Sprintf("TEST_ENV_VAR_%d", i)))
	}
}

func TestYamlEnvConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Yaml EnvConfig Suite")
}

// TODO: need a test that includes comments / last modified...

var _ = Describe("Yaml EnvConfig", Ordered, func() {
	testKey := "testKey1"
	testEnvVarKey := "TEST_ENV_VAR_1"
	testFileValue := "testValue-from-file"
	testEnvValue := "testValue-from-env"

	fileLock := filelock.NewFileLock(".test.lock")

	var path, val string

	AfterAll(func() {
		fileLock.Cleanup()
	})

	Context("Setup", func() {
		When("Happy path", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				_, err = NewYamlEnvConfig(path, fileLock)
			})

			It("Initializes successfully", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to set up: %s", err))
			})
		})

		// TODO: failure cases here?
	})

	Context("Setting", func() {

		When("File does not exist / env var is not set", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				ec, _ := NewYamlEnvConfig(path, fileLock)
				val, err = ec.Set(testKey, testEnvVarKey, &EnvEntry{
					Value: testFileValue,
				})
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to set value: %s", err))
			})

			It("Creates a file with the provided value", func() {
				expectFileKeySetTo(path, testKey, testEnvVarKey, testFileValue)
			})

			It("Sets the env var to the provided value", func() {
				expectEnvVarSetTo(testKey, testEnvVarKey, testFileValue)
			})

			It("Returns the provided value", func() {
				Expect(val).To(Equal(testFileValue), fmt.Sprintf("setting %s should return the provided value, '%s' -- got '%s'", testKey, testFileValue, val))
			})
		})

		When("File does not exist / env var is set", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				os.Setenv(testEnvVarKey, testEnvValue)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				val, err = ec.Set(testKey, testEnvVarKey, &EnvEntry{
					Value: testFileValue,
				})
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to set value: %s", err))
			})

			It("Creates a file with the env var value", func() {
				expectFileKeySetTo(path, testKey, testEnvVarKey, testEnvValue)
			})

			It("Does not modify the env var value", func() {
				expectEnvVarSetTo(testKey, testEnvVarKey, testEnvValue)
			})

			It("Returns the env var value", func() {
				Expect(val).To(Equal(testEnvValue), fmt.Sprintf("setting %s should return the value of %s, '%s' -- got '%s'", testKey, testEnvVarKey, testEnvValue, val))
			})
		})

		When("File and key exist / env var is not set", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				initializeConfigFile(path, exampleSmall)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				val, err = ec.Set(testKey, testEnvVarKey, &EnvEntry{
					Value: testFileValue,
				})
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to set value: %s", err))
			})

			It("Adds the provided value to the file", func() {
				expectFileKeySetTo(path, testKey, testEnvVarKey, testFileValue)
			})

			It("Updates the env var and sets it to the provided value", func() {
				expectEnvVarSetTo(testKey, testEnvVarKey, testFileValue)
			})

			It("Returns the provided value", func() {
				Expect(val).To(Equal(testFileValue), fmt.Sprintf("setting %s should return the provided value, '%s' -- got '%s'", testKey, testFileValue, val))
			})
		})

		When("File and key exist / env var is set", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				initializeConfigFile(path, exampleSmall)
				os.Setenv(testEnvVarKey, testEnvValue)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				val, err = ec.Set(testKey, testEnvVarKey, &EnvEntry{
					Value: testFileValue,
				})
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to set value: %s", err))
			})

			It("Adds the env var value to the file", func() {
				expectFileKeySetTo(path, testKey, testEnvVarKey, testEnvValue)
			})

			It("Does not modify the env var value", func() {
				expectEnvVarSetTo(testKey, testEnvVarKey, testEnvValue)
			})

			It("Returns the env var value", func() {
				Expect(val).To(Equal(testEnvValue), fmt.Sprintf("setting %s should return the value of %s, '%s' -- got '%s'", testKey, testEnvVarKey, testEnvValue, val))
			})
		})

		When("Setting many values at once, env vars not set", Ordered, func() {
			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")

				ec, _ := NewYamlEnvConfig(path, fileLock)
				for i := 1; i <= 12; i++ {
					go ec.Set(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), &EnvEntry{
						Value: fmt.Sprintf("testValue-%d", i),
					})
				}

				// let the sets happeen
				time.Sleep(1 * time.Second)
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("Sets all values in the file", func() {
				for i := 1; i <= 12; i++ {
					expectFileKeySetTo(path, fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), fmt.Sprintf("testValue-%d", i))
				}
			})

			It("Sets all env vars", func() {
				for i := 1; i <= 12; i++ {
					expectEnvVarSetTo(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), fmt.Sprintf("testValue-%d", i))
				}
			})
		})

		When("Setting many values at once, some env vars set", Ordered, func() {
			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				setManyEnvVars(testKey)
				for i := 1; i <= 12; i++ {
					if i%2 == 0 {
						// unset all even env vars
						os.Unsetenv(fmt.Sprintf("TEST_ENV_VAR_%d", i))
					}
				}

				ec, _ := NewYamlEnvConfig(path, fileLock)
				for i := 1; i <= 12; i++ {
					go ec.Set(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), &EnvEntry{
						Value: fmt.Sprintf("testValue-%d", i),
					})
				}

				// let the sets happeen
				time.Sleep(1 * time.Second)
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("Sets all values in the file correctly", func() {
				for i := 1; i <= 12; i++ {
					if i%2 == 0 {
						// since even env vars were unset, the file values should remain
						expectFileKeySetTo(path, fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), fmt.Sprintf("testValue-%d", i))
					} else {
						// odd env vars were not unset, so the env value should be written to the file
						expectFileKeySetTo(path, fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), fmt.Sprintf("VALUE_%d", i))
					}
				}
			})

			It("Sets unset env vars", func() {
				for i := 1; i <= 12; i++ {
					if i%2 == 0 {
						// even env var values should have been overwritten
						expectEnvVarSetTo(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), fmt.Sprintf("testValue-%d", i))
					} else {
						// odd env var values should remain
						expectEnvVarSetTo(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), fmt.Sprintf("VALUE_%d", i))
					}
				}
			})
		})

		When("Setting many values at once, all env vars set", Ordered, func() {
			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				setManyEnvVars(testKey)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				for i := 1; i <= 12; i++ {
					go ec.Set(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), &EnvEntry{
						Value: fmt.Sprintf("testValue-%d", i),
					})
				}

				// let the sets happeen
				time.Sleep(1 * time.Second)
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("Sets all values in the file correctly", func() {
				for i := 1; i <= 12; i++ {
					expectFileKeySetTo(path, fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), fmt.Sprintf("VALUE_%d", i))
				}
			})

			It("Does not modify any env var values", func() {
				for i := 1; i <= 12; i++ {
					expectEnvVarSetTo(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), fmt.Sprintf("VALUE_%d", i))
				}
			})
		})
	})

	Context("Getting", func() {
		When("File does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")

				ec, _ := NewYamlEnvConfig(path, fileLock)
				val, err = ec.Get(testKey, testEnvVarKey)
			})

			It("Returns a FileError", func() {
				Expect(errors.Is(err, &FileError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Key does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				initializeConfigFile(path, exampleSmall)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				val, err = ec.Get("testKey2", testEnvVarKey)
			})

			It("Returns a KeyError", func() {
				Expect(errors.Is(err, &KeyError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		// TODO: EnvKeyError

		When("Value is found in file, env var is not set", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				initializeConfigFile(path, exampleMedium)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				val, err = ec.Get(testKey, testEnvVarKey)
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to get value: %s", err))
			})

			It("Sets the env var to the file value", func() {
				expectEnvVarSetTo(testKey, testEnvVarKey, testFileValue)
			})

			It("Returns the file value", func() {
				Expect(val).To(Equal(testFileValue), fmt.Sprintf("getting %s should return the value in the file, '%s' -- got '%s'", testKey, testFileValue, val))
			})
		})

		When("Value is found in file, env var agrees", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				initializeConfigFile(path, exampleMedium)
				os.Setenv(testEnvVarKey, testFileValue)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				val, err = ec.Get(testKey, testEnvVarKey)
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to get value: %s", err))
			})

			It("Returns the file value", func() {
				Expect(val).To(Equal(testFileValue), fmt.Sprintf("getting %s should return the value in the file, '%s' -- got '%s'", testKey, testFileValue, val))
			})

			It("Does not update the env var", func() {
				expectEnvVarSetTo(testKey, testEnvVarKey, testFileValue)
			})
		})

		When("Value is found in file, env var disagrees", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				initializeConfigFile(path, exampleMedium)
				os.Setenv(testEnvVarKey, testEnvValue)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				val, err = ec.Get(testKey, testEnvVarKey)
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to get value: %s", err))
			})

			It("Updates the file with the env var value", func() {
				expectFileKeySetTo(path, testKey, testEnvVarKey, testEnvValue)
			})

			It("Does not modify the env var value", func() {
				expectEnvVarSetTo(testKey, testEnvVarKey, testEnvValue)
			})

			It("Returns the env var value", func() {
				Expect(val).To(Equal(testEnvValue), fmt.Sprintf("getting %s should return the value of %s, '%s' -- got '%s'", testKey, testEnvVarKey, testEnvValue, val))
			})
		})
	})

	Context("Deleting", func() {
		When("File does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")

				ec, _ := NewYamlEnvConfig(path, fileLock)
				err = ec.Delete(testKey, testEnvVarKey, false)
			})

			It("Returns a FileError", func() {
				Expect(errors.Is(err, &FileError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Key does not exist", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				initializeConfigFile(path, exampleSmall)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				err = ec.Delete("testKey2", testEnvVarKey, false)
			})

			It("Returns a KeyError", func() {
				Expect(errors.Is(err, &KeyError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		// TODO: EnvKeyError

		When("Performing a soft delete", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				initializeConfigFile(path, exampleMedium)
				os.Setenv(testEnvVarKey, testFileValue)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				err = ec.Delete(testKey, testEnvVarKey, false)
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to delete value: %s", err))
			})

			It("Removes the key from the file", func() {
				expectFileKeyUnset(path, testKey, testEnvVarKey)
			})

			It("Does not unset the env var", func() {
				expectEnvVarSetTo(testKey, testEnvVarKey, testFileValue)
			})
		})

		When("Performing a hard delete", Ordered, func() {
			var err error

			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				initializeConfigFile(path, exampleMedium)
				os.Setenv(testEnvVarKey, testFileValue)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				err = ec.Delete(testKey, testEnvVarKey, true)
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to delete value: %s", err))
			})

			It("Removes the key from the file", func() {
				expectFileKeyUnset(path, testKey, testEnvVarKey)
			})

			It("Unsets the env var", func() {
				expectEnvVarUnset(testKey, testEnvVarKey)
			})
		})

		When("Performing many soft deletes at once", Ordered, func() {
			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				initializeConfigFile(path, exampleLarge)
				setManyEnvVars(testKey)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				for i := 1; i <= 12; i++ {
					if i%2 == 0 {
						go ec.Delete(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), false)
					}
				}

				// let the deletes happeen
				time.Sleep(1 * time.Second)
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("Removes the correct keys from the file", func() {
				for i := 1; i <= 12; i++ {
					if i%2 == 0 {
						expectFileKeyUnset(path, fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i))
					} else {
						expectFileKeySetTo(path, fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), fmt.Sprintf("testValue-%d", i))
					}
				}
			})

			It("Does not unset any env vars", func() {
				for i := 1; i <= 12; i++ {
					expectEnvVarSetTo(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), fmt.Sprintf("VALUE_%d", i))
				}
			})
		})

		When("Performing many hard deletes at once", Ordered, func() {
			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				initializeConfigFile(path, exampleLarge)
				setManyEnvVars(testKey)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				for i := 1; i <= 12; i++ {
					if i%2 == 0 {
						go ec.Delete(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), true)
					}
				}

				// let the deletes happeen
				time.Sleep(1 * time.Second)
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			It("Removes the correct keys from the file", func() {
				for i := 1; i <= 12; i++ {
					if i%2 == 0 {
						expectFileKeyUnset(path, fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i))
					} else {
						expectFileKeySetTo(path, fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), fmt.Sprintf("testValue-%d", i))
					}
				}
			})

			It("Unsets the correct env vars", func() {
				for i := 1; i <= 12; i++ {
					if i%2 == 0 {
						expectEnvVarUnset(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i))
					} else {
						expectEnvVarSetTo(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), fmt.Sprintf("VALUE_%d", i))
					}
				}
			})
		})

		When("Performing a soft delete all", Ordered, func() {
			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				initializeConfigFile(path, exampleLarge)
				setManyEnvVars(testKey)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				ec.DeleteAll(testKey, false)
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			// FIXME: NO LONGER EXPECTED
			It("Truncates the file", func() {
				expectFileTruncated(path)
			})

			It("Does not unset the env vars", func() {
				for i := 1; i <= 12; i++ {
					expectEnvVarSetTo(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i), fmt.Sprintf("VALUE_%d", i))
				}
			})
		})

		When("Performing a hard delete all", Ordered, func() {
			BeforeAll(func() {
				path = filepath.Join(GinkgoT().TempDir(), "tmp-config.yml")
				initializeConfigFile(path, exampleLarge)
				setManyEnvVars(testKey)

				ec, _ := NewYamlEnvConfig(path, fileLock)
				ec.DeleteAll(testKey, true)
			})

			AfterAll(func() {
				unsetManyEnvVars(testKey)
			})

			// FIXME: NO LONGER EXPECTED
			It("Truncates the file", func() {
				expectFileTruncated(path)
			})

			It("Unsets the env vars", func() {
				for i := 1; i <= 12; i++ {
					expectEnvVarUnset(fmt.Sprintf("testKey%d", i), fmt.Sprintf("TEST_ENV_VAR_%d", i))
				}
			})
		})
	})
})
