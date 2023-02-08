package db

import "fmt"

// ConnectionRefused is used when the agent cannot make a connection to the specified tcp address
const ConnectionRefusedString = "connection refused"

type ConnectionRefusedError struct {
	innerError error
}

func NewConnectionRefusedError(err error) error {
	return &ConnectionRefusedError{
		innerError: err,
	}
}

func (e *ConnectionRefusedError) Error() string {
	if e.innerError == nil {
		return ConnectionRefusedString
	}
	return fmt.Sprintf("%s: %s", ConnectionRefusedString, e.innerError)
}

func (e *ConnectionRefusedError) Unwrap() error { return e.innerError }

// This error is for failing to establish a connection, there is something listening on the process but
// we failed somewhere in the process of making that connection
const ConnectionFailedErrorString = "failed to establish connection"

type ConnectionFailedError struct {
	innerError error
}

func NewConnectionFailedError(err error) error {
	return &ConnectionFailedError{
		innerError: err,
	}
}

func (e *ConnectionFailedError) Error() string {
	if e.innerError == nil {
		return ConnectionFailedErrorString
	}
	return fmt.Sprintf("%s: %s", ConnectionFailedErrorString, e.innerError)
}

func (e *ConnectionFailedError) Unwrap() error { return e.innerError }

// PwdbConfigErrors are best guesses that the database has been misconfigured leading to issues in
// the pwdb auth process
const TLSDisabledErrorString = "database does not accept ssl connections"

type TLSDisabledError struct{}

func (e *TLSDisabledError) Error() string {
	return TLSDisabledErrorString
}

// This error is triggered if bastion fails to co-sign client certificate
const ClientCertCosignErrorString = "bastion failed to cosign client certificate"

type ClientCertCosignError struct {
	innerError error
}

func NewClientCertCosignError(err error) error {
	return &ClientCertCosignError{
		innerError: err,
	}
}

func (e *ClientCertCosignError) Error() string {
	if e.innerError == nil {
		return ClientCertCosignErrorString
	}
	return fmt.Sprintf("%s: %s", ClientCertCosignErrorString, e.innerError)
}

func (e *ClientCertCosignError) Unwrap() error { return e.innerError }

// Something has gone wrong with the authentication process whether it's on our side or on the database
const PwdbMissingKeyErrorString = "missing SplitCert key"

type PwdbMissingKeyError struct {
	innerError error
}

func NewMissingKeyError(err error) error {
	return &PwdbMissingKeyError{
		innerError: err,
	}
}

func (e *PwdbMissingKeyError) Error() string {
	if e.innerError == nil {
		return PwdbMissingKeyErrorString
	}
	return fmt.Sprintf("%s: %s", PwdbMissingKeyErrorString, e.innerError)
}

func (e *PwdbMissingKeyError) Unwrap() error { return e.innerError }

// This error is for mismatched ca certs to catch pwdb db misconfigurations
// e.g. In the case where the certificate on the agent has been updated but not at the db
const UnrecognizedRootCertErrorString = "certificate signed by unknown authority"

type PwdbUnknownAuthorityError struct {
	innerError error
}

func NewPwdbUnknownAuthorityError(err error) error {
	return &PwdbUnknownAuthorityError{
		innerError: err,
	}
}

func (e *PwdbUnknownAuthorityError) Error() string {
	if e.innerError == nil {
		return UnrecognizedRootCertErrorString
	}
	return fmt.Sprintf("%s: %s", UnrecognizedRootCertErrorString, e.innerError)
}

func (e *PwdbUnknownAuthorityError) Unwrap() error { return e.innerError }

// Error to communicate if a certificate has expired
const ServerCertificateExpiredString = "certificate has expired"

type ServerCertificateExpired struct {
	err error
}

func NewServerCertificateExpired(err error) error {
	return &ServerCertificateExpired{
		err: err,
	}
}

func (e *ServerCertificateExpired) Error() string {
	return e.err.Error()
}

// Error to catch mismatched server name on certificate
const IncorrectServerNameString = "certificate is not valid for any names"

type IncorrectServerName struct {
	err error
}

func NewIncorrectServerName(err error) error {
	return &IncorrectServerName{
		err: err,
	}
}

func (e *IncorrectServerName) Error() string {
	return e.err.Error()
}
