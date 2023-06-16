package exec

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"gopkg.in/tomb.v2"

	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/plugin"
	"bastionzero.com/bzerolib/plugin/kube/actions/exec"
	kubeutils "bastionzero.com/bzerolib/plugin/kube/utils"
	smsg "bastionzero.com/bzerolib/stream/message"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ExecAction struct {
	tmb    tomb.Tomb
	logger *logger.Logger

	requestId       string
	logId           string
	commandBeingRun string
	doneChan        chan struct{}
	err             error

	// input and output channels relative to this plugin
	outputChan      chan plugin.ActionWrapper
	streamInputChan chan smsg.StreamMessage

	// to prevent us from sending two stop messages to the same pugin on the agent
	stopped     bool
	stoppedLock sync.Mutex
}

func New(
	logger *logger.Logger,
	outputChan chan plugin.ActionWrapper,
	doneChan chan struct{},
	requestId string,
	logId string,
	commandBeingRun string,
) *ExecAction {

	return &ExecAction{
		logger:          logger,
		requestId:       requestId,
		logId:           logId,
		commandBeingRun: commandBeingRun,
		doneChan:        doneChan,
		outputChan:      outputChan,
		streamInputChan: make(chan smsg.StreamMessage, 10),
	}
}

func (e *ExecAction) Kill(err error) {
	if e.tmb.Alive() {
		e.tmb.Kill(err)
		e.tmb.Wait()
	}
}

func (e *ExecAction) Done() <-chan struct{} {
	return e.doneChan
}

func (e *ExecAction) Err() error {
	return e.err
}

func (e *ExecAction) ReceiveMrtap(actionPayload []byte) {}

func (e *ExecAction) ReceiveStream(stream smsg.StreamMessage) {
	e.streamInputChan <- stream
}

