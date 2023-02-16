package pwdb

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/config/keyshardconfig/data"
	"bastionzero.com/bctl/v1/bzerolib/plugin/db"
	"github.com/crunchydata/crunchy-proxy/protocol"
)

// ref: https://github.com/CrunchyData/crunchy-proxy/blob/64e9426fd4ad77ec1652850d607a23a1201468a5/connect/connect.go
func (p *Pwdb) connect(keyData data.KeyEntry, targetUser string) (net.Conn, error) {
	connection, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", p.remoteHost, p.remotePort), 5*time.Second)
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
		return nil, fmt.Errorf("error sending initial TLS request to database: %s", err)
	}

	/* Receive SSL response message. */
	response := make([]byte, 4096)
	if _, err := connection.Read(response); err != nil {
		return nil, fmt.Errorf("error receiving initial TLS response from database: %s", err)
	}

	/*
	 * If SSL is not allowed by the backend then close the connection and
	 * throw an error.
	 */
	if len(response) > 0 && response[0] != 'S' {
		connection.Close()
		return nil, &db.TLSDisabledError{}
	}
	p.logger.Info("SSL connections are allowed by the database")

	p.logger.Info("Attempting to upgrade connection...")
	connection, err = p.upgradeConnection(keyData, connection, targetUser)
	if err != nil {
		return nil, err
	}

	p.logger.Debug("Connection successfully upgraded")
	return connection, nil
}

// ref: https://github.com/CrunchyData/crunchy-proxy/blob/64e9426fd4ad77ec1652850d607a23a1201468a5/connect/connect.go
func (p *Pwdb) upgradeConnection(keyData data.KeyEntry, connection net.Conn, role string) (net.Conn, error) {
	// hostname, _, _ := net.SplitHostPort(hostPort)
	tlsConfig := tls.Config{
		ServerName: p.remoteHost,
		RootCAs:    x509.NewCertPool(),
	}

	p.logger.Info("Loading CA Certificate")
	tlsConfig.RootCAs.AppendCertsFromPEM([]byte(keyData.CaCertPem))

	p.logger.Info("Loading client SSL certificate and key")
	cert, err := p.generateClientCert(keyData, role)
	if err != nil {
		return nil, err
	}
	tlsConfig.Certificates = []tls.Certificate{cert}

	p.logger.Info("Upgrading to SSL connection")
	client := tls.Client(connection, &tlsConfig)

	p.logger.Info("Initiating SSL Handshake")
	// Handshake now instead of on first write so we can bubble up error at the right time
	var unknownRootCert x509.UnknownAuthorityError
	err = client.Handshake()
	if err == nil {
		return client, nil
	}

	// If we're parsing an error, then we need to make sure we're closing the connection after
	defer connection.Close()

	if errors.As(err, &unknownRootCert) {
		return nil, db.NewPwdbUnknownAuthorityError(err)
	} else if strings.Contains(err.Error(), db.ServerCertificateExpiredString) {
		return nil, db.NewServerCertificateExpired(err)
	} else if strings.Contains(err.Error(), db.IncorrectServerNameString) {
		return nil, db.NewIncorrectServerName(err)
	} else {
		return nil, err
	}
}
