package config

import (
	"errors"
	"fmt"
	"sync"

	"bastionzero.com/bctl/v1/bctl/agent/config/data"
)

type keyShardConfigClient interface {
	FetchKeyShardData() (data.KeyShardData, error)
	Save(d interface{}) error
}

/*
A KeyShardConfig object contains an ordered list of key shards and the target(s) they map to. The interface
provides methods for inserting new keys (which will take precedence over existing ones for a given target),
adding new targets to an existing key, and deleting both keys and targets
*/
type KeyShardConfig struct {
	lock   sync.RWMutex
	data   data.KeyShardData
	client keyShardConfigClient
}

func LoadKeyShardConfig(client keyShardConfigClient) (*KeyShardConfig, error) {
	if data, err := client.FetchKeyShardData(); err != nil {
		return nil, configFetchError(err.Error())
	} else {
		return &KeyShardConfig{
			client: client,
			data:   data,
		}, nil
	}
}

// Attempt to add a new key->targets entry to the configuration. If the entry does not exist in the config,
// a new "last" entry is created. For all of newEntry.TargetIds, calling LastKey will return newEntry
//
// If the entry already exists, any newEntry.TargetIds absent from the existing entry are added to it.
// If all of newEntry.TargetIds are already present in the existing entry, a NoOpError is returned.
func (c *KeyShardConfig) AddKey(newEntry data.KeyEntry) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.FetchKeyShardData()
	if err != nil {
		return configFetchError(err.Error())
	}

	if idx, err := findEntry(current, newEntry.Key); err == nil {
		var addedSomeTargets bool
		for _, targetId := range newEntry.TargetIds {
			// add any new targets
			if !containsTarget(current[idx], targetId) {
				current[idx].TargetIds = append(current[idx].TargetIds, targetId)
				addedSomeTargets = true
			}
		}
		// let the caller know if we didn't do anything
		if !addedSomeTargets {
			return &NoOpError{}
		}
	} else {
		// if the new entry doesn't already exist, just append it
		current = append(current, newEntry)
	}

	c.data = current

	if err := c.client.Save(c.data); err != nil {
		return configSaveError(err.Error())
	}
	return nil
}

// Add a target to the entry matching the key. If there is no such entry, a KeyError is returned.
// If the target is already present in that entry, a NoOpError is returned
func (c *KeyShardConfig) AddTarget(key data.SplitPrivateKey, targetId string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.FetchKeyShardData()
	if err != nil {
		return configFetchError(err.Error())
	}

	idx, err := findEntry(current, key)
	if err != nil {
		return err
	}

	if containsTarget(current[idx], targetId) {
		return &NoOpError{}
	}

	current[idx].TargetIds = append(current[idx].TargetIds, targetId)

	c.data = current

	if err := c.client.Save(c.data); err != nil {
		return configSaveError(err.Error())
	}
	return nil
}

// Get the most recent key data for the given target. If the target is not present in any entry, a TargetError is returned
func (c *KeyShardConfig) LastKey(targetId string) (data.SplitPrivateKey, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	current, err := c.client.FetchKeyShardData()
	if err != nil {
		return data.SplitPrivateKey{}, configFetchError(err.Error())
	}

	idx, err := lastIndex(current, targetId)
	if err != nil {
		return data.SplitPrivateKey{}, err
	}

	return current[idx].Key, nil
}

// Remove an entire key->targets entry from the configuration. If there is no such entry, a KeyError is returned
func (c *KeyShardConfig) DeleteKey(key data.SplitPrivateKey) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.FetchKeyShardData()
	if err != nil {
		return configFetchError(err.Error())
	}

	idx, err := findEntry(current, key)
	if err != nil {
		return err
	}

	c.data = removeEntry(current, idx)

	if err := c.client.Save(c.data); err != nil {
		return configSaveError(err.Error())
	}
	return nil
}

// Remove a target from its most recent entry. If the target is not present in any entry, a TargetError is returned.
//
// If hard == true, removes the target from all entries in which it is present
func (c *KeyShardConfig) DeleteTarget(targetId string, hard bool) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.FetchKeyShardData()
	if err != nil {
		return configFetchError(err.Error())
	}

	afterDeletion, err := removeTarget(current, targetId, hard)
	if err != nil {
		return err
	}

	c.data = afterDeletion

	if err := c.client.Save(c.data); err != nil {
		return configSaveError(err.Error())
	}
	return nil
}

// get the index matching the given key
func findEntry(keyShards data.KeyShardData, key data.SplitPrivateKey) (int, error) {
	for i := range keyShards {
		if keyShards[i].Key.D == key.D {
			return i, nil
		}
	}
	// not found
	return -1, &KeyError{Key: key}
}

// check if an entry contains a given targetId
func containsTarget(entry data.KeyEntry, targetId string) bool {
	for _, tid := range entry.TargetIds {
		if tid == targetId {
			return true
		}
	}
	// not found
	return false
}

func lastIndex(keyShards data.KeyShardData, targetId string) (int, error) {
	for i := len(keyShards) - 1; i >= 0; i-- {
		if containsTarget(keyShards[i], targetId) {
			return i, nil
		}
	}
	// not found
	return -1, &TargetError{Target: targetId}
}

// remove the specified entry while preserving order
func removeEntry(keyShards data.KeyShardData, idx int) data.KeyShardData {
	return append(keyShards[:idx], keyShards[idx+1:]...)
}

// remove the specified target while preserving order
func removeTarget(keyShards data.KeyShardData, targetId string, hard bool) (data.KeyShardData, error) {
	// base case: remove first instance
	idx, err := lastIndex(keyShards, targetId)
	if err != nil {
		// for the sake of the recursive case, just return what we got
		return keyShards, err
	}

	// we know the entry at keyShards[idx] contains the target
	match := -1
	for i := range keyShards[idx].TargetIds {
		if keyShards[idx].TargetIds[i] == targetId {
			match = i
			break
		}
	}
	if match == -1 {
		// this really shouldn't be possible; better to check it than panic though!
		return nil, fmt.Errorf("underlying list corrupted; the lock may be violated")
	}

	// remove the target
	keyShards[idx].TargetIds = append(keyShards[idx].TargetIds[:match], keyShards[idx].TargetIds[match+1:]...)

	// recursive case: delete until we hit a target error
	if hard {
		keyShards, err = removeTarget(keyShards, targetId, hard)
		var targetError *TargetError
		if err != nil && !errors.As(err, &targetError) {
			return nil, err
		}
	}

	return keyShards, nil
}
