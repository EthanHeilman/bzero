package shellserver

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"gopkg.in/tomb.v2"

	"bastionzero.com/bctl/v1/bctl/daemon/datachannel"
	"bastionzero.com/bctl/v1/bctl/daemon/mrtap"
	"bastionzero.com/bctl/v1/bctl/daemon/mrtap/bzcert"
	"bastionzero.com/bctl/v1/bctl/daemon/plugin/shell"
	"bastionzero.com/bctl/v1/bctl/daemon/servers/dataconnection"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bctl/v1/bzerolib/keypair"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	bzplugin "bastionzero.com/bctl/v1/bzerolib/plugin"
	bzshell "bastionzero.com/bctl/v1/bzerolib/plugin/shell"
)

const (
	connectionCloseTimeout = 10 * time.Second
)

type ShellServer struct {
	logger  *logger.Logger
	errChan chan error

	conn connection.Connection
	dc   *datachannel.DataChannel

	// Shell specific vars
	targetUser    string
	dataChannelId string

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
) (*ShellServer, error) {

	server := &ShellServer{
		logger:        logger,
		errChan:       errChan,
		cert:          cert,
		targetUser:    targetUser,
		dataChannelId: dataChannelId,
		agentPubKey:   agentPubKey,
	}

	// Create our one connection
	subLogger := logger.GetConnectionLogger(uuid.New().String())
	wsLogger := logger.GetComponentLogger("Websocket")
	srLogger := logger.GetComponentLogger("SignalR")

	client := signalr.New(srLogger, websocket.New(wsLogger))
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
		server.logger.Infof("shell server tmb is dying")
		err := server.tmb.Err()
		if server.conn != nil {
			server.conn.Close(err, connectionCloseTimeout)
		}
		server.errChan <- err
		return err
	})

	return server, nil
}

func (ss *ShellServer) Start() error {
	if err := ss.newDataChannel(string(bzshell.DefaultShell)); err != nil {
		ss.conn.Close(err, connectionCloseTimeout)
		return fmt.Errorf("failed to create datachannel: %s", err)
	}
	return nil
}

func (ss *ShellServer) Close(err error) {
	ss.tmb.Kill(err)
}

func (ss *ShellServer) listenForChildrenDone() {
	// blocks until an underlying tomb is dead
	// we do it this way to prevent ss.Close() from being called twice in the event that dc dies first
	select {
	case <-ss.conn.Done():
		ss.Close(ss.conn.Err())
	case <-ss.dc.Done():
		ss.Close(ss.dc.Err())
	}
}

// for creating new datachannels
func (ss *ShellServer) newDataChannel(action string) error {
	var attach bool
	if ss.dataChannelId == "" {
		ss.dataChannelId = uuid.New().String()
		attach = false
		ss.logger.Infof("Creating new datachannel id: %s", ss.dataChannelId)
	} else {
		attach = true
		ss.logger.Infof("Attaching to an existing datachannel id: %s", ss.dataChannelId)
	}

	// every datachannel gets a uuid to distinguish it so a single connection can map to multiple datachannels
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

	mtLogger := ss.logger.GetComponentLogger("mrtap")
	mt, err := mrtap.New(mtLogger, ss.agentPubKey, ss.cert)
	if err != nil {
		return err
	}

	action = "shell/" + action
	ss.dc, err = datachannel.New(subLogger, ss.dataChannelId, ss.conn, mt, plugin, action, synPayload, attach, false)
	if err != nil {
		return err
	}

	// listen for news that the datachannel has died
	go ss.listenForChildrenDone()
	return nil
}
