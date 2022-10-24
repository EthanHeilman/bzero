package exit

import (
	"errors"
	"os"

	"bastionzero.com/bctl/v1/bzerolib/bzos"
	"bastionzero.com/bctl/v1/bzerolib/keysplitting/bzcert"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	bzshell "bastionzero.com/bctl/v1/bzerolib/plugin/shell"
	bzssh "bastionzero.com/bctl/v1/bzerolib/plugin/ssh"
	"bastionzero.com/bctl/v1/bzerolib/unix/unixuser"
)

// Daemon Exit Codes
const (
	Success            = 0
	UnspecifiedError   = 1
	BZCertIdTokenError = 2
	CancelledByUser    = 3
	UserNotFound       = 4
)

// This should be the one and only path by which the daemon exits;
// there should be exactly one invocation of this function in the entire codebase, in daemon.go
//
// Checks if the error is a specially handled error where we should exit the
// daemon process with a specific exit code
func HandleDaemonExit(err error, logger *logger.Logger) {
	if err == nil {
		os.Exit(Success)
	}
	// https://go.dev/blog/go1.13-errors targets
	var initialIdTokenError *bzcert.InitialIdTokenError
	var currentIdTokenError *bzcert.CurrentIdTokenError
	var osInterruptError *bzos.OsInterruptError
	var shellQuitError *bzshell.ShellQuitError
	var shellCancelledError *bzshell.ShellCancelledError
	var sshStdinClosedError *bzssh.SshStdinClosedError
	var userNotFoundError *unixuser.UserNotFoundError

	// Check if the error is either a bzcert.InitialIdTokenError (IdP key
	// rotation) or bzcert.CurrentIdTokenError (id token needs to be
	// refreshed) token error and prompt user to re-login
	if errors.As(err, &initialIdTokenError) || errors.As(err, &currentIdTokenError) {
		logger.Errorf("Error constructing BastionZero certificate: %s", err)
		logger.Errorf("IdP tokens are invalid/expired. Please try to re-login with the zli.")
		os.Exit(BZCertIdTokenError)
	} else if errors.As(err, &userNotFoundError) {
		logger.Errorf(err.Error())
		os.Exit(UserNotFound)
	} else if errors.As(err, &shellQuitError) || errors.As(err, &osInterruptError) || errors.As(err, &sshStdinClosedError) {
		logger.Errorf(err.Error())
		os.Exit(Success)
	} else if errors.As(err, &shellCancelledError) {
		logger.Errorf(err.Error())
		os.Exit(CancelledByUser)
	}

	logger.Errorf("exiting with error: %s", err)
	os.Exit(UnspecifiedError)
}
