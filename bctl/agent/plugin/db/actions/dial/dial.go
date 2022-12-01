package dial

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/crunchydata/crunchy-proxy/protocol"
	"gopkg.in/tomb.v2"

	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/plugin/db"
	"bastionzero.com/bctl/v1/bzerolib/plugin/db/actions/dial"
	smsg "bastionzero.com/bctl/v1/bzerolib/stream/message"
)

const (
	chunkSize      = 64 * 1024
	writeDeadline  = 5 * time.Second
	dialTCPTimeout = 30 * time.Second
)

type Dial struct {
	tmb    tomb.Tomb
	logger *logger.Logger

	// channel for letting the plugin know we're done
	doneChan chan struct{}

	// output channel to send all of our stream messages directly to datachannel
	streamOutputChan     chan smsg.StreamMessage
	streamMessageVersion smsg.SchemaVersion

	requestId        string
	remoteAddress    *net.TCPAddr
	remoteConnection *net.Conn
}

func New(logger *logger.Logger,
	ch chan smsg.StreamMessage,
	doneChan chan struct{},
	remoteHost string,
	remotePort int) (*Dial, error) {

	// Build our address
	address := fmt.Sprintf("%s:%v", remoteHost, remotePort)

	// Open up a connection to the TCP addr we are trying to connect to
	if raddr, err := net.ResolveTCPAddr("tcp", address); err != nil {
		logger.Errorf("Failed to resolve remote address: %s", err)
		return nil, fmt.Errorf("failed to resolve remote address: %s", err)
	} else {
		return &Dial{
			logger:           logger,
			doneChan:         doneChan,
			streamOutputChan: ch,
			remoteAddress:    raddr,
		}, nil
	}
}

func (d *Dial) Kill() {
	if !d.tmb.Alive() {
		return
	}

	d.tmb.Kill(nil)
	if d.remoteConnection != nil {
		(*d.remoteConnection).Close()
	}
	d.tmb.Wait()
}

func (d *Dial) Receive(action string, actionPayload []byte) ([]byte, error) {
	var err error

	switch dial.DialSubAction(action) {
	case dial.DialStart:
		var dialActionRequest dial.DialActionPayload
		if err = json.Unmarshal(actionPayload, &dialActionRequest); err != nil {
			err = fmt.Errorf("malformed dial action payload %v", actionPayload)
			break
		}
		return d.start(dialActionRequest, action)
	case dial.DialInput:

		// Deserialize the action payload, the only action passed is input
		var dbInput dial.DialInputActionPayload
		if err = json.Unmarshal(actionPayload, &dbInput); err != nil {
			err = fmt.Errorf("unable to unmarshal dial input message: %s", err)
			break
		}

		// Then send the data to our remote connection, decode the data first
		if dataToWrite, nerr := base64.StdEncoding.DecodeString(dbInput.Data); nerr != nil {
			err = nerr
			break
		} else {

			// Send this data to our remote connection
			d.logger.Info("Received data from daemon, forwarding to remote tcp connection")

			// Set a deadline for the write so we don't block forever
			(*d.remoteConnection).SetWriteDeadline(time.Now().Add(writeDeadline))
			if _, err := (*d.remoteConnection).Write(dataToWrite); !d.tmb.Alive() {
				return []byte{}, nil
			} else if err != nil {
				d.logger.Errorf("error writing to local TCP connection: %s", err)
				d.Kill()
			}
		}

	case dial.DialStop:
		d.Kill()
		return actionPayload, nil
	default:
		err = fmt.Errorf("unhandled stream action: %v", action)
	}

	if err != nil {
		d.logger.Error(err)
	}
	return []byte{}, err
}

func (d *Dial) start(dialActionRequest dial.DialActionPayload, action string) ([]byte, error) {
	// keep track of who we're talking to
	d.requestId = dialActionRequest.RequestId
	d.logger.Infof("Setting request id: %s", d.requestId)
	d.streamMessageVersion = dialActionRequest.StreamMessageVersion
	d.logger.Infof("Setting stream message version: %s", d.streamMessageVersion)

	var remoteConnection net.Conn
	var err error
	if dialActionRequest.TargetUser == "" {
		remoteConnection, err = net.DialTimeout("tcp", d.remoteAddress.String(), dialTCPTimeout)
	} else {
		remoteConnection, err = d.Connect(d.remoteAddress.String())
	}

	// For each start, call the dial the TCP address
	if err != nil {
		d.logger.Errorf("Failed to dial remote address: %s", err)
		return []byte{}, err
	} else {
		d.remoteConnection = &remoteConnection
	}

	// Setup a go routine to listen for messages coming from this local connection and send to daemon
	d.tmb.Go(func() error {
		defer close(d.doneChan)

		sequenceNumber := 0
		buff := make([]byte, chunkSize)

		for {
			// this line blocks until it reads output or error
			if n, err := (*d.remoteConnection).Read(buff); !d.tmb.Alive() {
				return nil
			} else if err != nil {
				if err == io.EOF {
					d.logger.Infof("db dial connection closed")

					// Let our daemon know that we have got the error and we need to close the connection
					switch d.streamMessageVersion {
					// prior to 202204
					case "":
						d.sendStreamMessage(sequenceNumber, smsg.DbStreamEnd, false, buff[:n])
					default:
						d.sendStreamMessage(sequenceNumber, smsg.Stream, false, buff[:n])
					}
				} else {
					d.logger.Errorf("failed to read from tcp connection: %s", err)
					d.sendStreamMessage(sequenceNumber, smsg.Error, false, []byte(err.Error()))
				}

				return err
			} else {
				d.logger.Debugf("Sending %d bytes from local tcp connection to daemon", n)

				// Now send this to daemon
				switch d.streamMessageVersion {
				// prior to 202204
				case "":
					d.sendStreamMessage(sequenceNumber, smsg.DbStream, true, buff[:n])
				default:
					d.sendStreamMessage(sequenceNumber, smsg.Stream, true, buff[:n])
				}

				sequenceNumber += 1
			}
		}
	})

	// Update our remote connection
	return []byte{}, nil
}

