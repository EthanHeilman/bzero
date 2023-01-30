package pwdb

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"

	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/plugin/db"
	"github.com/crunchydata/crunchy-proxy/protocol"
)

// ref: https://github.com/CrunchyData/crunchy-proxy/blob/64e9426fd4ad77ec1652850d607a23a1201468a5/connect/connect.go
func Connect(logger *logger.Logger, serviceUrl string, keyData data.KeyEntry, host string, port int, role string) (net.Conn, error) {
	connection, err := net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, db.NewConnectionRefusedError(err)
	}

	/*
	 * First determine if SSL is allowed by the backend. To do this, send an
	 * SSL request. The response from the backend will be a single byte
	 * message. If the value is 'S', then SSL connections are allowed and an
	 * upgrade to the connection should be attempted. If the value is 'N',
	 * then the backend does not support SSL connections.
	 */

	/* Create the SSL request message. */
	message := protocol.NewMessageBuffer([]byte{})
	message.WriteInt32(8)
	message.WriteInt32(protocol.SSLRequestCode)

	/* Send the SSL request message. */
	if _, err := connection.Write(message.Bytes()); err != nil {
		return nil, fmt.Errorf("error sending initial SSL request to database: %s", err)
	}

	/* Receive SSL response message. */
	response := make([]byte, 4096)
	if _, err := connection.Read(response); err != nil {
		return nil, fmt.Errorf("error receiving initial SSL response from database: %s", err)
	}

	/*
	 * If SSL is not allowed by the backend then close the connection and
	 * throw an error.
	 */
	if len(response) > 0 && response[0] != 'S' {
		connection.Close()
		return nil, &db.DBNoTLSError{}
	}
	logger.Info("SSL connections are allowed by the database")

	logger.Info("Attempting to upgrade connection...")
	connection, err = upgradeConnection(logger, serviceUrl, keyData, connection, host, role)
	if err != nil {
		return nil, err
	}

	logger.Debug("Connection successfully upgraded")
	return connection, nil
}

// ref: https://github.com/CrunchyData/crunchy-proxy/blob/64e9426fd4ad77ec1652850d607a23a1201468a5/connect/connect.go
func upgradeConnection(logger *logger.Logger, serviceUrl string, keyData data.KeyEntry, connection net.Conn, hostName, role string) (net.Conn, error) {
	// hostname, _, _ := net.SplitHostPort(hostPort)
	tlsConfig := tls.Config{
		ServerName: hostName,
		RootCAs:    x509.NewCertPool(),
	}

	logger.Info("Loading CA Certificate")
	tlsConfig.RootCAs.AppendCertsFromPEM([]byte(keyData.CaCertPem))

	logger.Info("Loading client SSL certificate and key")
	if cert, err := generateClientCert(logger, serviceUrl, keyData, role); err != nil {
		return nil, err
	} else {
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	logger.Info("Upgrading to SSL connection")
	client := tls.Client(connection, &tlsConfig)

	logger.Info("Initiating SSL Handshake")
	// Handshake now instead of on first write so we can bubble up error at the right time
	var unknownRootCert x509.UnknownAuthorityError
	err := client.Handshake()
	if err != nil {
		logger.Infof("Error: %s", err)
	}

	if errors.As(err, &unknownRootCert) {
		return nil, db.NewPwdbUnknownAuthorityError(err)
	} else if strings.Contains(err.Error(), db.ServerCertificateExpiredString) {
		return nil, db.NewServerCertificateExpired(err)
	} else if err != nil {
		return nil, err
	}

	return client, nil
}
