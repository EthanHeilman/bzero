package sshserver

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"gopkg.in/tomb.v2"

	"bastionzero.com/bctl/v1/bctl/daemon/datachannel"
	"bastionzero.com/bctl/v1/bctl/daemon/mrtap"
	"bastionzero.com/bctl/v1/bctl/daemon/mrtap/bzcert"
	"bastionzero.com/bctl/v1/bctl/daemon/plugin/ssh"
	"bastionzero.com/bctl/v1/bctl/daemon/servers/dataconnection"
	"bastionzero.com/bctl/v1/bzerolib/bzio"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bctl/v1/bzerolib/keypair"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	bzplugin "bastionzero.com/bctl/v1/bzerolib/plugin"
	bzssh "bastionzero.com/bctl/v1/bzerolib/plugin/ssh"
	"github.com/google/uuid"
)

const (
	connectionCloseTimeout = 10 * time.Second
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
	agentPubKey *keypair.PublicKey
	cert        *bzcert.DaemonBZCert

	tmb tomb.Tomb
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
	agentPubKey *keypair.PublicKey,
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
	wsLogger := logger.GetComponentLogger("Websocket")
	srLogger := logger.GetComponentLogger("SignalR")

	client := signalr.New(srLogger, nil, websocket.New(wsLogger, nil))
	if client, err := dataconnection.New(subLogger, connUrl, params, headers, client); err != nil {
		return nil, fmt.Errorf("failed to create connection: %s", err)
	} else {
		server.conn = client
	}

	// Create a tmb that just waits until its killed via server.Close and pushes
	// the error to the errChan. Using a tmb prevents any side-effects from
	// server.Close from being called multiple times.
	server.tmb.Go(func() error {
		<-server.tmb.Dying()
		err := server.tmb.Err()
		if server.conn != nil {
			server.conn.Close(err, connectionCloseTimeout)
		}
		server.errChan <- err
		return err
	})

	return server, nil
}

func (s *SshServer) Start() error {
	if err := s.newDataChannel(s.action); err != nil {
		s.conn.Close(err, connectionCloseTimeout)
		return fmt.Errorf("failed to create datachannel: %s", err)
	}
	return nil
}

func (s *SshServer) Close(err error) {
	s.tmb.Kill(err)
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

	// clear knownhosts file so that it only contains the key(s) from this session
	if err := fileIo.Truncate(s.knownHostsFile, 0); err != nil {
		s.logger.Errorf("failed to truncate known hosts file: %s", err)
	}
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

	mtLogger := s.logger.GetComponentLogger("mrtap")
	mt, err := mrtap.New(mtLogger, s.agentPubKey, s.cert)
	if err != nil {
		return err
	}

	action = "ssh/" + action
	s.dc, err = datachannel.New(subLogger, dcId, s.conn, mt, plugin, action, synPayload, attach, false)
	if err != nil {
		return err
	}

	// listen for news that the datachannel has died
	go s.listenForChildrenDone()
	return nil
}
