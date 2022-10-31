package envconfig

import (
	"errors"
	"fmt"
	"io/fs"
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

func (y *YamlEnvConfig) Set(id string, entry *ECEntry) (string, error) {
	lock, err := y.fileLock.NewLock()
	if err != nil {
		return "", fmt.Errorf("failed to create lock: %s", err)
	}

	for {
		if acquiredLock, err := lock.TryLock(); err != nil {
			return "", fmt.Errorf("failed to acquire lock: %s", err)
		} else if acquiredLock {
			//fmt.Printf("%s: I got the lock ;)\n", id)
			break
		} else {
			//fmt.Printf("%s: waiting for the lock :(\n", id)
		}
	}

	defer func() {
		lock.Unlock()

		//fmt.Printf("%s: I gave the lock back :D\n", id)
	}()

	// first, load entry into memory
	em, err := y.load()
	//fmt.Printf("1. %+v\n", em)
	// FIXME: hey what happens if this is empty?
	if err != nil {
		return "", err
	}

	if err = entry.Reconcile(); err != nil {
		return "", err
	}

	em[id] = *entry
	//fmt.Printf("2. %+v\n", em)

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
		return "", err
	}

	entry, ok := em[id]
	if !ok {
		return "", &KeyError{}
	}

	err = entry.Reconcile()
	em[id] = entry
	y.save(em)
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
		return err
	}

	entry, ok := em[id]
	if !ok {
		return &KeyError{}
	}

	delete(em, id)

	// finally hard delete, unset the env var
	if hard {
		os.Unsetenv(entry.Env)
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
			os.Unsetenv(entry.Env)
		}
	}

	return nil
}

func (y *YamlEnvConfig) save(em entryMap) error {
	// create if not exists, else overwrite entirely
	file, err := os.Create(y.path)
	if err != nil {
		return &FileError{Path: y.path, InnerErr: err}
	}

	defer file.Close()

	// marshal entrymap into bytes
	emBytes, err := yaml.Marshal(em)
	if err != nil {
		return err
	}

	// write bytes to file in a complete overwrite
	if _, err = file.Write(emBytes); err != nil {
		return &ValidationError{InnerErr: err}
	}

	return nil
}

func (y *YamlEnvConfig) load() (entryMap, error) {
	var data []byte
	var err error

	// if file does not exist, create it and return an empty map
	if _, err = os.Stat(y.path); errors.Is(err, fs.ErrNotExist) {
		if _, cerr := os.Create(y.path); cerr != nil {
			return nil, &FileError{Path: y.path, InnerErr: cerr}
		}
		return entryMap{}, nil
	} else if err != nil {
		return nil, &FileError{Path: y.path, InnerErr: err}
	}

	data, err = os.ReadFile(y.path)
	if err != nil {
		return nil, &FileError{Path: y.path, InnerErr: err}
	}

	var em entryMap
	if err = yaml.Unmarshal(data, &em); err != nil {
		return nil, &ValidationError{InnerErr: err}
	}

	return em, nil
}
