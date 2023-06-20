package pwdb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"cloud.google.com/go/cloudsqlconn"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"google.golang.org/api/impersonate"
	"google.golang.org/grpc/test/bufconn"
	"gopkg.in/tomb.v2"

	"bastionzero.com/agent/bastion"
	"bastionzero.com/agent/config/keyshardconfig/data"
	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/plugin/db"
	"bastionzero.com/bzerolib/plugin/db/actions/pwdb"
	smsg "bastionzero.com/bzerolib/stream/message"
)

const (
	chunkSize      = 128 * 1024 // 128 kB
	writeDeadline  = 5 * time.Second
	dialTCPTimeout = 30 * time.Second
)

type Dialer interface {
	Dial(network string, hostname string) (net.Conn, error)
}
type NetDialer struct{}

func (n *NetDialer) Dial(network string, dbEndPoint string) (net.Conn, error) {
	return net.Dial(network, dbEndPoint)
}

func RealRDSTokenBuilder(dbEndpoint string, region string, dbUser string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(time.Second*60))
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", err
	}
	return auth.BuildAuthToken(
		ctx,
		dbEndpoint, // Database Endpoint (With Port)
		region,     // AWS Region
		dbUser,     // Database Account
		cfg.Credentials,
	)
}

type PWDBConfig interface {
	LastKey(targetId string) (data.KeyEntry, error)
}

type Pwdb struct {
	tmb    tomb.Tomb
	logger *logger.Logger

	// channel for letting the plugin know we're done
	doneChan chan struct{}

	// output channel to send all of our stream messages directly to datachannel
	streamOutputChan     chan smsg.StreamMessage
	streamMessageVersion smsg.SchemaVersion

	// config for interacting with key shard store needed for pwdb
	keyshardConfig PWDBConfig

	bastionClient   bastion.ApiClient
	remoteHost      string
	remotePort      int
	remoteConn      net.Conn
	DbDialer        Dialer
	RDSTokenBuilder func(dbEndpoint string, region string, dbUser string) (string, error)
}

func New(logger *logger.Logger,
	ch chan smsg.StreamMessage,
	doneChan chan struct{},
	keyshardConfig PWDBConfig,
	bastion bastion.ApiClient,
	remoteHost string,
	remotePort int) (*Pwdb, error) {

	return &Pwdb{
		logger:           logger,
		doneChan:         doneChan,
		keyshardConfig:   keyshardConfig,
		bastionClient:    bastion,
		streamOutputChan: ch,
		remoteHost:       remoteHost,
		remotePort:       remotePort,
		DbDialer:         &NetDialer{}, // This is used for tests so they can hook the net.Conn to the database
		RDSTokenBuilder:  RealRDSTokenBuilder,
	}, nil
}

func (p *Pwdb) Kill() {
	if p.tmb.Alive() {
		p.tmb.Kill(nil)
		if p.remoteConn != nil {
			p.remoteConn.Close()
		}
		p.tmb.Wait()
	}
}

func (p *Pwdb) Receive(action string, actionPayload []byte) ([]byte, error) {
	switch pwdb.PwdbSubAction(action) {
	case pwdb.Connect:
		var connectReq pwdb.ConnectPayload
		if err := json.Unmarshal(actionPayload, &connectReq); err != nil {
			return nil, fmt.Errorf("malformed connect request payload: %s", err)
		}

		return nil, p.start(connectReq.TargetId, connectReq.TargetUser, action)
	case pwdb.Input:
		// Deserialize the action payload, the only action passed is inputReq
		var inputReq pwdb.InputPayload
		if err := json.Unmarshal(actionPayload, &inputReq); err != nil {
			return nil, fmt.Errorf("malformed input payload: %s", err)
		}

		if data, err := base64.StdEncoding.DecodeString(inputReq.Data); err != nil {
			return nil, fmt.Errorf("input message contained malformed base64 encoded data: %s", err)
		} else {
			return nil, p.writeToConnection(data)
		}
	case pwdb.Close:
		var closeReq pwdb.ClosePayload
		if err := json.Unmarshal(actionPayload, &closeReq); err != nil {
			return nil, fmt.Errorf("malformed close payload: %s", err)
		}

		p.logger.Infof("Closing because: %s", closeReq.Reason)

		p.Kill()
		return actionPayload, nil
	default:
		return nil, fmt.Errorf("unrecognized action: %s", action)
	}
}

