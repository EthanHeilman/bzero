package connection

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
