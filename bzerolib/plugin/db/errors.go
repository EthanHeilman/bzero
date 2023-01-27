package db

import "fmt"

// ConnectionRefused is used when the agent cannot make a connection to the specified tcp address
const ConnectionRefusedString = "connection refused"

type ConnectionRefusedError struct {
	InnerError error
}

func NewConnectionRefusedError(err error) error {
	return &ConnectionRefusedError{
		InnerError: err,
	}
}

func (e *ConnectionRefusedError) Error() string {
	if e.InnerError == nil {
		return ConnectionRefusedString
	}
	return fmt.Sprintf("%s: %s", ConnectionRefusedString, e.InnerError)
}

func (e *ConnectionRefusedError) Unwrap() error { return e.InnerError }

// This error is for failing to establish a connection, there is something listening on the process but
// we failed somewhere in the process of making that connection
const ConnectionFailedErrorString = "failed to establish connection"

type ConnectionFailedError struct {
	InnerError error
}

func NewConnectionFailedError(err error) error {
	return &ConnectionFailedError{
		InnerError: err,
	}
}

func (e *ConnectionFailedError) Error() string {
	if e.InnerError == nil {
		return ConnectionFailedErrorString
	}
	return fmt.Sprintf("%s: %s", ConnectionFailedErrorString, e.InnerError)
}

func (e *ConnectionFailedError) Unwrap() error { return e.InnerError }

// PwdbConfigErrors are best guesses that the database has been misconfigured leading to issues in
// the pwdb auth process
const DBNoTLSErrorString = "database does not accept ssl connections"

type DBNoTLSError struct{}

func (e *DBNoTLSError) Error() string {
	return DBNoTLSErrorString
}

// This error is triggered if bastion fails to co-sign client certificate
const ClientCertCosignErrorString = "bastion failed to cosign client certificate"

type ClientCertCosignError struct {
	InnerError error
}

func NewClientCertCosignError(err error) error {
	return &ClientCertCosignError{
		InnerError: err,
	}
}

func (e *ClientCertCosignError) Error() string {
	if e.InnerError == nil {
		return ClientCertCosignErrorString
	}
	return fmt.Sprintf("%s: %s", ClientCertCosignErrorString, e.InnerError)
}

func (e *ClientCertCosignError) Unwrap() error { return e.InnerError }

// Something has gone wrong with the authentication process whether it's on our side or on the database
// const PwdbAuthenticationErrorString = "SplitCert authentication error"

// type PwdbAuthenticationError struct{}

// func (e *PwdbAuthenticationError) Error() string { return PwdbAuthenticationErrorString }

// Something has gone wrong with the authentication process whether it's on our side or on the database
const PwdbMissingKeyErrorString = "missing SplitCert key"

type PwdbMissingKeyError struct {
	InnerError error
}

func NewMissingKeyError(err error) error {
	return &PwdbMissingKeyError{
		InnerError: err,
	}
}

func (e *PwdbMissingKeyError) Error() string {
	if e.InnerError == nil {
		return PwdbMissingKeyErrorString
	}
	return fmt.Sprintf("%s: %s", PwdbMissingKeyErrorString, e.InnerError)
}

func (e *PwdbMissingKeyError) Unwrap() error { return e.InnerError }
