package sshserver

import (
	"fmt"
	"net/http"
	"net/url"

	"bastionzero.com/bctl/v1/bctl/daemon/datachannel"
	"bastionzero.com/bctl/v1/bctl/daemon/keysplitting"
	"bastionzero.com/bctl/v1/bctl/daemon/keysplitting/bzcert"
	"bastionzero.com/bctl/v1/bctl/daemon/plugin/ssh"
	"bastionzero.com/bctl/v1/bzerolib/bzio"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	"bastionzero.com/bctl/v1/bzerolib/connection/universalconnection"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	bzplugin "bastionzero.com/bctl/v1/bzerolib/plugin"
	bzssh "bastionzero.com/bctl/v1/bzerolib/plugin/ssh"
	"github.com/google/uuid"
)

const (
	// connection parameters for all datachannels created by tcp server
	autoReconnect = false
)

type SshServer struct {
	logger  *logger.Logger
	errChan chan error
	action  string

	conn connection.Connection
	dc   *datachannel.DataChannel

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
	connUrl string,
	params url.Values,
	headers http.Header,
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
		localPort:      localPort,
		remoteHost:     remoteHost,
		remotePort:     remotePort,
	}

	// Create our one connection
	subLogger := logger.GetConnectionLogger(uuid.New().String())
	if client, err := universalconnection.New(subLogger, connUrl, params, headers, autoReconnect, universalconnection.DaemonDataChannel); err != nil {
		return nil, fmt.Errorf("failed to create connection: %s", err)
	} else {
		server.conn = client
	}

	return server, nil
}

func (s *SshServer) Start() error {
	if err := s.newDataChannel(s.action); err != nil {
		s.conn.Close(err)
		return fmt.Errorf("failed to create datachannel: %s", err)
	}
	return nil
}

func (s *SshServer) Close(err error) {
	if s.conn != nil {
		s.conn.Close(err)
	}
	s.errChan <- err
}

func (s *SshServer) listenForChildrenDone() {
	// blocks until an underlying tomb is dead
	// we do it this way to prevent s.Close() from being called twice in the event that dc dies first
	select {
	case <-s.conn.Done():
		s.Close(s.conn.Err())
	case <-s.dc.Done():
		s.Close(s.dc.Err())
	}
}

// for creating new datachannels
func (s *SshServer) newDataChannel(action string) error {
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
	s.dc, err = datachannel.New(subLogger, dcId, s.conn, keysplitter, plugin, action, synPayload, attach, false)
	if err != nil {
		return err
	}

	// listen for news that the datachannel has died
	go s.listenForChildrenDone()
	return nil
}
