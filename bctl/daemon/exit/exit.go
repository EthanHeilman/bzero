package exit

import (
	"errors"
	"os"

	"bastionzero.com/bctl/v1/bzerolib/bzos"
	"bastionzero.com/bctl/v1/bzerolib/keysplitting/bzcert"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	bzshell "bastionzero.com/bctl/v1/bzerolib/plugin/shell"
)

// Daemon Exit Codes
const (
	SUCCESS               = 0
	UNSPECIFIED_ERROR     = 1
	BZCERT_ID_TOKEN_ERROR = 2
	CANCELLED_BY_USER     = 3
)

// This should be the one and only path by which the daemon exits;
// there should be exactly one invocation of this function in the entire codebase, in daemon.go
//
// Checks if the error is a specially handled error where we should exit the
// daemon process with a specific exit code
func HandleDaemonExit(err error, logger *logger.Logger) {
	if err == nil {
		os.Exit(SUCCESS)
	}
	// https://go.dev/blog/go1.13-errors target
	// Check if the error is either a bzcert.InitialIdTokenError (IdP key
	// rotation) or bzcert.CurrentIdTokenError (id token needs to be
	// refreshed) token error and prompt user to re-login
	var initialIdTokenError *bzcert.InitialIdTokenError
	var currentIdTokenError *bzcert.CurrentIdTokenError
	var osInterruptError *bzos.OsInterruptError
	var shellQuitError *bzshell.ShellQuitError
	var shellCancelledError *bzshell.ShellCancelledError

	if errors.As(err, &initialIdTokenError) || errors.As(err, &currentIdTokenError) {
		logger.Errorf("Error constructing BastionZero certificate: %s", err)
		logger.Errorf("IdP tokens are invalid/expired. Please try to re-login with the zli.")
		os.Exit(BZCERT_ID_TOKEN_ERROR)
	} else if errors.As(err, &shellQuitError) || errors.As(err, &osInterruptError) {
		logger.Errorf(err.Error())
		os.Exit(SUCCESS)
	} else if errors.As(err, &shellCancelledError) {
		logger.Errorf(err.Error())
		os.Exit(CANCELLED_BY_USER)
	}

	logger.Errorf("exiting with error: %s", err)
	os.Exit(UNSPECIFIED_ERROR)
}