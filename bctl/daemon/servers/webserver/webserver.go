package webserver

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"bastionzero.com/bctl/v1/bctl/daemon/datachannel"
	"bastionzero.com/bctl/v1/bctl/daemon/keysplitting"
	"bastionzero.com/bctl/v1/bctl/daemon/keysplitting/bzcert"
	"bastionzero.com/bctl/v1/bctl/daemon/plugin/web"
	"bastionzero.com/bctl/v1/bctl/daemon/servers/daemondatachannelconnection"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	bzplugin "bastionzero.com/bctl/v1/bzerolib/plugin"
	bzweb "bastionzero.com/bctl/v1/bzerolib/plugin/web"
)

const (
	// connection parameters for all datachannels created by tcp server
	autoReconnect = true

	// TODO: make these easily configurable values
	maxRequestSize = 10 * 1024 * 1024  // 10MB
	maxFileUpload  = 151 * 1024 * 1024 // 151MB a little extra for request fluff
)

type WebServer struct {
	logger  *logger.Logger
	errChan chan error

	conn connection.Connection

	// Web specific vars
	// Either user the full dns (i.e. targetHostName) or the host:port
	targetPort int
	targetHost string

	// fields for new datachannels
	localPort   string
	localHost   string
	agentPubKey string
	cert        *bzcert.DaemonBZCert
}

func New(
	logger *logger.Logger,
	errChan chan error,
	localPort string,
	localHost string,
	targetPort int,
	targetHost string,
	cert *bzcert.DaemonBZCert,
	connUrl string,
	params url.Values,
	headers http.Header,
	agentPubKey string,
) (*WebServer, error) {

	server := &WebServer{
		logger:      logger,
		errChan:     errChan,
		cert:        cert,
		localPort:   localPort,
		localHost:   localHost,
		targetHost:  targetHost,
		targetPort:  targetPort,
		agentPubKey: agentPubKey,
	}

	// Create our one connection
	subLogger := logger.GetConnectionLogger(uuid.New().String())
	wsLogger := logger.GetComponentLogger("Websocket")
	if client, err := daemondatachannelconnection.New(subLogger, connUrl, params, headers, autoReconnect, websocket.New(wsLogger)); err != nil {
		return nil, fmt.Errorf("failed to create connection: %s", err)
	} else {
		server.conn = client
	}

	go server.listenForConnectionDone()

	return server, nil
}

func (w *WebServer) Start() error {
	// Create HTTP Server listens for incoming kubectl commands
	go func() {
		// Define our http handlers
		// library will automatically put each call in its own thread
		http.HandleFunc("/", w.capRequestSize(w.handleHttp))

		if err := http.ListenAndServe(fmt.Sprintf("%s:%s", w.localHost, w.localPort), nil); err != nil {
			w.logger.Error(err)
		}
	}()
	return nil
}

func (w *WebServer) Close(err error) {
	if w.conn != nil {
		w.conn.Close(err)
	}
	w.errChan <- err
}

func (w *WebServer) listenForConnectionDone() {
	// blocks until the underlying tomb is dead
	<-w.conn.Done()
	w.Close(w.conn.Err())
}

// this function operates as middleware between the http handler and the handleHttp call below
// it checks to see if someone is trying to send a request body that is far too large
func (w *WebServer) capRequestSize(h http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.Header.Get("Content-Type"), "multipart") {
			if request.ContentLength > maxFileUpload {
				// for multipart/form-data type requests, the request body won't exceed our maximum single request size
				// but we still want to cap the size of uploads because they are stored in their entirety on the target.
				// Not optimal, here's the ticket: CWC-1647
				// We shouldn't be relying on content length too much since it can be modified to be whatever.
				rerr := "BastionZero: Request is too large. Maximum upload is 150MB"
				w.logger.Errorf(rerr)
				http.Error(writer, rerr, http.StatusRequestEntityTooLarge)
				return
			}
		} else {
			request.Body = http.MaxBytesReader(writer, request.Body, maxRequestSize)
			if err := request.ParseForm(); err != nil {
				rerr := "BastionZero: Request is too large. Maximum request size is 10MB"
				w.logger.Errorf(rerr)
				http.Error(writer, rerr, http.StatusRequestEntityTooLarge)
				return
			}
		}

		h(writer, request)
	}
}

func (w *WebServer) handleHttp(writer http.ResponseWriter, request *http.Request) {
	// every datachannel gets a uuid to distinguish it so a single connection can map to multiple datachannels
	dcId := uuid.New().String()

	// create our new plugin and datachannel
	subLogger := w.logger.GetDatachannelLogger(dcId)
	subLogger = subLogger.GetPluginLogger(bzplugin.Web)
	plugin := web.New(subLogger, w.targetHost, w.targetPort)

	action := bzweb.Dial
	// This will work for http 1.1 and that is what we need to support
	// Ref: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Upgrade
	// Ref: https://datatracker.ietf.org/doc/html/rfc6455#section-1.7
	isWebsocketRequest := request.Header.Get("Upgrade")
	if isWebsocketRequest == "websocket" {
		action = bzweb.Websocket
	}

	if err := w.newDataChannel(dcId, action, plugin); err != nil {
		w.logger.Errorf("error starting datachannel: %s", err)
	}
	if err := plugin.StartAction(action, writer, request); err != nil {
		w.logger.Errorf("error starting action: %s", err)
	}
}

// for creating new datachannels
func (w *WebServer) newDataChannel(dcId string, action bzweb.WebAction, plugin *web.WebDaemonPlugin) error {

	attach := false
	subLogger := w.logger.GetDatachannelLogger(dcId)

	w.logger.Infof("Creating new datachannel for web with id: %s", dcId)

	// Build the actionParams to send to the datachannel to start the plugin
	synPayload := bzweb.WebActionParams{
		RemotePort: w.targetPort,
		RemoteHost: w.targetHost,
	}

	ksLogger := w.logger.GetComponentLogger("mrzap")
	keysplitter, err := keysplitting.New(ksLogger, w.agentPubKey, w.cert)
	if err != nil {
		return err
	}

	actString := "web/" + string(action)
	_, err = datachannel.New(subLogger, dcId, w.conn, keysplitter, plugin, actString, synPayload, attach, true)
	if err != nil {
		return err
	}
	return nil
}
