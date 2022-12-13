package config

import (
	"fmt"
)

type configFetchError string

func (e configFetchError) Error() string {
	return "failed to fetch config: " + string(e)
}

type configSaveError string

func (e configSaveError) Error() string {
	return "failed to save config: " + string(e)
}

// KeyError means the config is valid but there is no entry with the given key -- key is hashed to prevent it being logged
type KeyError struct{}

func (e *KeyError) Error() string {
	return "key not found"
}
func (e *KeyError) Unwrap() error { return nil }

// TargetError means the config is valid but there is no entry with the given targetId
type TargetError struct{ Target string }

func (e *TargetError) Error() string { return fmt.Sprintf("no entry with targetId %s", e.Target) }
func (e *TargetError) Unwrap() error { return nil }

// NoOpError means the requested change to the config would not change its state
type NoOpError struct{}

func (e *NoOpError) Error() string { return "Requested action is a no-op" }
func (e *NoOpError) Unwrap() error { return nil }
