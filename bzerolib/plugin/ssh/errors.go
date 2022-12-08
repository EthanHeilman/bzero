package ssh

// The SshStdinClosedError is used when the connection to local SSH via stdin is closed normally
// it should generally be treated as a successful termination / exit code 0
type SshStdinClosedError struct{}

func (e *SshStdinClosedError) Error() string { return "finished reading from stdin" }

func (e *SshStdinClosedError) Unwrap() error { return nil }
