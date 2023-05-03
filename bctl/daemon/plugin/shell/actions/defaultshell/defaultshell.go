package defaultshell

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"gopkg.in/tomb.v2"

	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/plugin"
	bzshell "bastionzero.com/bzerolib/plugin/shell"
	smsg "bastionzero.com/bzerolib/stream/message"
)

const (
	inputBufferSize   = 8 * 1024
	inputDebounceTime = 5 * time.Millisecond
)

type DefaultShell struct {
	tmb    tomb.Tomb
	logger *logger.Logger

	outputChan chan plugin.ActionWrapper // plugin's output queue
	doneChan   chan struct{}

	// channel where we push each individual keypress byte from StdIn
	stdInChan chan byte

	isConnected bool
}

func New(logger *logger.Logger, outboxQueue chan plugin.ActionWrapper, doneChan chan struct{}) *DefaultShell {
	return &DefaultShell{
		logger:     logger,
		outputChan: outboxQueue,
		doneChan:   doneChan,
		stdInChan:  make(chan byte, inputBufferSize),
	}
}

func (d *DefaultShell) Done() <-chan struct{} {
	return d.doneChan
}

func (d *DefaultShell) Err() error {
	return d.tmb.Err()
}

func (d *DefaultShell) Kill(err error) {
	d.tmb.Kill(err)
}

func (d *DefaultShell) Start(attach bool) error {
	if attach {
		// If we are attaching send a shell replay message to replay terminal
		// output
		shellReplayDataMessage := bzshell.ShellReplayMessage{}
		d.sendOutputMessage(bzshell.ShellReplay, shellReplayDataMessage)
	} else {
		// If we are not attaching then send a ShellOpen data message to start
		// the pty on the target
		openShellDataMessage := bzshell.ShellOpenMessage{
			StreamMessageVersion: smsg.CurrentSchema,
		}
		d.sendOutputMessage(bzshell.ShellOpen, openShellDataMessage)
	}
	go func() {
		defer close(d.doneChan)
		<-d.tmb.Dying()
	}()

	// Os-specific specialized setup
	return d.start(attach)
}

func (d *DefaultShell) Replay(replayData []byte) error {
	d.logger.Debug("Default shell received replay message with action")
	if _, err := os.Stdout.Write(replayData); err != nil {
		d.logger.Errorf("Error writing shell replay message to Stdout: %s", err)
		return err
	}

	return nil
}

func (d *DefaultShell) ReceiveStream(smessage smsg.StreamMessage) {
	d.logger.Debugf("Default shell received %v stream", smessage.Type)
	d.isConnected = true

	switch smsg.StreamType(smessage.Type) {
	case smsg.StdOut:
		if contentBytes, err := base64.StdEncoding.DecodeString(smessage.Content); err != nil {
			d.logger.Errorf("Error decoding ShellStdOut stream content: %s", err)
		} else {
			if _, err = os.Stdout.Write(contentBytes); err != nil {
				d.logger.Errorf("Error writing to Stdout: %s", err)
			}
		}
	case smsg.Stop:
		d.tmb.Kill(&bzshell.ShellQuitError{})
		return
	default:
		d.logger.Errorf("unhandled stream type: %s", smessage.Type)
	}
}

func (d *DefaultShell) readFromStdIn() error {
	b := make([]byte, 1)

	for {
		select {
		case <-d.tmb.Dying():
			return nil
		default:
			if n, err := os.Stdin.Read(b); !d.tmb.Alive() {
				return nil
			} else if err != nil || n != 1 {
				return fmt.Errorf("error reading last keypress from Stdin: %w", err)
			}

			if !d.isConnected {
				switch b[0] {
				// this appears to be cross-platform between Linux and MacOS
				// NOTE: there is a brief period when the user could press ctrl+\ right
				// 		when the zli is starting up that can put them in a weird state.
				//		This is not something we can catch right now but the user can
				//		press ctrl+d to get out of it
				case uint8(3), uint8(4), uint8(28):
					return &bzshell.ShellCancelledError{}
				default:
				}
			}

			d.stdInChan <- b[0]
		}
	}
}

// processes input channel by debouncing all keypresses within a time interval
func (d *DefaultShell) sendStdIn() {
	inputBuf := make([]byte, inputBufferSize)

	// slice the inputBuf to len 0 (but still keep capacity allocated)
	inputBuf = inputBuf[:0]

	for {
		select {
		case <-d.tmb.Dying():
			return
		case b := <-d.stdInChan:
			inputBuf = append(inputBuf, b)
		case <-time.After(inputDebounceTime):
			if len(inputBuf) >= 1 {
				// Send all accumulated keypresses in a shellInput data message
				shellInputDataMessage := bzshell.ShellInputMessage{
					Data: inputBuf,
				}
				d.sendOutputMessage(bzshell.ShellInput, shellInputDataMessage)

				// clear the input buffer by slicing it to size 0 which will still
				// keep memory allocated for the underlying capacity of the slice
				inputBuf = inputBuf[:0]
			}
		}
	}
}

func (d *DefaultShell) sendOutputMessage(action bzshell.ShellSubAction, payload interface{}) {
	// Send payload to plugin output queue
	payloadBytes, _ := json.Marshal(payload)
	d.outputChan <- plugin.ActionWrapper{
		Action:        string(action),
		ActionPayload: payloadBytes,
	}
}
