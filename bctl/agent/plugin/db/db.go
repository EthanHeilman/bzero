package db

import (
	"encoding/json"
	"fmt"
	"strings"

	"bastionzero.com/agent/bastion"
	"bastionzero.com/agent/plugin/db/actions/dial"
	"bastionzero.com/agent/plugin/db/actions/pwdb"
	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/plugin/db"
	smsg "bastionzero.com/bzerolib/stream/message"
)

type IDbAction interface {
	Receive(action string, actionPayload []byte) ([]byte, error)
	Kill()
}

type DbPlugin struct {
	logger *logger.Logger

	action           IDbAction
	streamOutputChan chan smsg.StreamMessage
	doneChan         chan struct{}

	// Either use the host:port
	remotePort int
	remoteHost string
}

func New(logger *logger.Logger,
	ch chan smsg.StreamMessage,
	keyshardConfig pwdb.PWDBConfig,
	bastion bastion.ApiClient,
	action string,
	payload []byte,
) (*DbPlugin, error) {

	// Unmarshal the Syn payload
	var syn db.DbActionParams
	if err := json.Unmarshal(payload, &syn); err != nil {
		return nil, fmt.Errorf("malformed SYN payload: %s", err)
	}

	// Create our plugin
	plugin := &DbPlugin{
		logger:           logger,
		streamOutputChan: ch,
		doneChan:         make(chan struct{}),
		remotePort:       syn.RemotePort,
		remoteHost:       syn.RemoteHost,
	}

	// Start up the action for this plugin
	subLogger := plugin.logger.GetActionLogger(action)
	if parsedAction, parsedTcpApp, err := parseActionTCPApp(action); err != nil {
		return nil, err
	} else {
		if parsedTcpApp == "" {
			return nil, fmt.Errorf("undefined tcp application: %s", parsedTcpApp)
		}
		var rerr error

		switch parsedAction {
		case db.Dial:
			plugin.action, rerr = dial.New(subLogger, plugin.streamOutputChan, plugin.doneChan, syn.RemoteHost, syn.RemotePort)
		case db.Pwdb:
			plugin.action, rerr = pwdb.New(subLogger, plugin.streamOutputChan, plugin.doneChan, keyshardConfig, bastion, syn.RemoteHost, syn.RemotePort)
		default:
			rerr = fmt.Errorf("unhandled DB action")
		}

		if rerr != nil {
			return nil, fmt.Errorf("failed to start DB plugin with action %s: %s", action, rerr)
		} else {
			plugin.logger.Infof("DB plugin started with %s action", action)
			return plugin, nil
		}
	}
}

func (d *DbPlugin) Done() <-chan struct{} {
	return d.doneChan
}

func (d *DbPlugin) Kill() {
	if d.action != nil {
		d.action.Kill()
	}
}

func (d *DbPlugin) Receive(action string, actionPayload []byte) ([]byte, error) {
	d.logger.Tracef("DB plugin received message with %s action", action)

	if payload, err := d.action.Receive(action, actionPayload); err != nil {
		return []byte{}, err
	} else {
		return payload, err
	}
}

// Parses the provided plugin action and the specified TCP application
func parseActionTCPApp(action string) (db.DbAction, db.TCPApplication, error) {
	parsedAction := strings.Split(action, "/")
	if len(parsedAction) < 2 {
		return "", "", fmt.Errorf("malformed action: %s", action)
	} else if len(parsedAction) == 2 {
		return db.DbAction(parsedAction[1]), db.DB, nil
	}
	return db.DbAction(parsedAction[1]), db.TCPApplication(parsedAction[2]), nil
}
