package unixuser

// this is an error returned when a user does not have the correct permissions
type PermissionDeniedError string

func (e PermissionDeniedError) Error() string {
	return "permission denied: " + string(e)
}

const UserNotFoundErrMsg = "unixuser error: user does not exist"

// The UserNotFoundError is used when the user attempts to connect as a user for which they have policy access,
// but which does not exist on the target. It should be treated as an error / exit code > 0
type UserNotFoundError struct{}

func (e *UserNotFoundError) Error() string { return UserNotFoundErrMsg }

func (e *UserNotFoundError) Unwrap() error { return nil }
