package userkeys

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"bastionzero.com/bctl/v1/bzerolib/filelock"
	"gopkg.in/yaml.v3"
)

// YamlUserKeys implements UserKeys with an underlying yaml file
type YamlUserKeys struct {
	path     string
	fileLock *filelock.FileLock
}

func NewYamlUserKeys(path string, fileLock *filelock.FileLock) (*YamlUserKeys, error) {
	// create path if needed

	if fileLock == nil {
		return nil, fmt.Errorf("fileLock must not be nil")
	}

	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create %s: %s", path, err)
	}

	return &YamlUserKeys{path, fileLock}, nil
}

func (y *YamlUserKeys) Add(newEntry KeyEntry) error {
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

	el, err := y.load()
	if err != nil {
		return err
	}

	// if this entry already exists, check which targets it maps to
	idx, err := findEntry(el, newEntry.Hash)
	if err == nil {
		var addedSomeTargets bool
		for _, targetId := range newEntry.TargetIds {
			// add any new targets
			if !containsTarget(el[idx], targetId) {
				el[idx].TargetIds = append(el[idx].TargetIds, targetId)
				addedSomeTargets = true
			}
		}
		// let the caller know if we didn't do anything
		if !addedSomeTargets {
			return &NoOpError{}
		}
	} else {
		// if the new entry doesn't already exist, just append it
		el = append(el, newEntry)
	}

	if err = y.save(el); err != nil {
		return err
	}

	return nil
}

func (y *YamlUserKeys) AddTarget(hash string, targetId string) error {
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

	el, err := y.load()
	if err != nil {
		return err
	}

	idx, err := findEntry(el, hash)
	if err != nil {
		return err
	}

	if containsTarget(el[idx], targetId) {
		return &NoOpError{}
	}

	el[idx].TargetIds = append(el[idx].TargetIds, targetId)

	if err = y.save(el); err != nil {
		return err
	}

	return nil
}

func (y *YamlUserKeys) LastKey(targetId string) (SplitPrivateKey, error) {
	lock, err := y.fileLock.NewLock()
	if err != nil {
		return SplitPrivateKey{}, fmt.Errorf("failed to create lock: %s", err)
	}

	for {
		if acquiredLock, err := lock.TryLock(); err != nil {
			return SplitPrivateKey{}, fmt.Errorf("failed to acquire lock: %s", err)
		} else if acquiredLock {
			break
		}
	}

	defer lock.Unlock()

	el, err := y.load()
	if err != nil {
		return SplitPrivateKey{}, err
	}

	idx, err := lastIndex(el, targetId)
	if err != nil {
		return SplitPrivateKey{}, err
	}

	return el[idx].Key, nil
}

func (y *YamlUserKeys) DeleteKey(hash string) error {
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

	el, err := y.load()
	if err != nil {
		return err
	}

	idx, err := findEntry(el, hash)
	if err != nil {
		return err
	}

	elAfterDelete := removeEntry(el, idx)

	if err = y.save(elAfterDelete); err != nil {
		return err
	}

	return nil
}

func (y *YamlUserKeys) DeleteTarget(targetId string, hard bool) error {
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

	el, err := y.load()
	if err != nil {
		return err
	}

	elAfterDelete, err := removeTarget(el, targetId, hard)
	if err != nil {
		return err
	}

	if err = y.save(elAfterDelete); err != nil {
		return err
	}

	return nil
}

// internal method, so we assume a lock has been acquired
func (y *YamlUserKeys) save(el entryList) error {
	// create if not exists, else overwrite entirely
	file, err := os.Create(y.path)
	if err != nil {
		return &FileError{Path: y.path, InnerErr: err}
	}

	defer file.Close()

	// marshal entrymap into bytes
	elBytes, err := yaml.Marshal(el)
	if err != nil {
		return err
	}

	// write bytes to file in a complete overwrite
	if _, err = file.Write(elBytes); err != nil {
		return &ValidationError{InnerErr: err}
	}

	return nil
}

// internal method, so we assume a lock has been acquired
func (y *YamlUserKeys) load() (entryList, error) {
	var data []byte
	var err error

	// if file does not exist, create it and return an empty map
	if _, err = os.Stat(y.path); errors.Is(err, fs.ErrNotExist) {
		if _, cerr := os.Create(y.path); cerr != nil {
			return nil, &FileError{Path: y.path, InnerErr: cerr}
		}
		return entryList{}, nil
	} else if err != nil {
		return nil, &FileError{Path: y.path, InnerErr: err}
	}

	data, err = os.ReadFile(y.path)
	if err != nil {
		return nil, &FileError{Path: y.path, InnerErr: err}
	}

	var el entryList
	if err = yaml.Unmarshal(data, &el); err != nil {
		return nil, &ValidationError{InnerErr: err}
	}

	return el, nil
}

// get the entryList index matching the given hash
func findEntry(el entryList, hash string) (int, error) {
	for i := range el {
		if el[i].Hash == hash {
			return i, nil
		}
	}
	// not found
	return -1, &HashError{Hash: hash}
}

// check if an entry contains a given targetId
func containsTarget(entry KeyEntry, targetId string) bool {
	for _, tid := range entry.TargetIds {
		if tid == targetId {
			return true
		}
	}
	// not found
	return false
}

func lastIndex(el entryList, targetId string) (int, error) {
	for i := len(el) - 1; i >= 0; i-- {
		if containsTarget(el[i], targetId) {
			return i, nil
		}
	}
	// not found
	return -1, &TargetError{Target: targetId}
}

// remove the specified entry while preserving order
func removeEntry(el entryList, idx int) entryList {
	return append(el[:idx], el[idx+1:]...)
}

// remove the specified target while preserving order
func removeTarget(el entryList, targetId string, hard bool) (entryList, error) {
	// base case: remove first instance
	idx, err := lastIndex(el, targetId)
	if err != nil {
		// for the sake of the recursive case, just return what we got
		return el, err
	}

	// we know the entry at el[idx] contains the target
	matchIndex := -1
	for i := range el[idx].TargetIds {
		if el[idx].TargetIds[i] == targetId {
			matchIndex = i
			break
		}
	}
	if matchIndex == -1 {
		// TODO:...
		return nil, fmt.Errorf("SOMETHING REALLY BAD HAPPENED")
	}

	// remove the target
	// TODO: ick
	// TODO: need to test this to make sure it's safe anyway
	el[idx].TargetIds = append(el[idx].TargetIds[:matchIndex], el[idx].TargetIds[matchIndex+1:]...)

	// recursive case: delete until we hit an error
	if hard {
		el, _ = removeTarget(el, targetId, hard)
		// TODO: should make sure err is targetError...
	}

	return el, nil
}
