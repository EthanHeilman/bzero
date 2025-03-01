package webwebsocket

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"bastionzero.com/bzerolib/bzhttp"
	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/plugin"
	"bastionzero.com/bzerolib/plugin/web/actions/webwebsocket"
	smsg "bastionzero.com/bzerolib/stream/message"
	"github.com/gorilla/websocket"

	"gopkg.in/tomb.v2"
)

type WebWebsocketAction struct {
	tmb       tomb.Tomb
	logger    *logger.Logger
	requestId string

	// input and output channels relative to this plugin
	outputChan      chan plugin.ActionWrapper
	streamInputChan chan smsg.StreamMessage

	// plugin done channel for signalling to the datachannel we're done
	doneChan chan struct{}
	err      error

	sequenceNumber int
}

func New(logger *logger.Logger, requestId string, outputChan chan plugin.ActionWrapper, doneChan chan struct{}) *WebWebsocketAction {
	return &WebWebsocketAction{
		logger:          logger,
		requestId:       requestId,
		outputChan:      outputChan,
		streamInputChan: make(chan smsg.StreamMessage, 10),
		doneChan:        doneChan,
		sequenceNumber:  0,
	}
}

func (w *WebWebsocketAction) Start(writer http.ResponseWriter, request *http.Request) error {
	// this action ends at the end of this function, in order to signal that to the parent plugin,
	// we close the output channel which will close the go routine listening on it
	defer close(w.doneChan)

	// First extract the headers out of the request
	headers := bzhttp.GetHeaders(request.Header)

	// Let the agent know to open up a websocket
	payload := webwebsocket.WebWebsocketStartActionPayload{
		RequestId:            w.requestId,
		StreamMessageVersion: smsg.CurrentSchema,
		Headers:              headers,
		Endpoint:             request.URL.String(),
		Method:               request.Method,
	}
	w.outbox(webwebsocket.Start, payload)

	return w.handleWebsocketRequest(writer, request)
}

func (w *WebWebsocketAction) handleWebsocketRequest(writer http.ResponseWriter, request *http.Request) error {
	// Upgrade the connection
	var upgrader = websocket.Upgrader{}
	conn, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		w.logger.Errorf("upgrade failed: %s", err)
		return err
	}

	// Setup a go routine to stream data from the agent back to daemon
	w.tmb.Go(func() error {
		defer conn.Close()
		defer close(w.doneChan)

		for {
			select {
			case <-w.tmb.Dead():
				return nil
			case incomingMessage := <-w.streamInputChan:
				switch incomingMessage.Type {
				// may be getting an old-fashioned or newfangled message, depending on what we asked for
				case smsg.DataOut, smsg.Data:
					if err := w.writeOutData(conn, incomingMessage.Content); err != nil {
						w.logger.Error(err)
						return err
					}
				case smsg.AgentStop, smsg.Stop:
					// End the local connection
					w.logger.Infof("Received close message from agent, closing websocket")
					return nil
				default:
					w.logger.Errorf("unhandled stream type: %s", incomingMessage.Type)
				}
			}
		}
	})

	// Continuosly read
	for {
		if mt, message, err := conn.ReadMessage(); !w.tmb.Alive() {
			return nil
		} else if err != nil {
			w.logger.Errorf("web websocket connection read failed: %s", err)

			// Let the agent know to close the websocket
			payload := webwebsocket.WebWebsocketDaemonStopActionPayload{
				RequestId: w.requestId,
			}
			w.outbox(webwebsocket.DaemonStop, payload)
			return fmt.Errorf("failed to read from connection: %s", err)
		} else {
			// Convert the message to a string
			messageBase64 := base64.StdEncoding.EncodeToString(message)

			// Send payload to plugin output queue
			payload := webwebsocket.WebWebsocketDataInActionPayload{
				RequestId:   w.requestId,
				Message:     messageBase64,
				MessageType: mt,
			}
			w.outbox(webwebsocket.DataIn, payload)
		}
	}
}

func (w *WebWebsocketAction) Kill(err error) {
	if w.tmb.Alive() {
		w.tmb.Kill(err)
		w.tmb.Wait()
	}
}

func (w *WebWebsocketAction) Done() <-chan struct{} {
	return w.doneChan
}

func (w *WebWebsocketAction) Err() error {
	return w.err
}

func (w *WebWebsocketAction) ReceiveStream(smessage smsg.StreamMessage) {
	w.logger.Debugf("web websocket action received %v stream", smessage.Type)
	w.streamInputChan <- smessage
}

func (w *WebWebsocketAction) writeOutData(conn *websocket.Conn, content string) error {
	// Stream data to the local connection
	// Undo the base 64 encoding
	incomingContent, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return fmt.Errorf("error decoding stream message: %v", err)
	}

	// Unmarshell the stream message
	var streamDataOut webwebsocket.WebWebsocketStreamDataOut
	if err := json.Unmarshal(incomingContent, &streamDataOut); err != nil {
		return fmt.Errorf("error unmarshalling stream message: %s", err)
	}

	// Unmarshel the websocket message
	websocketMessage, err := base64.StdEncoding.DecodeString(streamDataOut.Message)
	if err != nil {
		return fmt.Errorf("error decoding stream message: %v", err)
	}

	// Send the message to the user!
	if err := conn.WriteMessage(streamDataOut.MessageType, websocketMessage); err != nil {
		w.logger.Errorf("error writing to websocket: %s", err)
	}
	return nil
}

func (w *WebWebsocketAction) outbox(action webwebsocket.WebWebsocketSubAction, payload interface{}) {
	// Send payload to plugin output queue
	payloadBytes, _ := json.Marshal(payload)
	w.outputChan <- plugin.ActionWrapper{
		Action:        string(action),
		ActionPayload: payloadBytes,
	}
}
