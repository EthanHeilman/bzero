package shell

import (
	"encoding/json"
	"fmt"
	"strings"

	"bastionzero.com/bctl/v1/bctl/agent/plugin/shell/actions/defaultshell"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/plugin/shell"
	smsg "bastionzero.com/bctl/v1/bzerolib/stream/message"
)

type IShellAction interface {
	Receive(action string, actionPayload []byte) ([]byte, error)
	Kill()
}

type ShellPlugin struct {
	logger *logger.Logger

	action           IShellAction
	streamOutputChan chan smsg.StreamMessage
	doneChan         chan struct{}

	runAsUser string
}

func New(
	logger *logger.Logger,
	ch chan smsg.StreamMessage,
	action string,
	payload []byte,
) (*ShellPlugin, error) {

	// Unmarshal the Syn payload
	var synPayload shell.ShellActionParams
	if err := json.Unmarshal(payload, &synPayload); err != nil {
		return nil, fmt.Errorf("malformed Shell plugin SYN payload %s", string(payload))
	}
	fmt.Println("HERE 2.3.2.1")

	// Create our plugin
	plugin := &ShellPlugin{
		logger:           logger,
		streamOutputChan: ch,
		doneChan:         make(chan struct{}),
		runAsUser:        synPayload.TargetUser,
	}

	fmt.Println("HERE 2.3.2.2")

	// Start up the action for this plugin
	subLogger := plugin.logger.GetActionLogger(action)

	fmt.Println("HERE 2.3.2.3")

	if parsedAction, err := parseAction(action); err != nil {
		fmt.Println("Err 2.3.2.4", err)

		return nil, err
	} else {
		fmt.Println("HERE 2.3.2.4.1")

		switch parsedAction {
		case shell.DefaultShell:
			fmt.Println("HERE 2.3.2.5")

			plugin.action = defaultshell.New(subLogger, plugin.streamOutputChan, plugin.doneChan, plugin.runAsUser)
			plugin.logger.Infof("Shell plugin started %v action", action)
			return plugin, nil
		default:
			fmt.Println("HERE 2.3.2.6")
			return nil, fmt.Errorf("could not start unhandled shell action: %v", action)
		}
	}
}

func (s *ShellPlugin) Receive(action string, actionPayload []byte) ([]byte, error) {
	s.logger.Debugf("Shell plugin received message with %s action", action)

	if payload, err := s.action.Receive(action, actionPayload); err != nil {
		return []byte{}, err
	} else {
		return payload, err
	}
}

func parseAction(action string) (shell.ShellAction, error) {
	fmt.Println("HERE 2.3.2.3.1", action)

	parsedAction := strings.Split(action, "/")
	fmt.Println("HERE 2.3.2.3.2", parsedAction)

	if len(parsedAction) < 2 {
		return "", fmt.Errorf("malformed action: %s", action)
	}
	fmt.Println("HERE 2.3.2.3.3", parsedAction[1])

	return shell.ShellAction(parsedAction[1]), nil
}

func (s *ShellPlugin) Done() <-chan struct{} {
	return s.doneChan
}

func (s *ShellPlugin) Kill() {
	if s.action != nil {
		s.action.Kill()
	}
}
