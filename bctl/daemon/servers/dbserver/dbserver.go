package dbserver

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"bastionzero.com/bctl/v1/bctl/daemon/datachannel"
	"bastionzero.com/bctl/v1/bctl/daemon/mrtap"
	"bastionzero.com/bctl/v1/bctl/daemon/mrtap/bzcert"
	"bastionzero.com/bctl/v1/bctl/daemon/plugin/db"
	"bastionzero.com/bctl/v1/bctl/daemon/servers/dataconnection"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bctl/v1/bzerolib/keypair"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	bzplugin "bastionzero.com/bctl/v1/bzerolib/plugin"
	bzdb "bastionzero.com/bctl/v1/bzerolib/plugin/db"
)

const (
	connectionCloseTimeout = 10 * time.Second
)

type DbServer struct {
	logger *logger.Logger

	conn connection.Connection

	errChan     chan error
	tcpListener *net.TCPListener

	// Db specific vars
	action     bzdb.DbAction
	remotePort int
	remoteHost string
	targetUser string
	targetId   string

	// fields for new datachannels
	localPort   string
	localHost   string
	agentPubKey *keypair.PublicKey
	cert        *bzcert.DaemonBZCert
}

func New(logger *logger.Logger,
	errChan chan error,
	localPort string,
	localHost string,
	remotePort int,
	remoteHost string,
	cert *bzcert.DaemonBZCert,
	action string,
	targetUser string,
	targetId string,
	connUrl string,
	params url.Values,
	headers http.Header,
	agentPubKey *keypair.PublicKey,
) (*DbServer, error) {
	act := bzdb.DbAction(action)
	if act == "" {
		return nil, fmt.Errorf("unrecognized db action")
	}

	server := &DbServer{
		logger:      logger,
		errChan:     errChan,
		cert:        cert,
		localPort:   localPort,
		localHost:   localHost,
		targetUser:  targetUser,
		targetId:    targetId,
		remoteHost:  remoteHost,
		remotePort:  remotePort,
		action:      act,
		agentPubKey: agentPubKey,
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

	go server.listenForConnectionDone()

	return server, nil
}

func (d *DbServer) Start() error {
	// Test connection so that we can make some errors synchronous, we don't have control over how tunnelling
	// protocols decide to display error, if they even do. This means we can do our best to catch and display
	// to the user while we still have their attention
	d.logger.Infof("Testing connection")
	if err := d.newAction(nil); err != nil {
		return err
	}

	d.logger.Infof("Connection passed all tests")

	addr := fmt.Sprintf("%s:%s", d.localHost, d.localPort)

	// Now create our local listener for TCP connections
	localTcpAddress, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		d.conn.Close(err, connectionCloseTimeout)
		return fmt.Errorf("failed to resolve address %s: %s", addr, err)
	}

	d.logger.Infof("Setting up TCP listener")
	d.tcpListener, err = net.ListenTCP("tcp", localTcpAddress)
	if err != nil {
		d.conn.Close(err, connectionCloseTimeout)
		return fmt.Errorf("failed to open local port to listen: %s", err)
	}

	go d.handleConnections()

	d.logger.Infof("Listening on %s", addr)

	return nil
}

func (d *DbServer) Close(err error) {
	if d.conn != nil {
		d.conn.Close(err, connectionCloseTimeout)
	}
	if d.tcpListener != nil {
		d.tcpListener.Close()
	}
	d.errChan <- err
}

func (d *DbServer) listenForConnectionDone() {
	// blocks until the underlying tomb is dead
	<-d.conn.Done()
	d.Close(d.conn.Err())
}

func (d *DbServer) handleConnections() {
	// Block and keep listening for new tcp events
	for {
		conn, err := d.tcpListener.AcceptTCP()
		if err != nil {
			d.logger.Errorf("failed to accept connection: %s", err)
			return
		}

		d.logger.Infof("Accepting new tcp connection")

		// create our new datachannel in its own go routine so that we can accept other tcp connections
		go func() {
			if err := d.newAction(conn); err != nil {
				d.Close(err)
			}
		}()
	}
}

func (d *DbServer) newAction(conn *net.TCPConn) error {
	// every datachannel gets a uuid to distinguish it so a single connection can map to multiple datachannels
	dcId := uuid.New().String()
	subLogger := d.logger.GetDatachannelLogger(dcId)
	pluginLogger := subLogger.GetPluginLogger(bzplugin.Db)

	plugin := db.New(pluginLogger, d.targetUser, d.targetId)

	if err := d.newDataChannel(dcId, string(d.action), plugin); err != nil {
		return fmt.Errorf("error starting datachannel: %w", err)
	}

	d.logger.Infof("Starting plugin action")
	if err := plugin.StartAction(d.action, conn); err != nil {
		return fmt.Errorf("error starting action: %w", err)
	}

	return nil
}

// for creating new datachannels
func (d *DbServer) newDataChannel(dcId string, action string, plugin *db.DbDaemonPlugin) error {
	subLogger := d.logger.GetDatachannelLogger(dcId)

	d.logger.Infof("Creating new datachannel id: %s", dcId)

	// Build the synPayload to send to the datachannel to start the plugin
	synPayload := bzdb.DbActionParams{
		RemotePort: d.remotePort,
		RemoteHost: d.remoteHost,
	}

	mtLogger := d.logger.GetComponentLogger("mrtap")
	mt, err := mrtap.New(mtLogger, d.agentPubKey, d.cert)
	if err != nil {
		return err
	}

	action = "db/" + action
	attach := false
	_, err = datachannel.New(subLogger, dcId, d.conn, mt, plugin, action, synPayload, attach, true)
	if err != nil {
		return err
	}
	return nil
}
