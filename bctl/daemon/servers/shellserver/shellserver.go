package shellserver

import (
	"fmt"

	"github.com/google/uuid"

	"bastionzero.com/bctl/v1/bctl/daemon/datachannel"
	"bastionzero.com/bctl/v1/bctl/daemon/keysplitting"
	"bastionzero.com/bctl/v1/bctl/daemon/keysplitting/bzcert"
	"bastionzero.com/bctl/v1/bctl/daemon/plugin/shell"
	"bastionzero.com/bctl/v1/bzerolib/channels/websocket"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	bzplugin "bastionzero.com/bctl/v1/bzerolib/plugin"
	bzshell "bastionzero.com/bctl/v1/bzerolib/plugin/shell"
)

const (
	// websocket connection parameters for all datachannels created by tcp server
	autoReconnect = false
	getChallenge  = false
)

type ShellServer struct {
	logger  *logger.Logger
	errChan chan error

	websocket *websocket.Websocket
	dc        *datachannel.DataChannel

	// Shell specific vars
	targetUser    string
	dataChannelId string

	// fields for new datachannels
	agentPubKey string
	cert        *bzcert.DaemonBZCert
}

func New(
	logger *logger.Logger,
	errChan chan error,
	targetUser string,
	dataChannelId string,
	cert *bzcert.DaemonBZCert,
	serviceUrl string,
	params map[string]string,
	headers map[string]string,
	agentPubKey string,
) (*ShellServer, error) {

	server := &ShellServer{
		logger:        logger,
		errChan:       errChan,
		cert:          cert,
		targetUser:    targetUser,
		dataChannelId: dataChannelId,
		agentPubKey:   agentPubKey,
	}

	// Create a new websocket and datachannel
	if err := server.newWebsocket(uuid.New().String(), serviceUrl, params, headers); err != nil {
		return nil, fmt.Errorf("failed to create websocket: %s", err)
	}

	return server, nil
}

func (ss *ShellServer) Start() error {
	if err := ss.newDataChannel(string(bzshell.DefaultShell), ss.websocket); err != nil {
		ss.websocket.Close(err)
		return fmt.Errorf("failed to create datachannel: %s", err)
	}
	return nil
}

func (ss *ShellServer) Close(err error) {
	if ss.websocket != nil {
		ss.websocket.Close(err)
	}
	ss.errChan <- err
}

// for creating new websockets
func (ss *ShellServer) newWebsocket(wsId string, serviceUrl string, params map[string]string, headers map[string]string) error {
	subLogger := ss.logger.GetWebsocketLogger(wsId)
	if wsClient, err := websocket.New(subLogger, serviceUrl, params, headers, autoReconnect, getChallenge, websocket.Shell); err != nil {
		return err
	} else {
		ss.websocket = wsClient
		return nil
	}
}

func (ss *ShellServer) listenForChildrenDone() {
	// blocks until an underlying tomb is dead
	// we do it this way to prevent ss.Close() from being called twice in the event that dc dies first
	select {
	case <-ss.websocket.Done():
		ss.Close(ss.websocket.Err())
	case <-ss.dc.Done():
		ss.Close(ss.dc.Err())
	}
}

// for creating new datachannels
func (ss *ShellServer) newDataChannel(action string, websocket *websocket.Websocket) error {
	var attach bool
	if ss.dataChannelId == "" {
		ss.dataChannelId = uuid.New().String()
		attach = false
		ss.logger.Infof("Creating new datachannel id: %s", ss.dataChannelId)
	} else {
		attach = true
		ss.logger.Infof("Attaching to an existing datachannel id: %s", ss.dataChannelId)
	}

	// every datachannel gets a uuid to distinguish it so a single websockets can map to multiple datachannels
	subLogger := ss.logger.GetDatachannelLogger(ss.dataChannelId)

	// create our plugin and start the action
	pluginLogger := subLogger.GetPluginLogger(bzplugin.Shell)
	plugin := shell.New(pluginLogger)
	if err := plugin.StartAction(attach); err != nil {
		return fmt.Errorf("failed to start action: %s", err)
	}

	// Build the action payload to send in the syn message when opening the datachannel
	synPayload := bzshell.ShellActionParams{
		TargetUser: ss.targetUser,
	}

	ksLogger := ss.logger.GetComponentLogger("mrzap")
	keysplitter, err := keysplitting.New(ksLogger, ss.agentPubKey, ss.cert)
	if err != nil {
		return err
	}

	action = "shell/" + action
	ss.dc, err = datachannel.New(subLogger, ss.dataChannelId, websocket, keysplitter, plugin, action, synPayload, attach, false)
	if err != nil {
		return err
	}

	// listen for news that the datachannel has died
	go ss.listenForChildrenDone()
	return nil
}
