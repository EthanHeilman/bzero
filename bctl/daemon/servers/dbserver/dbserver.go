package dbserver

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"bastionzero.com/bzerolib/connection"
	"bastionzero.com/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bzerolib/keypair"
	"bastionzero.com/bzerolib/logger"
	bzplugin "bastionzero.com/bzerolib/plugin"
	bzdb "bastionzero.com/bzerolib/plugin/db"
	"bastionzero.com/daemon/datachannel"
	"bastionzero.com/daemon/mrtap"
	"bastionzero.com/daemon/mrtap/bzcert"
	"bastionzero.com/daemon/plugin/db"
	"bastionzero.com/daemon/servers/dataconnection"
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
	tcpApp     bzdb.TCPApplication
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
	tcpApp string,
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

	tcpApplication := bzdb.TCPApplication(tcpApp)
	if tcpApplication == "" {
		return nil, fmt.Errorf("unrecognized tcp application")
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
		tcpApp:      tcpApplication,
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
	if d.action == bzdb.Pwdb {
		// Test connection so that we can make some errors synchronous, we don't have control over how tunnelling
		// protocols decide to display error, if they even do. This means we can do our best to catch and display
		// to the user while we still have their attention
		// However, it doesn't yet make sense to do this for all connections, since applications that expect a data
		// stream will behave weirdly when the test connection comes in
		d.logger.Infof("Testing connection")
		server, _ := net.Pipe()
		defer server.Close()
		if err := d.newAction(server); err != nil {
			return err
		}
		d.logger.Infof("Connection passed all tests")
	}

	// Now create our local listener for TCP connections
	addr := fmt.Sprintf("%s:%s", d.localHost, d.localPort)
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

	if d.action == bzdb.Dial {
		// Do nothing with the first syn no-op call
		d.tcpListener.AcceptTCP()
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

func (d *DbServer) newAction(conn net.Conn) error {
	// every datachannel gets a uuid to distinguish it so a single connection can map to multiple datachannels
	dcId := uuid.New().String()
	subLogger := d.logger.GetDatachannelLogger(dcId)
	pluginLogger := subLogger.GetPluginLogger(bzplugin.Db)

	plugin := db.New(pluginLogger, d.targetUser, d.targetId)

	if err := d.newDataChannel(dcId, plugin); err != nil {
		return fmt.Errorf("error starting datachannel: %w", err)
	}

	d.logger.Infof("Starting plugin action")
	if err := plugin.StartAction(d.action, d.tcpApp, conn); err != nil {
		return fmt.Errorf("error starting action: %w", err)
	}

	return nil
}

func (d *DbServer) newDataChannel(dcId string, plugin *db.DbDaemonPlugin) error {
	subLogger := d.logger.GetDatachannelLogger(dcId)

	d.logger.Infof("Creating new datachannel for db with id: %s", dcId)

	// Build the synPayload to send to the datachannel to start the plugin
	var synPayload interface{}
	switch d.tcpApp {
	case bzdb.DB:
		synPayload = bzdb.DbActionParams{
			RemotePort: d.remotePort,
			RemoteHost: d.remoteHost,
		}
	case bzdb.RDP:
		synPayload = bzdb.RDPActionParams{
			RemotePort: d.remotePort,
			RemoteHost: d.remoteHost,
		}
	case bzdb.SQLSERVER:
		synPayload = bzdb.SQLServerActionParams{
			RemotePort: d.remotePort,
			RemoteHost: d.remoteHost,
		}
	default:
		return fmt.Errorf("unsupported tcp application type: %s", d.tcpApp)
	}

	mtLogger := subLogger.GetComponentLogger("mrtap")
	mt, err := mrtap.New(mtLogger, d.agentPubKey, d.cert)
	if err != nil {
		return err
	}

	action := "db/" + string(d.action) + "/" + string(d.tcpApp)
	attach := false
	_, err = datachannel.New(subLogger, dcId, d.conn, mt, plugin, action, synPayload, attach, true)
	if err != nil {
		return err
	}
	return nil
}
