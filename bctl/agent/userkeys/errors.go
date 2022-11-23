package userkeys

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

// HashError means the config is valid but there is no entry with the given hash
type HashError struct{ Hash string }

func (e *HashError) Error() string { return fmt.Sprintf("no entry with hash: %s", e.Hash) }
func (e *HashError) Unwrap() error { return nil }

// TargetError means the config is valid but there is no entry with the given targetId
type TargetError struct{ Target string }

func (e *TargetError) Error() string { return fmt.Sprintf("no entry with targetId: %s", e.Target) }
func (e *TargetError) Unwrap() error { return nil }

// NoOpError means the requested change to the config would not change its state
type NoOpError struct{}

func (e *NoOpError) Error() string { return "Requested action is a no-op" }
func (e *NoOpError) Unwrap() error { return nil }
