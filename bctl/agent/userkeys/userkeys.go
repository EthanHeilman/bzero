/*
package userkeys defines the interface of a data structure for accessing a user's split private keys
on the disk of the host machine

A UserKeys object contains an ordered list of key shards and the target(s) they map to. The interface
provides methods for inserting new keys (which will take precedence over existing ones for a given target),
adding new targets to an existing key, and deleting both keys and targets
*/

package userkeys

type PublicKey struct {
	N int64 `json:"n" yaml:"n"`
	E int   `json:"e" yaml:"e"`
}

type SplitPrivateKey struct {
	PublicKey PublicKey `json:"associatedPublicKey" yaml:"associatedPublicKey"`
	D         int64     `json:"d" yaml:"d"`
	E         int       `json:"e" yaml:"e"`
}

type KeyEntry struct {
	Key       SplitPrivateKey `json:"key" yaml:"key"`
	TargetIds []string        `json:"targetIds" yaml:"targetIds"`
}

// each id maps to a set of env vars; each env var maps to an Entry
type entryList []KeyEntry

type UserKeys interface {
	// Attempt to add a new key->targets entry to the configuration. If the entry does not exist in the config,
	// a new "last" entry is created. For all of newEntry.TargetIds, calling LastKey will return newEntry
	//
	// If the entry already exists, any newEntry.TargetIds absent from the existing entry are added to it.
	// If all of newEntry.TargetIds are already present in the existing entry, a NoOpError is returned.
	Add(newEntry KeyEntry) error

	// Add a target to the entry matching the key. If there is no such entry, a KeyError is returned.
	// If the target is already present in that entry, a NoOpError is returned
	AddTarget(key SplitPrivateKey, targetId string) error

	// Get the most recent key data for the given target. If the target is not present in any entry, a TargetError is returned
	LastKey(targetId string) (SplitPrivateKey, error)

	// Remove an entire key->targets entry from the configuration. If there is no such entry, a KeyError is returned
	DeleteKey(key SplitPrivateKey) error

	// Remove a target from its most recent entry. If the target is not present in any entry, a TargetError is returned.
	//
	// If hard, removes the target from all entries in which it is present
	DeleteTarget(targetId string, hard bool) error
}
