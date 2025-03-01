package dial

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"time"

	"gopkg.in/tomb.v2"

	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/plugin"
	"bastionzero.com/bzerolib/plugin/db/actions/dial"
	smsg "bastionzero.com/bzerolib/stream/message"
)

const (
	chunkSize     = 64 * 1024
	writeDeadline = 5 * time.Second
)

type DialAction struct {
	logger    *logger.Logger
	tmb       tomb.Tomb
	requestId string

	// input and output channels relative to this plugin
	outputChan      chan plugin.ActionWrapper
	streamInputChan chan smsg.StreamMessage

	// done channel for letting the plugin know we're done
	doneChan chan struct{}
	err      error
}

func New(
	logger *logger.Logger,
	requestId string,
	outboxQueue chan plugin.ActionWrapper,
	doneChan chan struct{},
) *DialAction {

	dial := &DialAction{
		logger:    logger,
		requestId: requestId,

		outputChan: outboxQueue,
		// TODO: CWC-2015: reduce this buffer size when we have improved the websocket queue model
		streamInputChan: make(chan smsg.StreamMessage, 256),
		doneChan:        doneChan,
	}

	return dial
}

func (d *DialAction) Start(lconn net.Conn) error {
	// Build and send the action payload to start the tcp connection on the agent
	payload := dial.DialActionPayload{
		RequestId:            d.requestId,
		StreamMessageVersion: smsg.CurrentSchema,
	}
	d.sendOutputMessage(dial.DialStart, payload)

	// Listen to stream messages coming from the agent, and forward to our local connection
	d.tmb.Go(func() error {
		defer lconn.Close()

		d.tmb.Go(func() error {
			defer close(d.doneChan)

			// listen to messages coming from the local tcp connection and sends them to the agent
			buf := make([]byte, chunkSize)
			sequenceNumber := 0

			for {
				if n, err := lconn.Read(buf); !d.tmb.Alive() {
					return nil
				} else if err != nil {
					// print our error message
					if err == io.EOF {
						d.logger.Info("local tcp connection has been closed")
					} else {
						d.logger.Errorf("error reading from local tcp connection: %s", err)
					}

					// let the agent know we need to stop
					payload := dial.DialActionPayload{
						RequestId: d.requestId,
					}
					d.sendOutputMessage(dial.DialStop, payload)

					return nil
				} else if n > 0 {
					// Build and send whatever we get from the local tcp connection to the agent
					dataToSend := base64.StdEncoding.EncodeToString(buf[:n])
					payload := dial.DialInputActionPayload{
						RequestId:      d.requestId,
						SequenceNumber: sequenceNumber,
						Data:           dataToSend,
					}
					d.sendOutputMessage(dial.DialInput, payload)

					sequenceNumber += 1
				}
			}
		})

		// variables for ensuring we receive stream messages in order
		expectedSequenceNumber := 0
		streamMessages := make(map[int]smsg.StreamMessage)

		for {
			select {
			case <-d.tmb.Dying():
				return nil
			case data := <-d.streamInputChan:
				if !d.tmb.Alive() {
					return nil
				}
				streamMessages[data.SequenceNumber] = data

				// process the incoming stream messages *in order*
				for streamMessage, ok := streamMessages[expectedSequenceNumber]; ok; streamMessage, ok = streamMessages[expectedSequenceNumber] {
					// if we got an old-fashioned end message or a newfangled one
					if streamMessage.Type == smsg.DbStreamEnd {
						// since there's no more stream coming, close the local connection
						d.logger.Errorf("remote tcp connection has been closed, closing local tcp connection")
						return nil

						// again, might have gotten an old or new message depending on what we asked for
					} else if streamMessage.Type == smsg.DbStream || streamMessage.Type == smsg.Stream {
						if contentBytes, err := base64.StdEncoding.DecodeString(streamMessage.Content); err != nil {
							d.logger.Errorf("could not decode db stream content: %s", err)
						} else {
							// Set a deadline for the write so we don't block forever
							lconn.SetWriteDeadline(time.Now().Add(writeDeadline))
							if _, err := lconn.Write(contentBytes); err != nil && d.tmb.Alive() {
								d.logger.Errorf("error writing to local TCP connection: %s", err)
								d.tmb.Kill(nil)
							}
						}

						if !streamMessage.More {
							d.logger.Errorf("remote tcp connection has been closed, closing local tcp connection")
							return nil
						}
					} else if streamMessage.Type == smsg.Error {
						if contentBytes, err := base64.StdEncoding.DecodeString(streamMessage.Content); err != nil {
							d.logger.Errorf("could not decode db stream content: %s", err)
						} else {
							d.logger.Infof("agent hit an error trying to read from remote connection: %s", string(contentBytes))
						}
					} else {
						d.logger.Debugf("unhandled stream type: %s", streamMessage.Type)
					}

					// remove the message we've already processed
					delete(streamMessages, expectedSequenceNumber)

					// increment our sequence number
					expectedSequenceNumber += 1
				}
			}
		}
	})
	return nil
}

func (d *DialAction) Done() <-chan struct{} {
	return d.doneChan
}

func (d *DialAction) Err() error {
	return d.err
}

func (d *DialAction) Kill(err error) {
	if d.tmb.Alive() {
		d.tmb.Kill(err) // kills all datachannel, plugin, and action goroutines
		d.tmb.Wait()
	}
}

func (d *DialAction) sendOutputMessage(action dial.DialSubAction, payload interface{}) {
	// Send payload to plugin output queue
	payloadBytes, _ := json.Marshal(payload)
	d.outputChan <- plugin.ActionWrapper{
		Action:        string(action),
		ActionPayload: payloadBytes,
	}
}

func (d *DialAction) ReceiveStream(smessage smsg.StreamMessage) {
	d.logger.Debugf("Dial action received %v stream, message count: %d", smessage.Type, len(d.streamInputChan)+1)
	d.streamInputChan <- smessage
}

func (d *DialAction) ReceiveMrtap(action string, actionPayload []byte) error {
	// the only MrTAP message that we would receive is the ack from the agent after stopping the dial action
	// we don't do anything with it on the daemon side, so we receive it here and it will get logged
	// but no particular action will be taken
	return nil
}
