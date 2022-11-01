package envconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"bastionzero.com/bctl/v1/bzerolib/filelock"
	"gopkg.in/yaml.v3"
)

// YamlEnvConfig implements EnvConfig with an underlying yaml file
type YamlEnvConfig struct {
	path     string
	fileLock *filelock.FileLock
}

// TODO: should I check for anything else that would merit an error here?
func NewYamlEnvConfig(path string, fileLock *filelock.FileLock) (*YamlEnvConfig, error) {
	// create path if needed
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create %s: %s", path, err)
	}
	return &YamlEnvConfig{path, fileLock}, nil
}

func (y *YamlEnvConfig) Set(id string, env string, entry *EnvEntry) (string, error) {
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

	idEnv := concat(id, env)

	if err = entry.Reconcile(idEnv); err != nil {
		return "", err
	}

	em = set(em, id, idEnv, entry)

	if err = y.save(em); err != nil {
		return "", err
	}

	return entry.Value, nil
}

func (y *YamlEnvConfig) Get(id string, env string) (string, error) {
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

	idEnv := concat(id, env)
	entry, err := get(em, id, idEnv)
	if err != nil {
		return "", err
	}

	err = entry.Reconcile(idEnv)
	set(em, id, idEnv, entry)
	y.save(em)
	return entry.Value, err
}

func (y *YamlEnvConfig) Delete(id string, env string, hard bool) error {
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

	idEnv := concat(id, env)
	if _, err = get(em, id, idEnv); err != nil {
		return err
	}

	delete(em[id], idEnv)

	// finally hard delete, unset the env var
	if hard {
		os.Unsetenv(idEnv)
	}

	return y.save(em)
}

func (y *YamlEnvConfig) DeleteAll(id string, hard bool) error {

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

	// second, hard delete, unset the env vars
	if hard {
		for env := range em[id] {
			os.Unsetenv(concat(id, env))
		}
	}

	// finally clear the contents of id in the config file
	delete(em, id)
	y.save(em)

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

// TODO:
func concat(id string, env string) string {
	cleanId := strings.ReplaceAll(id, "-", "_")
	return fmt.Sprintf("%s__%s", cleanId, env)
}

// sets em[id][key] = entry, creating a new map if needed
func set(em entryMap, id string, idEnv string, entry *EnvEntry) entryMap {
	if _, ok := em[id]; !ok {
		em[id] = make(map[string]*EnvEntry)
	}

	em[id][idEnv] = entry
	return em
}

func get(em entryMap, id string, idEnv string) (*EnvEntry, error) {
	if _, ok := em[id]; !ok {
		return nil, &KeyError{Key: id}
	}

	entry, ok := em[id][idEnv]
	if !ok {
		return nil, &EnvKeyError{Key: idEnv}
	}

	return entry, nil
}
