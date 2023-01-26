package config

import (
	"encoding/json"
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

// Returns a JSON representation of the data that can be loaded by another agent
func (c *KeyShardConfig) MarshalJSON() ([]byte, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	data, err := c.client.FetchKeyShardData()
	if err != nil {
		return nil, configFetchError(err.Error())
	}

	return json.Marshal(data)
}

// Attempt to add a new key->targets entry to the configuration. If the entry does not exist in the config,
// a new "last" entry is created. For all of newEntry.TargetIds, calling LastKey will return newEntry
//
// If the entry already exists, any newEntry.TargetIds absent from the existing entry are added to it.
// If all of newEntry.TargetIds are already present in the existing entry, a NoOpError is returned.
func (c *KeyShardConfig) AddKey(newEntry data.MappedKeyEntry) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.FetchKeyShardData()
	if err != nil {
		return configFetchError(err.Error())
	}

	if idx, err := findEntry(current, newEntry.KeyData); err == nil {
		var addedSomeTargets bool
		for _, targetId := range newEntry.TargetIds {
			// add any new targets
			if !containsTarget(current.Keys[idx], targetId) {
				current.Keys[idx].TargetIds = append(current.Keys[idx].TargetIds, targetId)
				addedSomeTargets = true
			}
		}
		// let the caller know if we didn't do anything
		if !addedSomeTargets {
			return &NoOpError{}
		}
	} else {
		// if the new entry doesn't already exist, just append it
		current.Keys = append(current.Keys, newEntry)
	}

	c.data = current

	if err := c.client.Save(c.data); err != nil {
		return configSaveError(err.Error())
	}
	return nil
}

// Add a target to all entries in the config
//
// If the target is already present in all entries, a NoOpError is returned
func (c *KeyShardConfig) AddTarget(targetId string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.FetchKeyShardData()
	if err != nil {
		return configFetchError(err.Error())
	}

	added := false
	for idx := range current.Keys {
		if !containsTarget(current.Keys[idx], targetId) {
			current.Keys[idx].TargetIds = append(current.Keys[idx].TargetIds, targetId)
			added = true
		}
	}

	if !added {
		return &NoOpError{}
	}

	c.data = current

	if err := c.client.Save(c.data); err != nil {
		return configSaveError(err.Error())
	}
	return nil
}

// Get the most recent key data for the given target. If the target is not present in any entry, a TargetError is returned
func (c *KeyShardConfig) LastKey(targetId string) (data.KeyEntry, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	current, err := c.client.FetchKeyShardData()
	if err != nil {
		return data.KeyEntry{}, configFetchError(err.Error())
	}

	idx, err := lastIndex(current, targetId)
	if err != nil {
		return data.KeyEntry{}, err
	}

	return current.Keys[idx].KeyData, nil
}

// Remove all keys from the config
//
// If the config was already empty, a NoOpError is returned
func (c *KeyShardConfig) Clear() error {
	c.lock.Lock()
	defer c.lock.Unlock()

	current, err := c.client.FetchKeyShardData()
	if err != nil {
		return configFetchError(err.Error())
	}

	if len(current.Keys) == 0 {
		return &NoOpError{}
	}

	if err := c.client.Save(data.KeyShardData{}); err != nil {
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
func findEntry(keyShards data.KeyShardData, key data.KeyEntry) (int, error) {
	for i := range keyShards.Keys {
		if keyShards.Keys[i].KeyData.KeyShardPem == key.KeyShardPem {
			return i, nil
		}
	}
	// not found
	return -1, &KeyError{}
}

// check if an entry contains a given targetId
func containsTarget(entry data.MappedKeyEntry, targetId string) bool {
	for _, tid := range entry.TargetIds {
		if tid == targetId {
			return true
		}
	}
	// not found
	return false
}

func lastIndex(keyShards data.KeyShardData, targetId string) (int, error) {
	for i := len(keyShards.Keys) - 1; i >= 0; i-- {
		if containsTarget(keyShards.Keys[i], targetId) {
			return i, nil
		}
	}
	// not found
	return -1, &TargetError{Target: targetId}
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
	for i := range keyShards.Keys[idx].TargetIds {
		if keyShards.Keys[idx].TargetIds[i] == targetId {
			match = i
			break
		}
	}
	if match == -1 {
		// this really shouldn't be possible; better to check it than panic though!
		return data.KeyShardData{}, fmt.Errorf("underlying list corrupted; the lock may be violated")
	}

	// remove the target
	keyShards.Keys[idx].TargetIds = append(keyShards.Keys[idx].TargetIds[:match], keyShards.Keys[idx].TargetIds[match+1:]...)

	// recursive case: delete until we hit a target error
	if hard {
		keyShards, err = removeTarget(keyShards, targetId, hard)
		var targetError *TargetError
		if err != nil && !errors.As(err, &targetError) {
			return data.KeyShardData{}, err
		}
	}

	return keyShards, nil
}
