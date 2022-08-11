package sshserver

import (
	"fmt"

	"bastionzero.com/bctl/v1/bctl/daemon/datachannel"
	"bastionzero.com/bctl/v1/bctl/daemon/keysplitting"
	"bastionzero.com/bctl/v1/bctl/daemon/keysplitting/bzcert"
	"bastionzero.com/bctl/v1/bctl/daemon/plugin/ssh"
	"bastionzero.com/bctl/v1/bzerolib/bzio"
	"bastionzero.com/bctl/v1/bzerolib/channels/websocket"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	bzplugin "bastionzero.com/bctl/v1/bzerolib/plugin"
	bzssh "bastionzero.com/bctl/v1/bzerolib/plugin/ssh"
	"github.com/google/uuid"
)

const (
	// websocket connection parameters for all datachannels created by tcp server
	autoReconnect = false
	getChallenge  = false
)

type SshServer struct {
	logger  *logger.Logger
	errChan chan error
	action  string

	websocket *websocket.Websocket
	dc        *datachannel.DataChannel

	remoteHost string
	remotePort int
	localPort  string
	targetUser string

	identityFile   string
	knownHostsFile string
	hostNames      []string

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
	identityFile string,
	knownHostsFile string,
	hostNames []string,
	remoteHost string,
	remotePort int,
	localPort string,
	action string,
) (*SshServer, error) {

	server := &SshServer{
		logger:         logger,
		errChan:        errChan,
		action:         action,
		targetUser:     targetUser,
		cert:           cert,
		agentPubKey:    agentPubKey,
		identityFile:   identityFile,
		knownHostsFile: knownHostsFile,
		hostNames:      hostNames,
		remoteHost:     remoteHost,
		remotePort:     remotePort,
		localPort:      localPort,
	}

	// Create a new websocket and datachannel
	if err := server.newWebsocket(uuid.New().String(), serviceUrl, params, headers); err != nil {
		return nil, fmt.Errorf("failed to create websocket: %s", err)
	}

	return server, nil
}

func (s *SshServer) Start() error {
	if err := s.newDataChannel(s.action, s.websocket); err != nil {
		s.websocket.Close(err)
		return fmt.Errorf("failed to create datachannel: %s", err)
	}
	return nil
}

func (s *SshServer) Close(err error) {
	if s.websocket != nil {
		s.websocket.Close(err)
	}
	s.errChan <- err
}

// for creating new websockets
func (s *SshServer) newWebsocket(wsId string, serviceUrl string, params map[string]string, headers map[string]string) error {
	subLogger := s.logger.GetWebsocketLogger(wsId)
	if wsClient, err := websocket.New(subLogger, serviceUrl, params, headers, autoReconnect, getChallenge, websocket.Ssh); err != nil {
		return err
	} else {
		s.websocket = wsClient
		return nil
	}
}

func (s *SshServer) listenForChildrenDone() {
	// blocks until an underlying tomb is dead
	// we do it this way to prevent s.Close() from being called twice in the event that dc dies first
	select {
	case <-s.websocket.Done():
		s.Close(s.websocket.Err())
	case <-s.dc.Done():
		s.Close(s.dc.Err())
	}
}

// for creating new datachannels
func (s *SshServer) newDataChannel(action string, websocket *websocket.Websocket) error {
	dcId := uuid.New().String()
	attach := false
	subLogger := s.logger.GetDatachannelLogger(dcId)

	s.logger.Infof("Creating new datachannel id: %s", dcId)

	fileIo := bzio.OsFileIo{}

	idFile := bzssh.NewIdentityFile(s.identityFile, fileIo)
	khFile := bzssh.NewKnownHosts(s.knownHostsFile, s.hostNames, fileIo)

	pluginLogger := subLogger.GetPluginLogger(bzplugin.Ssh)
	plugin := ssh.New(pluginLogger, s.localPort, idFile, khFile, bzio.StdIo{})
	if err := plugin.StartAction(action); err != nil {
		return fmt.Errorf("failed to start action: %s", err)
	}

	synPayload := bzssh.SshActionParams{
		TargetUser: s.targetUser,
		RemoteHost: s.remoteHost,
		RemotePort: s.remotePort,
	}

	ksLogger := s.logger.GetComponentLogger("mrzap")
	keysplitter, err := keysplitting.New(ksLogger, s.agentPubKey, s.cert)
	if err != nil {
		return err
	}

	action = "ssh/" + action
	s.dc, err = datachannel.New(subLogger, dcId, websocket, keysplitter, plugin, action, synPayload, attach, false)
	if err != nil {
		return err
	}

	// listen for news that the datachannel has died
	go s.listenForChildrenDone()
	return nil
}
