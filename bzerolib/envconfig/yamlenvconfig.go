package envconfig

import (
	"os"

	"bastionzero.com/bctl/v1/bzerolib/filelock"
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

	// second, clear the config file
	// TODO: Remove it?
	err := os.Truncate(y.path, 0)
	if err != nil {
		return err
	}

	// finally hard delete, unset the env vars
	if hard {

	}
}
