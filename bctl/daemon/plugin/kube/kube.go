package kube

import (
	"fmt"
	"net/http"

	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/plugin"
	bzkube "bastionzero.com/bzerolib/plugin/kube"
	smsg "bastionzero.com/bzerolib/stream/message"
	"bastionzero.com/daemon/plugin/kube/actions/exec"
	"bastionzero.com/daemon/plugin/kube/actions/portforward"
	"bastionzero.com/daemon/plugin/kube/actions/restapi"
	"bastionzero.com/daemon/plugin/kube/actions/stream"
	"github.com/google/uuid"
)

type IKubeDaemonAction interface {
	ReceiveMrtap(actionPayload []byte)
	ReceiveStream(stream smsg.StreamMessage)
	Start(writer http.ResponseWriter, request *http.Request) error
	Done() <-chan struct{}
	Err() error
	Kill(err error)
}
type KubeDaemonPlugin struct {
	logger *logger.Logger

	action   IKubeDaemonAction
	doneChan chan struct{}
	killed   bool

	// outbox channel
	outboxQueue chan plugin.ActionWrapper

	// Kube-specific vars
	targetUser   string
	targetGroups []string
}

func New(logger *logger.Logger, targetUser string, targetGroups []string) *KubeDaemonPlugin {
	return &KubeDaemonPlugin{
		logger:       logger,
		doneChan:     make(chan struct{}),
		killed:       false,
		outboxQueue:  make(chan plugin.ActionWrapper, 25),
		targetUser:   targetUser,
		targetGroups: targetGroups,
	}
}

func (k *KubeDaemonPlugin) Kill(err error) {
	k.killed = true
	if k.action != nil {
		k.action.Kill(err)
	}
}

func (k *KubeDaemonPlugin) Done() <-chan struct{} {
	return k.doneChan
}

func (k *KubeDaemonPlugin) Err() error {
	return k.action.Err()
}

func (k *KubeDaemonPlugin) Outbox() <-chan plugin.ActionWrapper {
	return k.outboxQueue
}

func (k *KubeDaemonPlugin) ReceiveStream(smessage smsg.StreamMessage) {
	if k.action != nil {
		k.action.ReceiveStream(smessage)
	} else {
		k.logger.Debugf("Kube plugin received a stream message before an action was created. Ignoring")
	}
}

func (k *KubeDaemonPlugin) StartAction(action bzkube.KubeAction, logId string, command string, writer http.ResponseWriter, reader *http.Request) error {
	if k.killed {
		return fmt.Errorf("plugin has already been killed, cannot create a new kube action")
	}
	// Always generate a requestId, each new kube command is its own request
	// TODO: deprecated
	requestId := uuid.New().String()

	// Create action logger
	actLogger := k.logger.GetActionLogger(string(action))
	actLogger.AddRequestId(requestId)

	switch action {
	case bzkube.Exec:
		k.action = exec.New(actLogger, k.outboxQueue, k.doneChan, requestId, logId, command)
	case bzkube.Stream:
		k.action = stream.New(actLogger, k.outboxQueue, k.doneChan, requestId, logId, command)
	case bzkube.RestApi:
		k.action = restapi.New(actLogger, k.outboxQueue, k.doneChan, requestId, logId, command)
	case bzkube.PortForward:
		k.action = portforward.New(actLogger, k.outboxQueue, k.doneChan, requestId, logId, command)
	default:
		rerr := fmt.Errorf("unrecognized kubectl action: %s", action)
		k.logger.Error(rerr)
		return rerr
	}

	k.logger.Infof("Created %s action with url: %s", action, reader.URL.Path)

	// send http handlers to action
	if err := k.action.Start(writer, reader); err != nil {
		k.logger.Error(fmt.Errorf("%s error: %s", action, err))
	}
	return nil
}

func (k *KubeDaemonPlugin) ReceiveMrtap(action string, actionPayload []byte) error {
	// if actionPayload is empty, then there's nothing we need to process
	if len(actionPayload) == 0 {
		return nil
	} else if k.action == nil {
		return fmt.Errorf("received MrTAP message before action was created")
	}

	k.action.ReceiveMrtap(actionPayload)
	return nil
}
