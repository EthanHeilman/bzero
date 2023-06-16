package exec

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/plugin/kube"
	bzexec "bastionzero.com/bzerolib/plugin/kube/actions/exec"
	kubeutils "bastionzero.com/bzerolib/plugin/kube/utils"
	smsg "bastionzero.com/bzerolib/stream/message"
)

// wrap this code so at test time we can inject a mock executor / config
var getExecutor = func(config *rest.Config, method string, url *url.URL) (remotecommand.Executor, error) {
	return remotecommand.NewSPDYExecutor(config, method, url)
}

var getConfig = func() (*rest.Config, error) {
	return rest.InClusterConfig()
}

type ExecAction struct {
	logger *logger.Logger

	doneChan chan struct{}

	// output channel to send all of our stream messages directly to datachannel
	streamOutputChan     chan smsg.StreamMessage
	streamMessageVersion smsg.SchemaVersion

	// To send input/resize to our exec sessions
	execStdinChannel  chan []byte
	execResizeChannel chan bzexec.KubeExecResizeActionPayload

	// we hold onto this so we can close appropriately
	stdinReader *StdReader

	serviceAccountToken string
	kubeHost            string
	targetGroups        []string
	targetUser          string
	logId               string
	requestId           string

	// to prevent us from responding to two stop messages in the same plugin
	stopped bool
}

func New(
	logger *logger.Logger,
	ch chan smsg.StreamMessage,
	doneChan chan struct{},
	serviceAccountToken string,
	kubeHost string,
	targetGroups []string,
	targetUser string,
) *ExecAction {

	return &ExecAction{
		logger:              logger,
		doneChan:            doneChan,
		streamOutputChan:    ch,
		execStdinChannel:    make(chan []byte, 10),
		execResizeChannel:   make(chan bzexec.KubeExecResizeActionPayload, 10),
		serviceAccountToken: serviceAccountToken,
		kubeHost:            kubeHost,
		targetGroups:        targetGroups,
		targetUser:          targetUser,
	}
}

func (e *ExecAction) Kill() {
	// If the datachannel is closed and this kill function is called, it doesn't necessarily mean
	// that the exec was properly closed, and because the below exec.Stream only returns when it's done, there's
	// no way to interrupt it or pass in a context. Therefore, we need to close the stream in order to pass an
	// io.EOF message to exec which will close the exec.Stream and that will close the go routine.
	// ref: https://github.com/kubernetes/client-go/issues/554
	if e.stdinReader != nil {
		e.stdinReader.Close()
		<-e.doneChan
	}
}

func (e *ExecAction) Receive(action string, actionPayload []byte) ([]byte, error) {
	switch bzexec.ExecSubAction(action) {

	// Start exec message required before anything else
	case bzexec.ExecStart:
		var startExecRequest bzexec.KubeExecStartActionPayload
		if err := json.Unmarshal(actionPayload, &startExecRequest); err != nil {
			rerr := fmt.Errorf("unable to unmarshal start exec message: %s", err)
			e.logger.Error(rerr)
			return []byte{}, rerr
		}

		return e.startExec(startExecRequest)

	case bzexec.ExecInput:
		var execInputAction bzexec.KubeStdinActionPayload
		if err := json.Unmarshal(actionPayload, &execInputAction); err != nil {
			rerr := fmt.Errorf("error unmarshaling stdin: %s", err)
			e.logger.Error(rerr)
			return []byte{}, rerr
		}

		// Always feed in the exec stdin a chunk at a time (i.e. break up the byte array into chunks)
		for i := 0; i < len(execInputAction.Stdin); i += kubeutils.ExecChunkSize {
			end := i + kubeutils.ExecChunkSize
			if end > len(execInputAction.Stdin) {
				end = len(execInputAction.Stdin)
			}
			// we must not write to this channel if it is closed
			if !e.stopped {
				e.execStdinChannel <- execInputAction.Stdin[i:end]
			}
		}
		return []byte{}, nil

	case bzexec.ExecResize:
		var execResizeAction bzexec.KubeExecResizeActionPayload
		if err := json.Unmarshal(actionPayload, &execResizeAction); err != nil {
			rerr := fmt.Errorf("error unmarshaling resize message: %s", err)
			e.logger.Error(rerr)
			return []byte{}, rerr
		}

		e.execResizeChannel <- execResizeAction
		return []byte{}, nil
	case bzexec.ExecStop:
		var execStopAction bzexec.KubeExecStopActionPayload
		if err := json.Unmarshal(actionPayload, &execStopAction); err != nil {
			e.logger.Errorf("error unmarshaling stop message: %s", err)
		}

		// close the execStdinChannel which will end the the stream after it
		// finishes reading all data in the execStdinChannel. This is in effect a "soft" close
		// in contrast to the e.stdinReader.Close() above, which exits without waiting
		// for all input to be processed
		if !e.stopped {
			close(e.execStdinChannel)
			e.stopped = true
		}
		return []byte{}, nil
	default:
		rerr := fmt.Errorf("unhandled exec action: %v", action)
		e.logger.Error(rerr)
		return []byte{}, rerr
	}
}

