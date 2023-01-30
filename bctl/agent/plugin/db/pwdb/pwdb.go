package pwdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"gopkg.in/tomb.v2"

	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/plugin/db"
	"bastionzero.com/bctl/v1/bzerolib/plugin/db/actions/pwdb"
	smsg "bastionzero.com/bctl/v1/bzerolib/stream/message"
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

	serviceUrl       string
	remoteHost       string
	remotePort       int
	remoteConnection *net.Conn
}

func New(logger *logger.Logger,
	ch chan smsg.StreamMessage,
	doneChan chan struct{},
	keyshardConfig PWDBConfig,
	serviceUrl string,
	remoteHost string,
	remotePort int) (*Pwdb, error) {

	return &Pwdb{
		logger:           logger,
		doneChan:         doneChan,
		keyshardConfig:   keyshardConfig,
		serviceUrl:       serviceUrl,
		streamOutputChan: ch,
		remoteHost:       remoteHost,
		remotePort:       remotePort,
	}, nil
}

func (p *Pwdb) Kill() {
	if p.tmb.Alive() {
		p.tmb.Kill(nil)
		if p.remoteConnection != nil {
			(*p.remoteConnection).Close()
		}
		p.tmb.Wait()
	}
}

func (p *Pwdb) Receive(action string, actionPayload []byte) ([]byte, error) {
	switch pwdb.PwdbSubAction(action) {
	case pwdb.Connect:
		var connectReq pwdb.PwdbConnectPayload
		if err := json.Unmarshal(actionPayload, &connectReq); err != nil {
			return nil, fmt.Errorf("malformed connect request payload: %s", err)
		}

		return nil, p.start(connectReq.TargetId, connectReq.TargetUser, action)
	case pwdb.Input:
		// Deserialize the action payload, the only action passed is inputReq
		var inputReq pwdb.PwdbInputPayload
		if err := json.Unmarshal(actionPayload, &inputReq); err != nil {
			return nil, fmt.Errorf("malformed input payload: %s", err)
		}

		return nil, p.writeToConnection(inputReq.Data)
	case pwdb.Close:
		p.Kill()
		return actionPayload, nil
	default:
		return nil, fmt.Errorf("unrecognized action: %s", action)
	}
}

func (p *Pwdb) start(targetId, targetUser, action string) error {
	p.logger.Infof("Connecting to database at %s:%s", p.remoteHost, p.remotePort)

	// Grab our key shard data from the vault
	keydata, err := p.keyshardConfig.LastKey(targetId)
	if err != nil {
		return db.NewMissingKeyError(err)
	}
	p.logger.Info("Loaded SplitCert key")

	// Make a tls connection using pwdb to database
	if conn, err := Connect(p.logger, p.serviceUrl, keydata, p.remoteHost, p.remotePort, targetUser); err != nil {
		return db.NewConnectionFailedError(err)
	} else {
		p.remoteConnection = &conn
	}
	p.logger.Infof("Successfully established SplitCert connection")

	// Read from connection and stream back to daemon
	p.tmb.Go(p.readFromConnection)

	return nil
}

func (p *Pwdb) writeToConnection(data []byte) error {
	if (*p.remoteConnection) == nil {
		return fmt.Errorf("attempted to write to connection for it was established")
	}
	// Send this data to our remote connection
	p.logger.Trace("Received data from daemon, writing to connection")

	// Set a deadline for the write so we don't block forever
	(*p.remoteConnection).SetWriteDeadline(time.Now().Add(writeDeadline))
	if _, err := (*p.remoteConnection).Write(data); !p.tmb.Alive() {
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
		if n, err := (*p.remoteConnection).Read(buf); !p.tmb.Alive() {
			return nil
		} else if n == 0 {
			continue
		} else if err != nil {
			if err == io.EOF {
				p.logger.Infof("connection closed")
				p.sendStreamMessage(sequenceNumber, smsg.Stream, false, buf[:n])
			} else {
				p.logger.Errorf("failed to read from connection: %s", err)
				p.sendStreamMessage(sequenceNumber, smsg.Error, false, []byte(err.Error()))
			}
			return err
		} else {
			p.logger.Tracef("Sending %d bytes from local tcp connection to daemon", n)
			p.sendStreamMessage(sequenceNumber, smsg.Stream, true, buf[:n])
			sequenceNumber += 1
		}
	}
}

func (p *Pwdb) sendStreamMessage(sequenceNumber int, streamType smsg.StreamType, more bool, contentBytes []byte) {
	p.streamOutputChan <- smsg.StreamMessage{
		SchemaVersion:  p.streamMessageVersion,
		SequenceNumber: sequenceNumber,
		Action:         string(db.Pwdb),
		Type:           streamType,
		More:           more,
		ContentBytes:   contentBytes,
	}
}
