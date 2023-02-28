package pwdb

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"gopkg.in/tomb.v2"

	"bastionzero.com/agent/config/keyshardconfig/data"
	"bastionzero.com/agent/plugin/db/actions/pwdb/client"
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

	bastion    *client.BastionClient
	remoteHost string
	remotePort int
	remoteConn net.Conn
}

func New(logger *logger.Logger,
	ch chan smsg.StreamMessage,
	doneChan chan struct{},
	keyshardConfig PWDBConfig,
	bastion *client.BastionClient,
	remoteHost string,
	remotePort int) (*Pwdb, error) {

	return &Pwdb{
		logger:           logger,
		doneChan:         doneChan,
		keyshardConfig:   keyshardConfig,
		bastion:          bastion,
		streamOutputChan: ch,
		remoteHost:       remoteHost,
		remotePort:       remotePort,
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
			p.logger.Tracef("Sending %d bytes from local tcp connection to daemon", n)
			p.sendStreamMessage(sequenceNumber, smsg.Stream, true, buf[:n])
			sequenceNumber += 1
		}
	}
}

func (p *Pwdb) sendStreamMessage(sequenceNumber int, streamType smsg.StreamType, more bool, contentBytes []byte) {
	p.logger.Infof("Sending sequence number %d", sequenceNumber)
	p.streamOutputChan <- smsg.StreamMessage{
		SchemaVersion:  p.streamMessageVersion,
		SequenceNumber: sequenceNumber,
		Action:         string(db.Pwdb),
		Type:           streamType,
		More:           more,
		Content:        base64.StdEncoding.EncodeToString(contentBytes),
	}
}
