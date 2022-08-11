package bzcert

import (
	"fmt"
)

// These errors follow to go convention of providing a Unwrap method which
// allows the inner error to be unwrapped further up the call stack and checked
// via errors.Is or errors.As. See https://go.dev/blog/go1.13-errors

type InitialIdTokenError struct {
	InnerError error
}

func (e *InitialIdTokenError) Error() string {
	return fmt.Sprintf("error verifying initial id token: %s", e.InnerError)
}

func (e *InitialIdTokenError) Unwrap() error { return e.InnerError }

type CurrentIdTokenError struct {
	InnerError error
}

func (e *CurrentIdTokenError) Error() string {
	return fmt.Sprintf("error verifying current id token: %s", e.InnerError)
}

func (e *CurrentIdTokenError) Unwrap() error { return e.InnerError }
