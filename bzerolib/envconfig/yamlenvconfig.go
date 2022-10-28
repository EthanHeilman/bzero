package envconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"bastionzero.com/bctl/v1/bzerolib/filelock"
	"gopkg.in/yaml.v3"
)

// YamlEnvConfig implements EnvConfig with an underlying yaml file
type YamlEnvConfig struct {
	path     string
	fileLock *filelock.FileLock
}

// TODO: should I check for anything that would merit an error here?
func NewYamlEnvConfig(path string, fileLock *filelock.FileLock) *YamlEnvConfig {
	return &YamlEnvConfig{path, fileLock}
}

func (y *YamlEnvConfig) DeleteAll(hard bool) error {
	// first, load everything into memory
	em, err := y.load()

	// second, clear the config file
	// TODO: Remove it?
	if err = os.Truncate(y.path, 0); err != nil {
		return err
	}

	// finally hard delete, unset the env vars
	if hard {
		// TODO: loop through em
	}
}

// TODO: acquire a lock first
// actually no that's the caller's job since this is internal
func (y *YamlEnvConfig) save(em EntryMap) error {
	// check if file exists
	if _, err := os.Stat(y.path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// path/to/whatever does *not* exist
		} else {

			// Schrodinger: file may or may not exist. See err for details.

			// Therefore, do *NOT* use !os.IsNotExist(err) to test for file existence

		}

	}

	// create if it doesn't
	if err := os.MkdirAll(filepath.Dir(y.path), os.ModePerm); err != nil {
		return fmt.Errorf("failed to create %s: %s", y.path, err)
	}
	// marshal entrymap into bytes
	// write bytes to file in a complete overwrite
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
