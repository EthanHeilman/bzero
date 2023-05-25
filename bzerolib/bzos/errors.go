package bzos

// The ShutdownError is used when the daemon receives a graceful shutdown request via its control server
type ShutdownError struct{}

func (e *ShutdownError) Error() string { return "received a graceful shutdown request" }

func (e *ShutdownError) Unwrap() error { return nil }