func (p *Pwdb) start(targetId, targetUser, action string) error {

	// If this is a GCP connection use GCP IAM Authentication rather than IAM.
	if strings.HasPrefix(p.remoteHost, "gcp://") {
		// Make a GCP  connection using attach IAM service account to database
		if conn, err := p.gcpDial(targetUser); err != nil {
			return db.NewConnectionFailedError(err)
		} else {
			p.remoteConn = conn
		}
	} else if strings.HasPrefix(p.remoteHost, "rds://") {
		// Make a RDS  connection using an AWS IAM role account to database
		if conn, err := p.rdsDial(targetUser); err != nil {
			p.logger.Errorf("Failed to establish RDS psql connection, %v", err)
			return db.NewConnectionFailedError(err)
		} else {
			p.remoteConn = conn
			p.logger.Infof("Successfully established RDS psql connection")
		}
	} else {
		p.logger.Infof("Connecting to database at %s:%d", p.remoteHost, p.remotePort)

		// Grab our key shard data from the vault
		keydata, err := p.keyshardConfig.LastKey(targetId)
		if err != nil {
			return db.NewMissingKeyError(err)
		}
		p.logger.Info("Loaded SplitCert key")

		// Make a tls connection using pwdb to database
		if conn, err := p.connect(keydata, targetUser); err != nil {
			return db.NewConnectionFailedError(err)
		} else {
			p.remoteConn = conn
		}
		p.logger.Infof("Successfully established SplitCert connection")
	}

	// Read from connection and stream back to daemon
	p.tmb.Go(p.readFromConnection)

	return nil
}

func (p *Pwdb) writeToConnection(data []byte) error {
	if p.remoteConn == nil {
		return fmt.Errorf("attempted to write to connection before it was established")
	}

	// Set a deadline for the write so we don't block forever
	p.remoteConn.SetWriteDeadline(time.Now().Add(writeDeadline))
	if _, err := p.remoteConn.Write(data); !p.tmb.Alive() {
		return nil
	} else if err != nil {
		return fmt.Errorf("error writing to local connection: %s", err)
	}

	return nil
}

func (p *Pwdb) gcpDial(targetUser string) (net.Conn, error) {
	if !strings.HasPrefix(p.remoteHost, "gcp://") {
		return nil, fmt.Errorf("gcpDial called with remoteHost=%v and is non-conforming to the pattern for GCP hosts: gcp://*", p.remoteHost)
	}

	p.logger.Infof("Connecting to GCP database with instance name %s", p.remoteHost)
	instanceConnectionName := strings.Split(p.remoteHost, "gcp://")[1]

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(time.Second*60))
	defer cancel()

	// Get the GCP Token to let us talk and auth to the database
	gcpToken, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
		TargetPrincipal: targetUser,
		Scopes:          []string{"https://www.googleapis.com/auth/cloud-platform", "https://www.googleapis.com/auth/sqlservice.admin"},
	})
	if err != nil {
		return nil, err
	}
	dialer, err := cloudsqlconn.NewDialer(
		ctx,
		cloudsqlconn.WithIAMAuthNTokenSources(gcpToken, gcpToken),
		cloudsqlconn.WithIAMAuthN(),
	)
	if err != nil {
		return nil, err
	}
	if remoteConn, err := dialer.Dial(ctx, instanceConnectionName); err != nil {
		p.logger.Errorf("Failed to dial remote address: %s", err)
		return nil, err
	} else {
		p.logger.Infof("Successfully established GCP IAM connection")
		return remoteConn, nil
	}
}

