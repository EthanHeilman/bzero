/*
package envconfig defines the interface for and implementations of a configuration object
that can be modified by separate parties who may not be aware of one another. It combines a persistent file
with environment variables so that running processes and initial machine setup are always consistent

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

// TODO: revisit this structure once I sort out yaml
// TODO: revisit documentation
type ECEntry struct {
	Value   string
	Comment string
	// TODO: last modified??
	Env string
}

// TODO: map[string]*Entry???
// FIXME: oh shit, this won't work because each key is actually associated with multiple entries
type entryMap map[string]ECEntry

type EnvConfig interface {
	// Set takes an Entry and returns the value written to the file. If Entry.EnvVar is unset or is set in
	// agreement with Entry.EnvVar, then Entry.Value is returned (and Entry.EnvVar is set to Entry.Value).
	// Otherwise, the value of Entry.EnvVar is both written to the underlying file and returned
	// TODO: pointer?
	Set(id string, entry *ECEntry) (string, error)

	// Get takes an id and returns a value. If Entry.EnvVar is set and disagrees with Entry.Value,
	// the value of Entry.EnvVar is both written to the underlying file and returned. If it is not set, Entry.Value is
	// returned and written to Entry.EnvVar
	//
	// returns a non-nil error if the value is not found in the file
	Get(id string) (string, error)

	// Delete takes an id and removes the corresponding Entry from the underlying config file. If hard == true, it also
	// unsets Entry.EnvVar
	Delete(id string, hard bool) error

	// DeleteAll clears the underlying config file. If hard == true, it also unsets Entry.EnvVar for every Entry in the file
	DeleteAll(hard bool) error
}

// a successful return from Reconcile guarantees that entry.Value and the value of entry.EnvVar are in agreement
func (e *ECEntry) Reconcile() error {
	// if the env var is set, see if we need to update the entry's value
	if envVal, ok := os.LookupEnv(e.Env); ok {
		if envVal != e.Value {
			e.Value = envVal
		}
		return nil
	}

	// otherwise, set the env var and return what was in the file
	return os.Setenv(e.Env, e.Value)
}