func (d *Dial) sendStreamMessage(sequenceNumber int, streamType smsg.StreamType, more bool, contentBytes []byte) {
	d.streamOutputChan <- smsg.StreamMessage{
		SchemaVersion:  d.streamMessageVersion,
		SequenceNumber: sequenceNumber,
		Action:         string(db.Dial),
		Type:           streamType,
		More:           more,
		Content:        base64.StdEncoding.EncodeToString(contentBytes),
	}
}

func (d *Dial) Connect(host string) (net.Conn, error) {
	connection, err := net.Dial("tcp", host)

	if err != nil {
		return nil, err
	}

	d.logger.Info("SSL connections are enabled.")

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
	_, err = connection.Write(message.Bytes())

	if err != nil {
		d.logger.Errorf("Error sending SSL request to backend.")
		d.logger.Errorf("Error: %s", err.Error())
		return nil, err
	}

	/* Receive SSL response message. */
	response := make([]byte, 4096)
	_, err = connection.Read(response)

	if err != nil {
		d.logger.Errorf("Error receiving SSL response from backend.")
		d.logger.Errorf("Error: %s", err.Error())
		return nil, err
	}

	/*
	 * If SSL is not allowed by the backend then close the connection and
	 * throw an error.
	 */
	if len(response) > 0 && response[0] != 'S' {
		d.logger.Errorf("The backend does not allow SSL connections.")
		connection.Close()
	} else {
		d.logger.Debug("SSL connections are allowed by PostgreSQL.")
		d.logger.Debug("Attempting to upgrade connection.")
		connection = d.UpgradeClientConnection(host, connection)
		d.logger.Debug("Connection successfully upgraded.")
	}

	return connection, nil
}

