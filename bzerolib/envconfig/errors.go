package envconfig

import "fmt"

// FileError means the config file could not be opened
type FileError struct {
	Path     string
	InnerErr error
}

func (e *FileError) Error() string {
	return fmt.Sprintf("unable to open file %s: %s", e.Path, e.InnerErr)
}

func (e *FileError) Unwrap() error { return e.InnerErr }

// ValidationError means the config contents are not valid
type ValidationError struct {
	InnerErr error
}

func (e *ValidationError) Error() string { return fmt.Sprintf("invalid config: %s", e.InnerErr) }
func (e *ValidationError) Unwrap() error { return e.InnerErr }

// KeyError means the config is valid but the requested key is not present
type KeyError struct{ Key string }

func (e *KeyError) Error() string { return fmt.Sprintf("no such key: %s", e.Key) }
func (e *KeyError) Unwrap() error { return nil }
