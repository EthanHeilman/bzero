package pwdb

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"

	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"github.com/crunchydata/crunchy-proxy/protocol"
)

type PWDBConfig interface {
	LastKey(targetId string) (data.KeyEntry, error)
}

// ref: https://github.com/CrunchyData/crunchy-proxy/blob/64e9426fd4ad77ec1652850d607a23a1201468a5/connect/connect.go
func Connect(logger *logger.Logger, keyData data.KeyEntry, host, role string) (net.Conn, error) {
	connection, err := net.Dial("tcp", host)
	if err != nil {
		return nil, err
	}

	logger.Info("SSL connections are enabled to the database")

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
		return nil, fmt.Errorf("error sending initial SSL request to backend: %s", err)
	}

	/* Receive SSL response message. */
	response := make([]byte, 4096)
	if _, err := connection.Read(response); err != nil {
		return nil, fmt.Errorf("error receiving initial SSL response from backend: %s", err)
	}

	/*
	 * If SSL is not allowed by the backend then close the connection and
	 * throw an error.
	 */
	if len(response) > 0 && response[0] != 'S' {
		connection.Close()
		return nil, fmt.Errorf("the database does not allow SSL connections")
	}
	logger.Info("SSL connections are allowed by the database")

	logger.Info("Attempting to upgrade connection...")
	connection, err = upgradeConnection(logger, keyData, connection, host, role)
	if err != nil {
		return nil, err
	}

	logger.Debug("Connection successfully upgraded.")
	return connection, nil
}

// ref: https://github.com/CrunchyData/crunchy-proxy/blob/64e9426fd4ad77ec1652850d607a23a1201468a5/connect/connect.go
func upgradeConnection(logger *logger.Logger, keyData data.KeyEntry, connection net.Conn, hostPort, role string) (net.Conn, error) {
	// hostname, _, _ := net.SplitHostPort(hostPort)
	tlsConfig := tls.Config{
		InsecureSkipVerify: true,
		RootCAs:            x509.NewCertPool(),
	}

	logger.Info("Loading CA Certificate")
	tlsConfig.RootCAs.AppendCertsFromPEM([]byte(keyData.CaCertPem))

	logger.Info("Loading client SSL certificate and key")
	if cert, err := tlsKeyPair(logger, keyData, role); err != nil {
		return nil, err
	} else {
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	logger.Info("Upgrading to SSL connection")
	client := tls.Client(connection, &tlsConfig)
	return client, nil
}
