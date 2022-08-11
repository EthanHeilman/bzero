package bzos

// The OsInterruptError is used when the daemon is interrupted by SIGINT, SIGTERM, or SIGQUIT
type OsInterruptError struct{}

func (e *OsInterruptError) Error() string { return "interrupted by OS signal" }

func (e *OsInterruptError) Unwrap() error { return nil }
