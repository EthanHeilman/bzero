package envconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"bastionzero.com/bctl/v1/bzerolib/filelock"
	"gopkg.in/yaml.v3"
)

const (
	lockFileName = ".bzero.lock"
)

// YamlEnvConfig implements EnvConfig with an underlying yaml file
type YamlEnvConfig struct {
	path     string
	fileLock *filelock.FileLock
}

// TODO: should I check for anything that would merit an error here?
func NewYamlEnvConfig(path string, fileLock *filelock.FileLock) (*YamlEnvConfig, error) {
	// create path if needed
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create %s: %s", path, err)
	}
	return &YamlEnvConfig{path, fileLock}, nil
}

func (y *YamlEnvConfig) Get(id string) (string, error) {
	// first, load entry into memory
	em, err := y.load()
	if err != nil {
		// TODO: special
		return "", err
	}

	entry, ok := em[id]
	if !ok {
		// TODO: special
		return "", fmt.Errorf("TODO:")
	}

	// if the env var is set, see if we need to update the entry's value
	if envVal, ok := os.LookupEnv(entry.EnvVar); ok {
		if envVal != entry.Value {
			entry.Value = envVal
			em[id] = entry
		}

		return envVal, nil
	}

	// otherwise, set the env var and return what was in the file
	if err = os.Setenv(entry.EnvVar, entry.Value); err != nil {
		// even though we have the value, this is an error case because the whole mechanism
		// relies on the environment variable being availalbe to read and write to
		return "", err
	}
}

// TODO: lock
func (y *YamlEnvConfig) Delete(id string, hard bool) error {
	// first, load entry into memory
	em, err := y.load()
	if err != nil {
		// TODO: special
		return err
	}

	entry, ok := em[id]
	if !ok {
		// TODO: special
		return err
	}

	delete(em, id)

	// finally hard delete, unset the env var
	if hard {
		os.Unsetenv(entry.EnvVar)
	}

	return nil
}

// TODO: acquire lock first
func (y *YamlEnvConfig) DeleteAll(hard bool) error {
	// first, load everything into memory
	em, err := y.load()
	if err != nil {
		return err
	}

	// second, clear the config file
	// TODO: Remove it?
	if err = os.Truncate(y.path, 0); err != nil {
		return err
	}

	// finally hard delete, unset the env vars
	if hard {
		for _, entry := range em {
			os.Unsetenv(entry.EnvVar)
		}
	}
}

// TODO: acquire a lock first
// actually no that's the caller's job since this is internal
func (y *YamlEnvConfig) save(em EntryMap) error {
	// FIXME: perms
	file, err := os.OpenFile(y.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 700)
	if err != nil {
		return err
	}

	defer file.Close()

	// marshal entrymap into bytes
	emBytes, err := yaml.Marshal(em)
	if err != nil {
		return err
	}

	// write bytes to file in a complete overwrite
	if _, err = file.Write(emBytes); err != nil {
		return err
	}

	return nil
}

// TODO: acquire a lock first
// actually no that's the caller's job since this is internal
func (y *YamlEnvConfig) load() (EntryMap, error) {
	data, err := os.ReadFile("items.yaml")
	if err != nil {
		// TODO: look into special file not exist error
		return nil, err
	}

	var em EntryMap
	if err = yaml.Unmarshal(data, &em); err != nil {
		return nil, err
	}

	return em, nil
}
