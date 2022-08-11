package shell

// The ShellQuitError is used when the user exits a shell session;
// it should generally be treated as a successful termination / exit code 0
type ShellQuitError struct{}

func (e *ShellQuitError) Error() string { return "received shell quit stream message" }

func (e *ShellQuitError) Unwrap() error { return nil }

// The ShellCancelledError is used when the user aborts a shell session via signal before the connection has been made
// it should generally be treated as a successful termination / exit code 0
type ShellCancelledError struct{}

func (e *ShellCancelledError) Error() string { return "shell request cancelled by user" }

func (e *ShellCancelledError) Unwrap() error { return nil }
