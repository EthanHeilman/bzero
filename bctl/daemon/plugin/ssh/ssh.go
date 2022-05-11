package ssh

import (
	"fmt"

	"bastionzero.com/bctl/v1/bctl/daemon/plugin/ssh/actions/defaultssh"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	bzplugin "bastionzero.com/bctl/v1/bzerolib/plugin"
	bzssh "bastionzero.com/bctl/v1/bzerolib/plugin/ssh"
	smsg "bastionzero.com/bctl/v1/bzerolib/stream/message"
)

// Perhaps unnecessary but it is nice to make sure that each action is implementing a common function set
type ISshAction interface {
	ReceiveStream(stream smsg.StreamMessage)
	Start() error
	Kill()
}

type SshDaemonPlugin struct {
	logger       *logger.Logger
	outboxQueue  chan bzplugin.ActionWrapper
	doneChan     chan struct{}
	killed       bool
	action       ISshAction
	identityFile string
}

func New(logger *logger.Logger, identityFile string) *SshDaemonPlugin {
	return &SshDaemonPlugin{
		logger:       logger,
		outboxQueue:  make(chan bzplugin.ActionWrapper, 10),
		doneChan:     make(chan struct{}),
		killed:       false,
		identityFile: identityFile,
	}
}

func (s *SshDaemonPlugin) StartAction() error {
	if s.killed {
		return fmt.Errorf("plugin has already been killed, cannot create a new ssh action")
	}

	// Create the DefaultSsh action
	actLogger := s.logger.GetActionLogger(string(bzssh.DefaultSsh))
	s.action = defaultssh.New(actLogger, s.outboxQueue, s.doneChan, s.identityFile)

	// Start the ssh action
	if err := s.action.Start(); err != nil {
		return fmt.Errorf("error starting the default ssh action: %s", err)
	} else {
		return nil
	}
}

func (s *SshDaemonPlugin) Kill() {
	s.killed = true
	if s.action != nil {
		s.action.Kill()
	}
}

func (s *SshDaemonPlugin) Done() <-chan struct{} {
	return s.doneChan
}

func (s *SshDaemonPlugin) Outbox() <-chan bzplugin.ActionWrapper {
	return s.outboxQueue
}

func (s *SshDaemonPlugin) ReceiveStream(smessage smsg.StreamMessage) {
	s.logger.Debugf("ssh plugin received %v stream", smessage.Type)
	if s.action != nil {
		s.action.ReceiveStream(smessage)
	} else {
		s.logger.Debug("ssh plugin received stream message before an action was created. Ignoring")
	}
}

func (s *SshDaemonPlugin) ReceiveKeysplitting(action string, actionPayload []byte) error {
	return nil
}
