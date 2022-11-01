/*
package envconfig defines the interface for and implementations of a configuration object that can be
modified by separate parties who may access the configuration in different ways. It combines a persistent
file with environment variables so that running processes and initial machine setup are always consistent

// FIXME: this ain't right...
An EnvConfig stores a collection of Entries in a file. Each Entry maps a unique identifier to a value,
the name of an environment variable, and an optional description comment. The key distinguishing feature
of the EnvConfig is that the environment variable takes precedence. When Setting or Getting an Entry, if
the value of the Entry's environment variable disagrees with the value stored in the file, the file will be
overwritten with the environment variable's value and returned. If the environment variable is not set,
Getting and Setting will set it to the file's value
*/

package envconfig

import (
	"os"
)

// TODO: revisit documentation given the new structure
type EnvEntry struct {
	Value   string
	Comment string
}

// each id maps to a set of env vars; each env var maps to an Entry
type entryMap map[string]map[string]*EnvEntry

// FIXME: these descriptions are all messed up
type EnvConfig interface {
	// Set takes an id and an Entry and returns the value added to id's array of Entries in the file.
	// If Entry.EnvVar is unset or is set in agreement with Entry.EnvVar, then Entry.Value is returned
	// (and Entry.EnvVar is set to Entry.Value if unset). Otherwise, the value of Entry.EnvVar is both
	// written to the underlying file and returned
	Set(id string, env string, entry *EnvEntry) (string, error)

	// Get takes an id and the name of an env var and returns a value. If Entry.EnvVar is set and disagrees with Entry.Value,
	// the value of Entry.EnvVar is both written to the underlying file and returned. If it is not set, Entry.Value is
	// returned and written to Entry.EnvVar
	//
	// returns a KeyError if the id is not found in the file and an EnvKeyError if the env var is not found among the id's Entries
	Get(id string, env string) (string, error)

	// Delete takes an id and the name of an env var and removes the corresponding Entry from the underlying config file.
	// If hard == true, it also unsets Entry.EnvVar
	Delete(id string, env string, hard bool) error

	// DeleteAll takes an id and clears all of its Entries. If hard == true, it also unsets Entry.EnvVar for every Entry
	DeleteAll(id string, hard bool) error
}

// a successful return from Reconcile guarantees that entry.Value and the value of env are in agreement
func (e *EnvEntry) Reconcile(idEnv string) error {
	// if the env var is set, see if we need to update the entry's value
	if envVal, ok := os.LookupEnv(idEnv); ok {
		if envVal != e.Value {
			e.Value = envVal
		}
		return nil
	}

	// otherwise, set the env var and return what was in the file
	return os.Setenv(idEnv, e.Value)
}
