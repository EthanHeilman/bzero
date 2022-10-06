package kubeserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"bastionzero.com/bctl/v1/bctl/daemon/datachannel"
	"bastionzero.com/bctl/v1/bctl/daemon/keysplitting"
	"bastionzero.com/bctl/v1/bctl/daemon/keysplitting/bzcert"
	"bastionzero.com/bctl/v1/bctl/daemon/plugin/kube"
	"bastionzero.com/bctl/v1/bctl/daemon/servers/dataconnection"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	bzplugin "bastionzero.com/bctl/v1/bzerolib/plugin"
	bzkube "bastionzero.com/bctl/v1/bzerolib/plugin/kube"
	kubeutils "bastionzero.com/bctl/v1/bzerolib/plugin/kube/utils"
)

const (
	// This token is used when validating our Bearer token. Our token comes in with the form "{localhostToken}++++{english command i.e. zli kube get pods}++++{logId}"
	// The english command and logId are only generated if the user is using "zli kube ..."
	// So we use this securityTokenDelimiter to split up our token and extract what might be there
	securityTokenDelimiter = "++++"

	connectionCloseTimeout = 10 * time.Second
)

type StatusMessage struct {
	ExitMessage string `json:"ExitMessage"`
}

type KubeServer struct {
	logger *logger.Logger

	conn connection.Connection

	errChan     chan error
	exitMessage string

	// fields for processing incoming kubectl commands
	localhostToken string

	// fields for new connections
	cert     bzcert.IDaemonBZCert
	certPath string
	keyPath  string

	// fields for new datachannels
	targetUser   string
	targetGroups []string
	agentPubKey  string
	localPort    string
	localHost    string
}

func New(
	logger *logger.Logger,
	errChan chan error,
	localPort string,
	localHost string,
	certPath string,
	keyPath string,
	cert bzcert.IDaemonBZCert,
	targetUser string,
	targetGroups []string,
	localhostToken string,
	connUrl string,
	params url.Values,
	headers http.Header,
	agentPubKey string,
) (*KubeServer, error) {

	server := &KubeServer{
		logger:         logger,
		errChan:        errChan,
		exitMessage:    "",
		localhostToken: localhostToken,
		cert:           cert,
		certPath:       certPath,
		keyPath:        keyPath,
		targetUser:     targetUser,
		targetGroups:   targetGroups,
		agentPubKey:    agentPubKey,
		localPort:      localPort,
		localHost:      localHost,
	}

	// Create our one connection in the form of a connection
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

func (k *KubeServer) Start() error {
	// Create HTTP Server listens for incoming kubectl commands
	k.logger.Debugf("starting kube server...")
	go func() {
		// Define our http handlers
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			k.rootCallback(k.logger, w, r)
		})

		http.HandleFunc("/bastionzero-ready", func(w http.ResponseWriter, r *http.Request) {
			k.isReadyCallback(w, r)
		})

		http.HandleFunc("/bastionzero-status", func(w http.ResponseWriter, r *http.Request) {
			k.statusCallback(w, r)
		})

		k.logger.Debugf("listening for connections at %s:%s", k.localHost, k.localHost)
		if err := http.ListenAndServeTLS(k.localHost+":"+k.localPort, k.certPath, k.keyPath, nil); err != nil {
			k.logger.Error(err)
		}
		k.logger.Debugf("successfully began listening")
	}()

	return nil
}

func (k *KubeServer) Close(err error) {
	if k.conn != nil {
		k.conn.Close(err, connectionCloseTimeout)
	}
	k.errChan <- err
}

