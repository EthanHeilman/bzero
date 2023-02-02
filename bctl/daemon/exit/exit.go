package exit

import (
	"errors"
	"os"

	"bastionzero.com/bctl/v1/bzerolib/bzos"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/mrtap/bzcert"
	"bastionzero.com/bctl/v1/bzerolib/plugin/db"
	bzshell "bastionzero.com/bctl/v1/bzerolib/plugin/shell"
	bzssh "bastionzero.com/bctl/v1/bzerolib/plugin/ssh"
	"bastionzero.com/bctl/v1/bzerolib/unix/unixuser"
)

// Daemon Exit Codes
const (
	Success                       = 0
	UnspecifiedError              = 1
	CancelledByUser               = 3
	UserNotFound                  = 4
	ZliConfigError                = 5
	ServiceAccountNotConfigured   = 6
	PolicyEditedConnectionClosed  = 7
	PolicyDeletedConnectionClosed = 8
	IdleTimeout                   = 9
	ConnectionRefused             = 10
	ConnectionFailed              = 11
	TLSDisabledError              = 12
	ClientCertCosignError         = 13
	PwdbMissingKey                = 14
	PwdbUnkownAuthority           = 15
	ServerCertificateExpired      = 16
	IncorrectServerName           = 17
	BZCertIdTokenError            = 18
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
	var certConfigError *bzcert.CertConfigError
	var serviceAccountError *bzcert.ServiceAccountError
	var policyEditedError *connection.PolicyEditedConnectionClosedError
	var policyDeletedError *connection.PolicyDeletedConnectionClosedError
	var idleTimeoutError *connection.IdleTimeoutConnectionClosedError
	var connectionRefused *db.ConnectionRefusedError
	var connectionFailed *db.ConnectionFailedError
	var tlsDisabledError *db.TLSDisabledError
	var clientCosignError *db.ClientCertCosignError
	var pwdbMissingKeyError *db.PwdbMissingKeyError
	var pwdbUnknownAuthorityError *db.PwdbUnknownAuthorityError
	var serverCertExpired *db.ServerCertificateExpired
	var incorrectServerName *db.IncorrectServerName

	// Check if the error is either a bzcert.InitialIdTokenError (IdP key
	// rotation) or bzcert.CurrentIdTokenError (id token needs to be
	// refreshed) token error and prompt user to re-login
	if errors.As(err, &initialIdTokenError) || errors.As(err, &currentIdTokenError) {
		logger.Errorf("Error constructing BastionZero certificate: %s", err)
		logger.Errorf("IdP tokens are invalid/expired. Please try to re-login with the zli")
		os.Exit(BZCertIdTokenError)
	} else if errors.As(err, &certConfigError) {
		logger.Errorf("Error parsing zli config file: %s", err)
		logger.Errorf("Please try to re-login with the zli")
		os.Exit(ZliConfigError)
	} else if errors.As(err, &serviceAccountError) {
		logger.Error(err)
		os.Exit(ServiceAccountNotConfigured)
	} else if errors.As(err, &userNotFoundError) {
		logger.Error(err)
		os.Exit(UserNotFound)
	} else if errors.As(err, &policyEditedError) {
		logger.Error(err)
		os.Exit(PolicyEditedConnectionClosed)
	} else if errors.As(err, &policyDeletedError) {
		logger.Error(err)
		os.Exit(PolicyDeletedConnectionClosed)
	} else if errors.As(err, &idleTimeoutError) {
		logger.Error(err)
		os.Exit(IdleTimeout)
	} else if errors.As(err, &shellQuitError) || errors.As(err, &osInterruptError) || errors.As(err, &sshStdinClosedError) {
		logger.Error(err)
		os.Exit(Success)
	} else if errors.As(err, &shellCancelledError) {
		logger.Error(err)
		os.Exit(CancelledByUser)
	} else if errors.As(err, &connectionRefused) {
		os.Exit(ConnectionRefused)
	} else if errors.As(err, &connectionFailed) {
		os.Exit(ConnectionFailed)
	} else if errors.As(err, &tlsDisabledError) {
		os.Exit(TLSDisabledError)
	} else if errors.As(err, &clientCosignError) {
		os.Exit(ClientCertCosignError)
	} else if errors.As(err, &pwdbMissingKeyError) {
		os.Exit(PwdbMissingKey)
	} else if errors.As(err, &pwdbUnknownAuthorityError) {
		os.Exit(PwdbUnkownAuthority)
	} else if errors.As(err, &serverCertExpired) {
		os.Exit(ServerCertificateExpired)
	} else if errors.As(err, &incorrectServerName) {
		os.Exit(IncorrectServerName)
	}

	logger.Errorf("exiting with error: %s", err)
	os.Exit(UnspecifiedError)
}
