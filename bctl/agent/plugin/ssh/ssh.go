package ssh

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"bastionzero.com/agent/plugin/ssh/actions/opaquessh"
	"bastionzero.com/agent/plugin/ssh/actions/transparentssh"
	"bastionzero.com/agent/plugin/ssh/authorizedkeys"
	"bastionzero.com/bzerolib/bzio"
	"bastionzero.com/bzerolib/logger"
	bzssh "bastionzero.com/bzerolib/plugin/ssh"
	smsg "bastionzero.com/bzerolib/stream/message"
	"bastionzero.com/bzerolib/unix/unixuser"
)

const (
	sshDir         = ".ssh"
	maxKeyLifetime = 30 * time.Second
)

type ISshAction interface {
	Receive(action string, actionPayload []byte) ([]byte, error)
	Kill()
}

type SshPlugin struct {
	logger *logger.Logger

	action           ISshAction
	streamOutputChan chan smsg.StreamMessage

	doneChan chan struct{}
}

func New(logger *logger.Logger, ch chan smsg.StreamMessage, action string, payload []byte) (*SshPlugin, error) {

	// Unmarshal the Syn payload
	var synPayload bzssh.SshActionParams
	var err error
	if err = json.Unmarshal(payload, &synPayload); err != nil {
		return nil, fmt.Errorf("malformed Ssh plugin SYN payload %v: %s", string(payload), err)
	}

	// Create our plugin
	plugin := &SshPlugin{
		logger:           logger,
		streamOutputChan: ch,
		doneChan:         make(chan struct{}),
	}

	// Start up the action for this plugin
	subLogger := plugin.logger.GetActionLogger(action)

	var parsedAction bzssh.SshAction
	if parsedAction, err = parseAction(action); err != nil {
		return nil, err
	}

	// Create will create the user with the given username if it is allowed, or it will return the existing user
	usr, err := unixuser.LookupOrCreateFromList(synPayload.TargetUser)
	if err != nil {
		return nil, fmt.Errorf("failed to use ssh as user %s: %s", synPayload.TargetUser, err)
	}

	// we place the authorized keys lock file inside the user's /home/.ssh/ directory because that is the least bad place for it
	// source: https://i.stack.imgur.com/BlpRb.png
	authorizedKeysLogger := subLogger.GetComponentLogger("authorized_keys")
	authKeys, err := authorizedkeys.New(authorizedKeysLogger, plugin.doneChan, usr, sshDir, sshDir, maxKeyLifetime)
	if err != nil {
		return nil, fmt.Errorf("failed to set up authorized_keys file: %s", err)
	}

	remoteAddress := fmt.Sprintf("%s:%d", synPayload.RemoteHost, synPayload.RemotePort)
	switch parsedAction {
	case bzssh.OpaqueSsh:
		// Open up a connection to the TCP addr we are trying to connect to
		raddr, err := net.ResolveTCPAddr("tcp", remoteAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve remote address: %s", err)
		}

		remoteConnection, err := net.DialTCP("tcp", nil, raddr)
		if err != nil {
			return nil, fmt.Errorf("failed to dial remote address: %s", err)
		}

		plugin.action = opaquessh.New(
			subLogger,
			plugin.doneChan,
			plugin.streamOutputChan,
			remoteConnection,
			authKeys,
			bzio.OsFileIo{},
		)

	case bzssh.TransparentSsh:
		// we need to add a key for when we "authenticate" our own local connection
		// this doesn't apply to virtual targets, which we will need to consider separately
		privateBytes, publicBytes, _ := bzssh.GenerateKeys()
		authKeys.Add(string(publicBytes))

		signer, err := gossh.ParsePrivateKey(privateBytes)
		if err != nil {
			return nil, err
		}

		config := &gossh.ClientConfig{
			User: synPayload.TargetUser,
			HostKeyCallback: func(hostname string, remote net.Addr, key gossh.PublicKey) error {
				// in future, when we are connecting to remote targets with the agent, we may wish to
				// do something with this key. However, in the current use case we are assured to be
				// connecting to localhost, which makes the host key pretty meaningless.
				// Therefore, we ignore it for now
				return nil
			},
			Auth: []gossh.AuthMethod{
				gossh.PublicKeys(signer),
			},
		}

		conn, err := gossh.Dial("tcp", remoteAddress, config)
		if err != nil {
			return nil, fmt.Errorf("dial error: %s", err)
		}

		plugin.action = transparentssh.New(
			subLogger,
			plugin.doneChan,
			plugin.streamOutputChan,
			conn,
		)

	default:
		return nil, fmt.Errorf("unhandled Ssh action %s", parsedAction)
	}

	plugin.logger.Infof("Ssh plugin started with %v action", action)
	return plugin, nil
}

func (s *SshPlugin) Receive(action string, actionPayload []byte) ([]byte, error) {
	s.logger.Debugf("Ssh plugin received message with %s action", action)

	if payload, err := s.action.Receive(action, actionPayload); err != nil {
		s.logger.Error(err)
		return []byte{}, err
	} else {
		return payload, nil
	}
}

func (s *SshPlugin) Done() <-chan struct{} {
	return s.doneChan
}

func (s *SshPlugin) Kill() {
	if s.action != nil {
		s.action.Kill()
	}
}

func parseAction(action string) (bzssh.SshAction, error) {
	parsedAction := strings.Split(action, "/")
	if len(parsedAction) < 2 {
		return "", fmt.Errorf("malformed action: %s", action)
	}
	return bzssh.SshAction(parsedAction[1]), nil
}
