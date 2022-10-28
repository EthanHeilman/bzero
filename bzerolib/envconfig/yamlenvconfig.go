package envconfig

import (
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
func NewYamlEnvConfig(path string, fileLock *filelock.FileLock) (*YamlEnvConfig, error) {
	// create path if needed
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create %s: %s", path, err)
	}
	return &YamlEnvConfig{path, fileLock}, nil
}

func (y *YamlEnvConfig) Set(id string, entry *Entry) (string, error) {
	lock, err := y.fileLock.NewLock()
	if err != nil {
		return "", fmt.Errorf("failed to create lock: %s", err)
	}

	for {
		if acquiredLock, err := lock.TryLock(); err != nil {
			return "", fmt.Errorf("failed to acquire lock: %s", err)
		} else if acquiredLock {
			break
		}
	}

	defer lock.Unlock()

	// first, load entry into memory
	em, err := y.load()
	// FIXME: hey what happens if this is empty?
	if err != nil {
		// TODO: special
		return "", err
	}

	if err = entry.Reconcile(); err != nil {
		return "", err
	}

	em[id] = *entry

	if err = y.save(em); err != nil {
		return "", err
	}

	return entry.Value, nil
}

func (y *YamlEnvConfig) Get(id string) (string, error) {
	lock, err := y.fileLock.NewLock()
	if err != nil {
		return "", fmt.Errorf("failed to create lock: %s", err)
	}

	for {
		if acquiredLock, err := lock.TryLock(); err != nil {
			return "", fmt.Errorf("failed to acquire lock: %s", err)
		} else if acquiredLock {
			break
		}
	}

	defer lock.Unlock()

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

	err = entry.Reconcile()
	return entry.Value, err
}

func (y *YamlEnvConfig) Delete(id string, hard bool) error {
	lock, err := y.fileLock.NewLock()
	if err != nil {
		return fmt.Errorf("failed to create lock: %s", err)
	}

	for {
		if acquiredLock, err := lock.TryLock(); err != nil {
			return fmt.Errorf("failed to acquire lock: %s", err)
		} else if acquiredLock {
			break
		}
	}

	defer lock.Unlock()

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

	return y.save(em)
}

func (y *YamlEnvConfig) DeleteAll(hard bool) error {

	lock, err := y.fileLock.NewLock()
	if err != nil {
		return fmt.Errorf("failed to create lock: %s", err)
	}

	for {
		if acquiredLock, err := lock.TryLock(); err != nil {
			return fmt.Errorf("failed to acquire lock: %s", err)
		} else if acquiredLock {
			break
		}
	}

	defer lock.Unlock()

	// first, load everything into memory
	em, err := y.load()
	if err != nil {
		return err
	}

	// second, clear the config file
	if err = os.Truncate(y.path, 0); err != nil {
		return err
	}

	// finally hard delete, unset the env vars
	if hard {
		for _, entry := range em {
			os.Unsetenv(entry.EnvVar)
		}
	}

	return nil
}

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