func (e *ExecAction) startExec(startExecRequest bzexec.KubeExecStartActionPayload) ([]byte, error) {
	e.logger.Infof("executing kube exec cmd: %s. command: %s. isTty: %t. isStdIn: %t", startExecRequest.CommandBeingRun, startExecRequest.Command, startExecRequest.IsTty, startExecRequest.IsStdIn)
	// keep track of who we're talking to
	e.requestId = startExecRequest.RequestId
	e.logger.Infof("Setting request id: %s", e.requestId)
	e.logId = startExecRequest.LogId
	e.streamMessageVersion = startExecRequest.StreamMessageVersion
	e.logger.Infof("Setting stream message version: %s", e.streamMessageVersion)

	// Now open up our local exec session
	// Create the in-cluster config
	config, err := getConfig()
	if err != nil {
		rerr := fmt.Errorf("error creating in-custer config: %s", err)
		e.logger.Error(rerr)
		return []byte{}, rerr
	}

	// Always ensure that our targetUser is set
	if e.targetUser == "" {
		rerr := fmt.Errorf("target user field is not set")
		e.logger.Error(rerr)
		return []byte{}, rerr
	}

	// Add our impersonation information
	config.Impersonate = rest.ImpersonationConfig{
		UserName: e.targetUser,
		Groups:   e.targetGroups,
	}
	config.BearerToken = e.serviceAccountToken

	kubeExecApiUrl := e.kubeHost + startExecRequest.Endpoint
	kubeExecApiUrlParsed, err := url.Parse(kubeExecApiUrl)
	if err != nil {
		rerr := fmt.Errorf("could not parse kube exec url: %s", err)
		e.logger.Error(rerr)
		return []byte{}, rerr
	}

	// Turn it into a SPDY executor
	exec, err := getExecutor(config, "POST", kubeExecApiUrlParsed)
	if err != nil {
		return []byte{}, fmt.Errorf("error creating Spdy executor: %s", err)
	}

	// NOTE: don't need to version this because Type is not read on the other end
	stderrWriter := NewStdWriter(e.streamOutputChan, e.streamMessageVersion, e.requestId, string(kube.Exec), smsg.StdErr, e.logId)
	stdoutWriter := NewStdWriter(e.streamOutputChan, e.streamMessageVersion, e.requestId, string(kube.Exec), smsg.StdOut, e.logId)
	terminalSizeQueue := NewTerminalSizeQueue(startExecRequest.RequestId, e.execResizeChannel)

	// runs the exec interaction with the kube server
	go func() {
		defer close(e.doneChan)

		if startExecRequest.IsStdIn {
			e.stdinReader = NewStdReader(string(bzexec.StdIn), startExecRequest.RequestId, e.execStdinChannel)

			if startExecRequest.IsTty {
				err = exec.Stream(remotecommand.StreamOptions{
					Stdin:             e.stdinReader,
					Stdout:            stdoutWriter,
					Stderr:            stderrWriter,
					TerminalSizeQueue: terminalSizeQueue,
					Tty:               true,
				})
			} else {
				err = exec.Stream(remotecommand.StreamOptions{
					Stdin:  e.stdinReader,
					Stdout: stdoutWriter,
					Stderr: stderrWriter,
				})
			}
		} else {
			err = exec.Stream(remotecommand.StreamOptions{
				Stdout: stdoutWriter,
				Stderr: stderrWriter,
			})
		}

		if err != nil {
			rerr := fmt.Errorf("error in SPDY stream: %s", err)
			e.logger.Error(rerr)
			e.sendStreamMessage(0, smsg.Error, false, []byte(rerr.Error()))
		}

		// Now close the stream by sending an empty stdout stream message with more=false
		e.sendStreamMessage(stdoutWriter.SequenceNumber, smsg.StdOut, false, []byte{})
	}()

	return []byte{}, nil
}

func (e *ExecAction) sendStreamMessage(sequenceNumber int, streamType smsg.StreamType, more bool, contentBytes []byte) {
	e.streamOutputChan <- smsg.StreamMessage{
		SchemaVersion:  e.streamMessageVersion,
		SequenceNumber: sequenceNumber,
		Action:         string(kube.Exec),
		Type:           streamType,
		More:           more,
		Content:        base64.StdEncoding.EncodeToString(contentBytes),
	}
}
