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

type Entry struct {
	Id      string
	Value   string
	Comment string
	EnvVar  string
}

type EnvConfig interface {
	// Set takes an Entry and returns the value written to the file. If Entry.EnvVar is unset or is set in
	// agreement with Entry.EnvVar, then Entry.Value is returned (and Entry.EnvVar is set to Entry.Value).
	//	Otherwise, the value of Entry.EnvVar is both written to the underlying file and returned
	Set(entry Entry) (string, error)
	// Get takes an id and returns the corresponding Entry. If Entry.EnvVar is set and disagrees with Entry.Value,
	// the value of Entry.EnvVar is both written to the underlying file and returned. If it is not set, Entry.Value is
	// returned and written to Entry.EnvVar
	Get(id string) (Entry, error)
	// Delete takes an id and removes the corresponding Entry from the underlying config file. If hard == true, it also
	// unsets Entry.EnvVar
	Delete(id string, hard bool) error
	// DeleteAll clears the underlying config file. If hard == true, it also unsets Entry.EnvVar for every Entry in the file
	DeleteAll(hard bool) error
}