// TODO: this logic may no longer be necessary, but would require a zli change to remove
func (k *KubeServer) isReadyCallback(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (k *KubeServer) statusCallback(w http.ResponseWriter, r *http.Request) {
	// Build our status message
	statusMessage := StatusMessage{
		ExitMessage: k.exitMessage,
	}

	if registerJson, err := json.Marshal(statusMessage); err != nil {
		k.logger.Errorf("error marshalling status message: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusOK)
		w.Write(registerJson)
	}
}

func (k *KubeServer) listenForConnectionDone() {
	// blocks until the underlying tomb is dead
	<-k.conn.Done()
	k.Close(k.conn.Err())
}

// for creating new datachannels
func (k *KubeServer) newDataChannel(dcId string, action string, plugin *kube.KubeDaemonPlugin, writer http.ResponseWriter) error {
	subLogger := k.logger.GetDatachannelLogger(dcId)

	k.logger.Infof("Creating new datachannel id: %s", dcId)

	// Build the actionParams to send to the datachannel to start the plugin
	synPayload := bzkube.KubeActionParams{
		TargetUser:   k.targetUser,
		TargetGroups: k.targetGroups,
	}

	ksLogger := k.logger.GetComponentLogger("mrzap")
	keysplitter, err := keysplitting.New(ksLogger, k.agentPubKey, k.cert)
	if err != nil {
		return err
	}

	action = "kube/" + action
	attach := false
	_, err = datachannel.New(subLogger, dcId, k.conn, keysplitter, plugin, action, synPayload, attach, true)

	if err != nil {
		return err
	}
	return nil
}

func (k *KubeServer) bubbleUpError(w http.ResponseWriter, msg string, statusCode int) {
	w.WriteHeader(statusCode)
	k.logger.Error(errors.New(msg))
	w.Write([]byte(msg))
}

func (k *KubeServer) rootCallback(logger *logger.Logger, w http.ResponseWriter, r *http.Request) {
	k.logger.Infof("Handling %s - %s\n", r.URL.Path, r.Method)

	// First verify our token and extract any commands if we can
	tokenToValidate := r.Header.Get("Authorization")

	// Remove the `Bearer `
	tokenToValidate = strings.Replace(tokenToValidate, "Bearer ", "", -1)

	// Validate the token
	tokensSplit := strings.Split(tokenToValidate, securityTokenDelimiter)
	if tokensSplit[0] != k.localhostToken {
		k.bubbleUpError(w, "localhost token did not validate. Ensure you are using the right Kube config file", http.StatusInternalServerError)
		return
	}

	// Check if we have a command to extract
	command := "N/A" // TODO: should be empty string
	logId := uuid.New().String()
	if len(tokensSplit) == 3 {
		command = tokensSplit[1]
		logId = tokensSplit[2]
	}

	// Determine the action
	action := getAction(r)

	// start up our plugin
	// every datachannel gets a uuid to distinguish it so a single connection can map to multiple datachannels
	dcId := uuid.New().String()

	pluginLogger := logger.GetPluginLogger(bzplugin.Kube)
	pluginLogger = pluginLogger.GetDatachannelLogger(dcId)
	plugin := kube.New(pluginLogger, k.targetUser, k.targetGroups)

	if err := k.newDataChannel(dcId, string(action), plugin, w); err != nil {
		k.logger.Error(err)
	}

	if err := plugin.StartAction(action, logId, command, w, r); err != nil {
		logger.Errorf("error starting action: %s", err)
	}
}

func getAction(req *http.Request) bzkube.KubeAction {
	// parse action from incoming request
	switch {
	// interactive commands that require both stdin and stdout
	case isExecRequest(req):
		return bzkube.Exec

	// Persistent, yet not interactive commands that serve continual output but only listen for a single, request-cancelling input
	case isPortForwardRequest(req):
		return bzkube.PortForward
	case isStreamRequest(req):
		return bzkube.Stream

	// simple call and response aka restapi requests
	default:
		return bzkube.RestApi
	}
}

func isPortForwardRequest(request *http.Request) bool {
	return strings.HasSuffix(request.URL.Path, "/portforward")
}

func isExecRequest(request *http.Request) bool {
	return strings.HasSuffix(request.URL.Path, "/exec") || strings.HasSuffix(request.URL.Path, "/attach")
}

func isStreamRequest(request *http.Request) bool {
	return (strings.HasSuffix(request.URL.Path, "/log") && kubeutils.IsQueryParamPresent(request, "follow")) || kubeutils.IsQueryParamPresent(request, "watch")
}