func (d *Dial) UpgradeClientConnection(hostPort string, connection net.Conn) net.Conn {
	// hostname, _, _ := net.SplitHostPort(hostPort)
	tlsConfig := tls.Config{
		InsecureSkipVerify: true,
	}

	/* Add client SSL certificate and key. */
	d.logger.Debug("Loading SSL certificate and key")
	cert, _ := tls.LoadX509KeyPair("/home/ec2-user/certs/client.crt", "/home/ec2-user/certs/client.key")
	tlsConfig.Certificates = []tls.Certificate{cert}

	/* Add root CA certificate. */
	d.logger.Debug("Loading root CA.")
	tlsConfig.RootCAs = x509.NewCertPool()
	tlsConfig.RootCAs.AppendCertsFromPEM([]byte(`
	-----BEGIN CERTIFICATE-----
	MIIFczCCA1ugAwIBAgIRAOS3pZlUykM5zgm1N+pm0CswDQYJKoZIhvcNAQELBQAw
	UzEMMAoGA1UEBhMDVVNBMRYwFAYDVQQIEw1NYXNzYWNodXNldHRzMQ8wDQYDVQQH
	EwZCb3N0b24xGjAYBgNVBAoTEUJhc3Rpb25aZXJvLCBJbmMuMB4XDTIyMTAxOTIw
	MzgzMloXDTIzMTAxOTIwMzgzMlowUzEMMAoGA1UEBhMDVVNBMRYwFAYDVQQIEw1N
	YXNzYWNodXNldHRzMQ8wDQYDVQQHEwZCb3N0b24xGjAYBgNVBAoTEUJhc3Rpb25a
	ZXJvLCBJbmMuMIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA08XAuaYR
	w/ge3apqxgFyK0x15NE+bwVYwwR18qT3E5qjwsHWpO84W8O0naerc96ktuy77Q/3
	ozT0ILKb1Zm/k7evxTRC0riV6vLRH+S5EsPPMwKRy3j682K4i4upeeZi1YlCOPCj
	I4l2gBrHr3KCTzrkuijS0xVrBAM5zOuurg7SNfL3enflODDbKFLGheIOyyrl3JnU
	epGkVo7oZmZVF+dGoaO4OaZOEtUIgm9Dij0rtPrwQsXQzRTGYdlFYsKyrL3UnEyI
	jhPVSY0CAcrsgPcsLroXcK9oojk6oQ36n5divX83RgI2dGHsmnC7z/MweeZ2dmXS
	ZqsjPUgLHSBq5hFgXEV1gGNtPJIvOKNOn4kFtbAmg3upyBjGiA2bmNZPFg4Etglv
	8axK8RmF2sLud1soMtZiC78xlg++vmrfp71rp5ymWVmQucR5rmJChslke1sNMltQ
	V8xOnShYuIx9ttdEGukEHRAEptwF4yTfpwn+T0zhLJnTU9Ea2D+TMMqeOaD9JfMd
	DHpN5/CKvk1weJWe5cmphTcPMg1x/7fnMv2MUIZKSEEyTHAiCBxkZRrN3My+klil
	I+vShM7zS7c1XLPFZn6VN25TU4De8m6UcY3qg7JR5yXIHsR1f4Fazk2WJF03TiqI
	SRz9Vwe5qZiXBxK3eP4xYYZY9yhif6YVyXcCAwEAAaNCMEAwDgYDVR0PAQH/BAQD
	AgIEMA8GA1UdEwEB/wQFMAMBAf8wHQYDVR0OBBYEFL1w3h1kRkJnusxK4itJmQ8d
	QuF7MA0GCSqGSIb3DQEBCwUAA4ICAQBNIDmPPgMNwdgW/9nPwZlo2zlZ1EFWEHwA
	cXN/1aqVWXVSzir5gtz2s/zqBG07XjP1VvbiDGVhhRI6+/sq4iU3lW/+atFGCgLJ
	N4ojOncqPNj2tyVBn//4X2/GsW8D7jxfqgtP2tX0140iCZpy9Vs6Gv3hyVfjZPRD
	xtl74eZDITjnMcvAv+Wl7Dn3xV/4lRPQHMVyH2egs6ga+C+GcmWke7tc4JRI2A/c
	QBYsFlZlYy5/E9Is8jydTqdiQR/AVDo4wg60RGnuwap9YFXpKWn43hdUgBs3esoM
	oJSYNtJjCA3Z0ERc6uAa5/44o9dMaQN58EWkjfILF7jlSTgs9SYIzD0gO2HChHY5
	UIUIaHP8dedso7EFVIZ5/X0ROFOpGO5PD7PtneS22tSl69Z9Ekv4ZoFAVWLcerLd
	YnPJ6gGE8rClZc9BfQ8VgKiLB3Qw07WS5Y6QTIXnz/xWHXM0KeHgk+HDEjbhs5AZ
	N/e2NxZJZjunrMrZtiD/DYmFyyEjKFq9h3O3+UWsSQU8WHXBBz5R5t2UarFHBS2M
	eWog7yoRYOfhaJqqu04+iOg23UIxqCYnW5wQ7jtEeC7bGwp9G8Cj/5zXTVYMAR8l
	Ffa3KOUlrfEXIIkFUm4/np9kBAA9P0e8OPNAi5BgDrr+wW4J6wA5hZzmeJJb0S7U
	dT3KFaOUug==
	-----END CERTIFICATE-----
`))

	/* Upgrade the connection. */
	d.logger.Info("Upgrading to SSL connection.")
	client := tls.Client(connection, &tlsConfig)

	// err := d.verifyCertificateAuthority(client, &tlsConfig)
	// if err != nil {
	// 	d.logger.Errorf("Could not verify certificate authority: %s", err.Error())
	// 	return nil
	// } else {
	// 	d.logger.Info("Successfully verified CA")
	// }

	return client
}

/*
 * This function will perform a TLS handshake with the server and to verify the
 * certificates against the CA.
 *
 * client - the TLS client connection.
 * tlsConfig - the configuration associated with the connection.
 */
// func (d *Dial) verifyCertificateAuthority(client *tls.Conn, tlsConf *tls.Config) error {
// 	err := client.Handshake()

// 	if err != nil {
// 		return err
// 	}

// 	/* Get the peer certificates. */
// 	certs := client.ConnectionState().PeerCertificates

// 	/* Setup the verification options. */
// 	options := x509.VerifyOptions{
// 		DNSName:       client.ConnectionState().ServerName,
// 		Intermediates: x509.NewCertPool(),
// 		Roots:         tlsConf.RootCAs,
// 	}

// 	for i, certificate := range certs {
// 		/*
// 		 * The first certificate in the list is client certificate and not an
// 		 * intermediate certificate. Therefore it should not be added.
// 		 */
// 		if i == 0 {
// 			continue
// 		}

// 		options.Intermediates.AddCert(certificate)
// 	}

// 	/* Verify the client certificate.
// 	 *
// 	 * The first certificate in the certificate to verify.
// 	 */
// 	_, err = certs[0].Verify(options)

// 	return err
// }