func (e *ExecAction) Start(writer http.ResponseWriter, request *http.Request) error {
	// create new SPDY service for exec communication
	subLogger := e.logger.GetComponentLogger("SPDY")
	spdy, err := NewSPDYService(subLogger, writer, request)
	if err != nil {
		e.logger.Error(err)
		return err
	}

	// Determine if this is tty
	isStdIn := kubeutils.IsQueryParamPresent(request, "stdin")
	isTty := kubeutils.IsQueryParamPresent(request, "tty")

	// Now since we made our local connection to kubectl, initiate a connection with Bastion
	e.sendStartMessage(isStdIn, isTty, request.URL.Query()["command"], request.URL.String())

	// Set up a go function for stdout
	e.tmb.Go(func() error {
		defer close(e.doneChan)
		closeChan := spdy.conn.CloseChan()

		for {
			select {
			case <-e.tmb.Dying():
				return nil
			case streamMessage := <-e.streamInputChan:
				// check for end of stream
				contentBytes, _ := base64.StdEncoding.DecodeString(streamMessage.Content)

				// write message to output
				switch streamMessage.Type {
				case smsg.StdOut:
					// For backwards compatibility check for stdout message with
					// EscChar but for newer agents we should always be sending
					// an empty StreamMessage with more = false to indicate the
					// stream ended
					if string(contentBytes) == exec.EscChar || !streamMessage.More {
						e.logger.Info("exec stream ended")
						spdy.conn.Close()
						return nil
					}
					if n, err := spdy.stdoutStream.Write(contentBytes); err != nil {
						e.logger.Errorf("error writing stdout bytes to spdy stream: %s", err)
					} else if n != len(contentBytes) {
						e.logger.Errorf("error writing %d stdout bytes to spdy stream - only wrote %d instead", len(contentBytes), n)
					}
				case smsg.StdErr:
					if n, err := spdy.stderrStream.Write(contentBytes); err != nil {
						e.logger.Errorf("error writing stderr bytes to spdy stream: %s", err)
					} else if n != len(contentBytes) {
						e.logger.Errorf("error writing %d stderr bytes to spdy stream - only wrote %d instead", len(contentBytes), n)
					}
				case smsg.Error:
					errMsg := string(contentBytes)
					spdy.writeStatus(&StatusError{ErrStatus: metav1.Status{
						Status:  metav1.StatusFailure,
						Message: errMsg,
					}})
					spdy.conn.Close()
					return fmt.Errorf("error in kube exec on agent: %s", errMsg)
				default:
					e.logger.Errorf("unrecognized stream type: %s", streamMessage.Type)
				}
			case <-closeChan:
				e.stoppedLock.Lock()
				if !e.stopped {
					// Send message to agent to close the stream
					payload := exec.KubeExecStopActionPayload{
						RequestId: e.requestId,
						LogId:     e.logId,
					}
					e.outbox(exec.ExecStop, payload)
					e.stopped = true
				}
				e.stoppedLock.Unlock()
				return nil
			}
		}
	})

	if isStdIn {
		// Set up a go function to read from stdin
		go func() {
			for {
				// Reset buffer every loop
				buffer := make([]byte, 0)

				// Define our chunkBuffer
				chunkSizeBuffer := make([]byte, kubeutils.ExecChunkSize)

				select {
				case <-e.tmb.Dying():
					return
				default:
					// Keep reading from our stdin stream if we see multiple chunks coming in
					for {
						if n, err := spdy.stdinStream.Read(chunkSizeBuffer); !e.tmb.Alive() {
							return
						} else if err != nil {
							if err == io.EOF {
								e.logger.Infof("finished reading from stdin")
							} else {
								e.logger.Errorf("failed reading from stdin stream: %s", err)
							}

							// Send final stdin message if non-empty
							buffer = append(buffer, chunkSizeBuffer[:n]...)
							if len(buffer) > 0 {
								e.sendStdinMessage(buffer)
							}

							e.stoppedLock.Lock()
							if !e.stopped {
								// Send ExecStop message to close the stdin stream on the agent
								e.outbox(exec.ExecStop, exec.KubeExecStopActionPayload{})
								e.stopped = true
							}
							e.stoppedLock.Unlock()
							return
						} else {
							// Append the new chunk to our buffer
							buffer = append(buffer, chunkSizeBuffer[:n]...)

							// If we stop seeing chunks (i.e. n != 8192) or we have reached our max buffer size, break
							if n != kubeutils.ExecChunkSize || len(buffer) > kubeutils.ExecDefaultMaxBufferSize {
								break
							}
						}
					}
					// Send message to agent
					e.sendStdinMessage(buffer)
				}
			}
		}()
	}

	if isTty {
		// Set up a go function for resize if we are running interactively
		go func() {
			for {
				select {
				case <-e.tmb.Dying():
					return
				default:
					decoder := json.NewDecoder(spdy.resizeStream)

					size := TerminalSize{}
					if err := decoder.Decode(&size); err != nil {
						if err == io.EOF {
							return
						} else {
							e.logger.Error(fmt.Errorf("error decoding resize message: %s", err))
						}
					} else {
						// Emit this as a new resize event
						e.sendResizeMessage(size.Width, size.Height)
					}
				}
			}
		}()
	}

	return nil
}

func (e *ExecAction) outbox(action exec.ExecSubAction, payload interface{}) {
	// Send payload to plugin output queue
	payloadBytes, _ := json.Marshal(payload)
	e.outputChan <- plugin.ActionWrapper{
		Action:        string(action),
		ActionPayload: payloadBytes,
	}
}

func (e *ExecAction) sendStartMessage(isStdIn bool, isTty bool, command []string, endpoint string) {
	payload := exec.KubeExecStartActionPayload{
		RequestId:            e.requestId,
		StreamMessageVersion: smsg.CurrentSchema,
		LogId:                e.logId,
		IsStdIn:              isStdIn,
		IsTty:                isTty,
		Command:              command,
		Endpoint:             endpoint,
		CommandBeingRun:      e.commandBeingRun,
	}
	e.outbox(exec.ExecStart, payload)
}

func (e *ExecAction) sendResizeMessage(width uint16, height uint16) {
	payload := exec.KubeExecResizeActionPayload{
		RequestId: e.requestId,
		LogId:     e.logId,
		Width:     width,
		Height:    height,
	}
	e.outbox(exec.ExecResize, payload)
}

func (e *ExecAction) sendStdinMessage(stdin []byte) {
	payload := exec.KubeStdinActionPayload{
		RequestId: e.requestId,
		LogId:     e.logId,
		Stdin:     stdin,
	}
	e.outbox(exec.ExecInput, payload)
}
