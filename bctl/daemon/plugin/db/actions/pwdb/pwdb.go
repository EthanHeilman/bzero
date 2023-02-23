package pwdb

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"gopkg.in/tomb.v2"

	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/plugin"
	"bastionzero.com/bctl/v1/bzerolib/plugin/db/actions/pwdb"
	smsg "bastionzero.com/bctl/v1/bzerolib/stream/message"
)

const (
	chunkSize     = 128 * 1024 // 128 kB
	writeDeadline = 5 * time.Second
)

type Pwdb struct {
	logger *logger.Logger
	tmb    tomb.Tomb

	targetUser string
	targetId   string

	// input and output channels relative to this plugin
	outputChan      chan plugin.ActionWrapper
	streamInputChan chan smsg.StreamMessage
	mrtapInputChan  chan plugin.ActionWrapper

	// done channel for letting the plugin know we're done
	doneChan chan struct{}
}

func New(
	logger *logger.Logger,
	targetUser string,
	targetId string,
	outboxQueue chan plugin.ActionWrapper,
	doneChan chan struct{},
) *Pwdb {

	return &Pwdb{
		logger:     logger,
		targetUser: targetUser,
		targetId:   targetId,
		outputChan: outboxQueue,
		// TODO: CWC-2015: reduce this buffer size when we have improved the websocket queue model
		streamInputChan: make(chan smsg.StreamMessage, 256),
		mrtapInputChan:  make(chan plugin.ActionWrapper, 100),
		doneChan:        doneChan,
	}
}

func (p *Pwdb) Start(lconn net.Conn) error {
	p.logger.Infof("Establishing SplitCert connection")
	// Send message to agent so that we can test the connection
	payload := pwdb.ConnectPayload{
		TargetUser:           p.targetUser,
		TargetId:             p.targetId,
		StreamMessageVersion: smsg.CurrentSchema,
	}
	p.sendOutputMessage(pwdb.Connect, payload)

	// Wait for a message to come in on the stream message channel
	select {
	case msg := <-p.mrtapInputChan:
		if msg.Action == string(pwdb.Connect) {
			p.logger.Infof("Successfully connected")
		} else {
			return fmt.Errorf("MrTAP message did not correlate with the expected action taken")
		}
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timed out waiting for a successful connection attempt")
	case <-p.tmb.Dying():
		return p.tmb.Err()
	}

	go func() {
		for {
			select {
			case <-p.tmb.Dying():
				return
			case <-p.mrtapInputChan:
				// receive mrtap messages, however we don't have anything to do with them so this statement prevents
				// the chan from filling up and blocking
			}
		}
	}()

	p.tmb.Go(func() error {
		p.tmb.Go(func() error {
			return p.readFromConnection(lconn)
		})

		return p.writeToConnection(lconn)
	})

	return nil
}

func (p *Pwdb) readFromConnection(lconn net.Conn) error {
	defer close(p.doneChan)
	p.logger.Info("Listening on local connection")

	// listen to messages coming from the local tcp connection and sends them to the agent
	buf := make([]byte, chunkSize)

	for {
		if n, err := lconn.Read(buf); !p.tmb.Alive() {
			return nil
		} else if err != nil {
			if err == io.EOF {
				p.logger.Info("local tcp connection has been closed")
			} else {
				p.logger.Errorf("error reading from local tcp connection: %s", err)
			}

			// close the connection at the agent
			p.sendOutputMessage(pwdb.Close, pwdb.ClosePayload{
				Reason: err.Error(),
			})
			return err
		} else if n > 0 {
			payload := pwdb.InputPayload{
				Data: base64.StdEncoding.EncodeToString(buf[:n]),
			}
			p.sendOutputMessage(pwdb.Input, payload)
		}
	}
}

func (p *Pwdb) writeToConnection(lconn net.Conn) error {
	// this will make sure we stop reading when we're done writing
	defer lconn.Close()

	for {
		select {
		case <-p.tmb.Dying():
			return nil
		case msg := <-p.streamInputChan:
			// You may think this is irrelevant and can't we just remove it? NO. Perhaps because of the encoding
			// perhaps because of mystical forces beyond our control, the encoding this goes through causes some
			// kind of data loss or manipulation and breaks dbeaver.
			content, err := base64.StdEncoding.DecodeString(msg.Content)
			if err != nil {
				p.logger.Errorf("failed to decode stream message content: %s", err)
				return err
			}

			switch msg.Type {
			case smsg.Stream:
				// Set a deadline for the write so we don't block forever
				lconn.SetWriteDeadline(time.Now().Add(writeDeadline))
				if _, err := lconn.Write(content); err != nil {
					p.logger.Errorf("error writing to local TCP connection: %s", err)
					return err
				}

				// if the stream is done, we close
				if !msg.More {
					p.logger.Errorf("remote tcp connection has been closed, closing local tcp connection")
					return fmt.Errorf("stream end")
				}
			case smsg.Error:
				p.logger.Infof("agent hit an error trying to read from remote connection: %s", string(content))
				return fmt.Errorf(string(content))
			default:
				p.logger.Errorf("unhandled stream type: %s", msg.Type)
			}
		}
	}
}

func (p *Pwdb) Done() <-chan struct{} {
	return p.doneChan
}

func (p *Pwdb) Err() error {
	return p.tmb.Err()
}

func (p *Pwdb) Kill(err error) {
	if p.tmb.Alive() {
		p.tmb.Kill(err) // kills all datachannel, plugin, and action goroutines
		p.tmb.Wait()
	}
}

func (p *Pwdb) sendOutputMessage(action pwdb.PwdbSubAction, payload interface{}) {
	// Send payload to plugin output queue
	payloadBytes, _ := json.Marshal(payload)
	p.outputChan <- plugin.ActionWrapper{
		Action:        string(action),
		ActionPayload: payloadBytes,
	}
}

func (p *Pwdb) ReceiveStream(smessage smsg.StreamMessage) {
	p.logger.Infof("Received %s stream, message count: %d", smessage.Type, len(p.streamInputChan)+1)
	p.streamInputChan <- smessage
}

func (p *Pwdb) ReceiveMrtap(action string, actionPayload []byte) error {
	p.mrtapInputChan <- plugin.ActionWrapper{
		Action:        action,
		ActionPayload: actionPayload,
	}
	return nil
}
