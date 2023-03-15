package connection

import (
	"fmt"
	"time"
)

const (
	PolicyEditedErrTemplate  = "has been edited and does not provide access anymore"
	PolicyDeletedErrTemplate = "has been deleted"
	IdleTimeoutTemplate      = "Closing connection after idle timeout"
)

// The PolicyEditedConnectionClosedError is used when a dataconnection is closed by the bastion because the policy allowing it
// was edited, removing the access. It should generally be treated as a failure / nonzero exit code
type PolicyEditedConnectionClosedError struct {
	Reason string
}

func (e *PolicyEditedConnectionClosedError) Error() string { return e.Reason }

func (e *PolicyEditedConnectionClosedError) Unwrap() error { return nil }

// The PolicyDeletedConnectionClosedError is used when a dataconnection is closed by the bastion because the policy allowing it
// was deleted (possibly by JIT expiration) removing the access. It should generally be treated as a failure / nonzero exit code
type PolicyDeletedConnectionClosedError struct {
	Reason string
}

func (e *PolicyDeletedConnectionClosedError) Error() string { return e.Reason }

func (e *PolicyDeletedConnectionClosedError) Unwrap() error { return nil }

// The IdleTimeoutConnectionClosedError is used when a dataconnection is closed
// by an agent after an extended period of time of inactivity from the daemon.
type IdleTimeoutConnectionClosedError struct {
	Reason string
}

func NewIdleTimeoutConnectionClosedError(idleTimeout time.Duration) *IdleTimeoutConnectionClosedError {
	idleTimeoutErrMsg := fmt.Sprintf("%s of %s.", IdleTimeoutTemplate, idleTimeout)
	return &IdleTimeoutConnectionClosedError{Reason: idleTimeoutErrMsg}
}

func (e *IdleTimeoutConnectionClosedError) Error() string { return e.Reason }

func (e *IdleTimeoutConnectionClosedError) Unwrap() error { return nil }