func (p *Pwdb) rdsDial(targetUser string) (net.Conn, error) {
	p.logger.Infof("Connecting to RDS database with instance name %s", p.remoteHost)
	if !strings.HasPrefix(p.remoteHost, "rds://") ||
		!strings.Contains(p.remoteHost, ".rds.amazonaws.com") {
		return nil, fmt.Errorf("rdsDial called with remoteHost=%v and is non-conforming to the pattern for RDS hosts: rds://*.<region>.rds.amazonaws.com", p.remoteHost)
	}

	instanceConnectionName := strings.Split(p.remoteHost, "rds://")[1]

	dbUser := targetUser
	dbHost := instanceConnectionName

	dbPort := p.remotePort
	dbEndpoint := fmt.Sprintf("%s:%d", dbHost, dbPort)

	// Example DBbase "database-name.cdhu0l.us-east-1.rds.amazonaws.com"
	// Extract the region, us-east-1. from the end.
	hostParts := strings.Split(dbHost, ".")
	region := hostParts[len(hostParts)-4]

	authenticationToken, err := p.RDSTokenBuilder(
		dbEndpoint, // Database Endpoint (With Port)
		region,     // AWS Region
		dbUser,     // Database Account
	)
	if err != nil {
		return nil, err
	}
	p.logger.Debug("AWS Auth Token Granted")

	// It is security critical that we use a bufconn rather than a localhost
	//  socket because we add authentication to any connection that
	//  connects to this listener. By using bufconn, no one outside the Agent
	//  process can connet to this listener, this allows the Agent to  exclude
	//  all unapproved connection.
	ln := bufconn.Listen(4096)
	dialer := p.DbDialer

	proxyServer, sslProxyLn := psqlProxy(dbUser, authenticationToken, dbEndpoint, ln, dialer, p.logger)

	go func() {
		// To make sure we close and clean up all goroutines, we watch for a
		//  signal from the doneChan and Shutdown the ProxyServer. This should
		//  also shutdown the SSLProxy as well, but if through some accident
		//  the dialer has not been called the SSLProxy will hold a goroutine
		//  to accept connections from sslProxyLn. As a precaution we also
		//  attempt to close sslProxyLn.
		<-p.doneChan
		proxyServer.Shutdown()
		sslProxyLn.Close()
	}()

	p.logger.Debug("Started psql brokering proxy")
	lconn, err := ln.Dial()

	return lconn, err
}

func (p *Pwdb) readFromConnection() error {
	defer close(p.doneChan)

	sequenceNumber := 0
	buf := make([]byte, chunkSize)

	for {
		// this line blocks until it reads output or error
		if n, err := p.remoteConn.Read(buf); !p.tmb.Alive() {
			return nil
		} else if err != nil {
			if err == io.EOF {
				p.logger.Infof("connection closed")
				p.sendStreamMessage(sequenceNumber, smsg.Stream, false, buf[:n])
			} else {
				p.logger.Errorf("failed to read from connection: %s", err)
				p.sendStreamMessage(sequenceNumber, smsg.Error, false, []byte(err.Error()))
			}
			return err
		} else if n > 0 {
			p.sendStreamMessage(sequenceNumber, smsg.Stream, true, buf[:n])
			sequenceNumber += 1
		}
	}
}

func (p *Pwdb) sendStreamMessage(sequenceNumber int, streamType smsg.StreamType, more bool, contentBytes []byte) {
	p.logger.Debugf("Sending sequence number %d", sequenceNumber)
	p.streamOutputChan <- smsg.StreamMessage{
		SchemaVersion:  p.streamMessageVersion,
		SequenceNumber: sequenceNumber,
		Action:         string(db.Pwdb),
		Type:           streamType,
		More:           more,
		Content:        base64.StdEncoding.EncodeToString(contentBytes),
	}
}
