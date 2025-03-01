package controlchannel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"bastionzero.com/agent/agenttype"
	"bastionzero.com/agent/bastion"
	"bastionzero.com/agent/bastion/agentidentity"
	"bastionzero.com/agent/bastion/report"
	"bastionzero.com/agent/config/keyshardconfig/data"
	"bastionzero.com/agent/controlchannel/dataconnection"
	"bastionzero.com/agent/mrtap"
	"bastionzero.com/agent/plugin/db/actions/pwdb"
	"bastionzero.com/bzerolib/connection"
	am "bastionzero.com/bzerolib/connection/agentmessage"
	"bastionzero.com/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bzerolib/keypair"
	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/mrtap/bzcert"
	"bastionzero.com/bzerolib/mrtap/util"

	"gopkg.in/tomb.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	HeartRate    = 1 * time.Minute
	closeTimeout = 10 * time.Second

	ManualRestartMsg = "received manual restart from subject"
)

type AgentDatachannelConnection interface {
	connection.Connection
	NumDataChannels() int
}

type ControlChannelConfig interface {
	mrtap.MrtapConfig
	SetServiceAccountJwksUrl(jwksUrlPattern string) error
}

type KeyShardConfig interface {
	pwdb.PWDBConfig
	AddKey(newEntry data.MappedKeyEntry) error
}

type ControlChannel struct {
	conn      connection.Connection
	logger    *logger.Logger
	tmb       tomb.Tomb
	channelId string

	ccConfig       ControlChannelConfig
	keyShardConfig KeyShardConfig

	// agent attributes
	agentType    agenttype.AgentType
	agentIdToken agentidentity.AgentIdentityToken
	privateKey   *keypair.PrivateKey

	// accepts input from a connection
	inputChan chan am.AgentMessage

	// regularly notifies the agent we are still functioning
	agentPongChan chan bool

	// notifies the agent of significant-but-non-fatal runtime errors
	runtimeErrChan chan error

	// struct for keeping track of all connections key'ed with connectionId (connections with associated datachannels)
	connections     map[string]AgentDatachannelConnection
	connectionsLock sync.Mutex

	// helps with race, see ShouldBeSendingPongs() for more information
	isSendingPongs bool

	// keeps track of the last fetch of cluster users we did, we update if changes on new fetch are detected
	clusterUserCache []string

	// for communicating with the bastion
	bastionClient bastion.ApiClient

	logFilePath string
}

func Start(logger *logger.Logger,
	bastion bastion.ApiClient,
	id string,
	conn connection.Connection, // control channel connection
	agentType agenttype.AgentType,
	agentIdToken agentidentity.AgentIdentityToken,
	privateKey *keypair.PrivateKey,
	cConfig ControlChannelConfig,
	keyShardConfig KeyShardConfig,
	logFilePath string,
) (*ControlChannel, error) {

	control := &ControlChannel{
		conn:             conn,
		logger:           logger,
		channelId:        id,
		bastionClient:    bastion,
		agentType:        agentType,
		agentIdToken:     agentIdToken,
		privateKey:       privateKey,
		ccConfig:         cConfig,
		keyShardConfig:   keyShardConfig,
		inputChan:        make(chan am.AgentMessage, 25),
		connections:      make(map[string]AgentDatachannelConnection),
		agentPongChan:    make(chan bool),
		runtimeErrChan:   make(chan error),
		isSendingPongs:   conn.Ready(),
		clusterUserCache: []string{},
		logFilePath:      logFilePath,
	}

	// Since the CC has its own websocket and Bastion doesn't know what it is, there's no point
	// subscribing to the connection with a unique id. As long as we use the same one when we unsubscribe
	conn.Subscribe("", control)

	// initialize our user key storage
	var err error
	if err != nil {
		return nil, fmt.Errorf("failed to setup user key storage: %s", err)
	}

	// Set up our handler to deal with incoming messages
	control.tmb.Go(func() error {
		// send healthcheck messages at every "heartbeat"
		control.tmb.Go(func() error {
			ticker := time.NewTicker(HeartRate)
			defer ticker.Stop()
			for {
				select {
				case <-control.tmb.Dying():
					logger.Info("Ceasing heartbeats")
					return nil
				case <-ticker.C:
					// only try to send heartbeats if we're connected
					if conn.Ready() {
						if err := control.reportHealth(); err != nil {
							control.logger.Errorf("error reporting health: %s", err)
						}
						// there's actually no reason to set control.isSendingPongs to true here.
						// All that would do would be to create a race condition between when we send the
						// healthcheck and when we receive the reply
					} else {
						// if we're disconnected and not sending healthchecks, we must immediately change state
						// so that the agent knows not to expect any pongs
						control.isSendingPongs = false
					}
				}
			}
		})

		// Make a context and tie it in with our tomb to use for processing control messages
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			select {
			case <-ctx.Done():
			case <-control.tmb.Dying():
				cancel()
			}
		}()

		for {
			select {
			case <-control.tmb.Dying():
				// We need to close all open client connections if the control channel has been closed
				logger.Info("Closing all agent connections since control channel has been closed")

				for _, conn := range control.connections {
					// Then close the connection
					conn.Close(control.tmb.Err(), 10*time.Second)
				}
				return nil
			case agentMessage := <-control.inputChan:
				// Process each message in its own thread
				// TODO: (CWC-2038) it would be safer to put a limit on how many threads can be created this way
				go func() {
					if err := control.processInput(agentMessage, ctx); err != nil {
						logger.Error(err)
						control.runtimeErrChan <- err
					}
				}()
			case <-conn.Done():
				return fmt.Errorf("connection closed with err: %s", conn.Err())
			}
		}
	})

	return control, nil
}

func (c *ControlChannel) Close(reason error) {
	if c.tmb.Alive() {
		c.logger.Infof("Control channel closing because: %s", reason)

		c.tmb.Kill(reason)

		// we need to provide a guarantee that this closes even if websockets / plugins are stuck
		select {
		case <-c.tmb.Dead():
		case <-time.After(closeTimeout):
			c.logger.Infof("Timed out after %s waiting for connection to close", closeTimeout.String())
		}
	} else {
		c.logger.Infof("Close was called while in a dying state")
	}
}

func (c *ControlChannel) Receive(agentMessage am.AgentMessage) {
	c.inputChan <- agentMessage
}

func (c *ControlChannel) Done() <-chan struct{} {
	return c.tmb.Dead()
}

func (c *ControlChannel) Err() error {
	return c.tmb.Err()
}

// used to alert anyone who's listening that we encountered a significant-but-not-fatal error
func (c *ControlChannel) RuntimeErr() <-chan error {
	return c.runtimeErrChan
}

// used to alert anyone who's listening that we're still alive
func (c *ControlChannel) Pong() <-chan bool {
	return c.agentPongChan
}

// the purpose of this method is to prevent a race between the CC reconnecting and the agent
// missing a pong from the CC. The mechanism described here should guarnatee that following a reconnect,
// the agent gives the CC a grace period of at least one full set of missed pongs. Without this mechanism,
// the agent might restart in the time between the CC reconnecting and receiving the next healthcheck pong from bastion
func (c *ControlChannel) ShouldBeSendingPongs() bool {
	readyToSendPongs := c.conn.Ready()

	// no matter what, update our state based on the connection's status
	defer func() { c.isSendingPongs = readyToSendPongs }()

	// if we were disconnected the last time the agent checked but have since reconnected, we want to give ourselves time
	// for the agent to hear from us before it finds out the connection is ready again. Therefore, in this special case,
	// we ask it to wait for one more set of pongs
	if readyToSendPongs && !c.isSendingPongs {
		return false
	} else {
		// if we're disconnected, or have been connected for two consecutive calls, return the status as is
		return readyToSendPongs
	}
}

// Wraps and sends the payload
func (c *ControlChannel) send(messageType am.MessageType, messagePayload interface{}) error {
	c.logger.Debugf("control channel is sending %s message", messageType)
	if messageBytes, err := json.Marshal(messagePayload); err != nil {
		return fmt.Errorf("failed to marshal %s message payload: %s", messageType, err)
	} else {
		agentMessage := am.AgentMessage{
			ChannelId:      c.channelId,
			MessageType:    messageType,
			SchemaVersion:  am.CurrentVersion,
			MessagePayload: messageBytes,
		}

		// Push message to websocket channel output
		c.conn.Send(agentMessage)
		return nil
	}
}

// This is our main process function where incoming messages from the connection will be processed
func (c *ControlChannel) processInput(agentMessage am.AgentMessage, ctx context.Context) error {
	c.logger.Debugf("control channel received %s message", am.MessageType(agentMessage.MessageType))

	switch am.MessageType(agentMessage.MessageType) {
	case am.HealthCheck:
		// congratulations, we're still functioning and can tell the agent we're alive
		c.agentPongChan <- true
	case am.Restart:
		var restartRequest RestartAgentMessage
		if err := json.Unmarshal(agentMessage.MessagePayload, &restartRequest); err != nil {
			c.logger.Errorf("malformed restart agent request: %s", err)
			restartRequest = RestartAgentMessage{}
		}
		c.Close(fmt.Errorf("%s: %+v", ManualRestartMsg, restartRequest))
	case am.RetrieveLogs:
		var retrieveLogsRequest RetrieveAgentLogsMessage
		if err := json.Unmarshal(agentMessage.MessagePayload, &retrieveLogsRequest); err != nil {
			return fmt.Errorf("malformed retrieve agent logs request: %s", err)
		}

		c.logger.Infof("Retrieving logs")
		if err := report.ReportLogs(ctx, c.bastionClient, c.agentType, retrieveLogsRequest.UserEmail, retrieveLogsRequest.UploadLogsRequestId, c.logFilePath); err != nil {
			return fmt.Errorf("failed to send agent logs to Bastion: %s", err)
		} else {
			c.logger.Infof("Successfully sent agent logs to Bastion")
		}
	case am.Configure:
		var configureReq ConfigureServiceAccountMessage
		if err := json.Unmarshal(agentMessage.MessagePayload, &configureReq); err != nil {
			return fmt.Errorf("malformed configure agent request: %s", err)
		}
		if err := c.configureServiceAccount(configureReq.BZCert, configureReq.Signature, configureReq.ServiceAccountConfiguration); err != nil {
			return fmt.Errorf("error while configuring agent with jwksUrlPattern %s : %s", configureReq.ServiceAccountConfiguration.JwksUrlPattern, err)
		}
	case am.OpenWebsocket:
		var owRequest OpenWebsocketMessage
		if err := json.Unmarshal(agentMessage.MessagePayload, &owRequest); err != nil {
			return fmt.Errorf("malformed open websocket request: %s", err)
		}
		return c.openWebsocket(owRequest.ConnectionId, owRequest.ConnectionServiceUrl)
	case am.CloseWebsocket:
		var cwRequest CloseWebsocketMessage
		if err := json.Unmarshal(agentMessage.MessagePayload, &cwRequest); err != nil {
			return fmt.Errorf("malformed close websocket request")
		} else {
			if conn, ok := c.getConnectionMap(cwRequest.ConnectionId); ok {
				c.logger.Infof("Closing connection with id %s", cwRequest.ConnectionId)
				conn.Close(fmt.Errorf("connection was closed through the control channel with reason: %s", cwRequest.Reason), 10*time.Second)
			} else {
				return fmt.Errorf("could not close non existent connection with id: %s", cwRequest.ConnectionId)
			}
		}
	case am.KeyShard:
		var ksRequest KeyShardMessage
		if err := json.Unmarshal(agentMessage.MessagePayload, &ksRequest); err != nil {
			return fmt.Errorf("malformed distribute shard request: %s", err)
		}

		if err := c.keyShardConfig.AddKey(data.MappedKeyEntry{
			KeyData:   ksRequest.KeyShard,
			TargetIds: ksRequest.TargetIds,
		}); err != nil {
			return fmt.Errorf("failed to add key shard to config: %s", err)
		}

		c.logger.Infof("successfully stored key shard for targets %s", strings.Join(ksRequest.TargetIds, ", "))
	default:
		return fmt.Errorf("unrecognized message type: %s", agentMessage.MessageType)
	}

	return nil
}

func (c *ControlChannel) openWebsocket(connectionId, connectionServiceUrl string) error {
	subLogger := c.logger.GetConnectionLogger(connectionId)

	wsLogger := subLogger.GetComponentLogger("Websocket")
	srLogger := subLogger.GetComponentLogger("SignalR")

	client := signalr.New(srLogger, websocket.New(wsLogger))
	headers := http.Header{}
	params := url.Values{}
	if conn, err := dataconnection.New(
		subLogger,
		c.bastionClient,
		connectionServiceUrl,
		connectionId,
		c.ccConfig,
		c.keyShardConfig,
		c.agentIdToken,
		c.privateKey,
		params,
		headers,
		client,
	); err != nil {
		return fmt.Errorf("could not create new connection: %s", err)
	} else {
		// add the connection to our connections dictionary
		c.logger.Infof("Created connection with id: %s", connectionId)
		c.updateConnectionsMap(connectionId, conn)

		// wait for this connection to close and then delete it from the map
		<-conn.Done()
		c.logger.Infof("Connection %s closed: %s", connectionId, conn.Err())
		c.deleteConnectionsMap(connectionId)
	}
	return nil
}

func (c *ControlChannel) configureServiceAccount(bzcert bzcert.BZCert, signature string, saConfiguration ServiceAccountConfiguration) (err error) {
	// Verify the BZCert
	if err := bzcert.Verify(c.ccConfig.GetIdpProvider(), c.ccConfig.GetIdpOrgId(), c.ccConfig.GetServiceAccountJwksUrls()); err != nil {
		return fmt.Errorf("failed to verify configure's BZCert: %w", err)
	}

	// Verify the signature
	var pubkey *keypair.PublicKey
	if pubkey, err = keypair.PublicKeyFromString(bzcert.ClientPublicKey); err != nil {
		return fmt.Errorf("malformed public key: %s", bzcert.ClientPublicKey)
	}

	// Verify the signature
	var hashBits []byte
	var ok bool
	if hashBits, ok = util.HashPayload(saConfiguration); !ok {
		return fmt.Errorf("failed to hash the mrtap payload")
	}
	if ok := pubkey.Verify(hashBits, signature); !ok {
		return fmt.Errorf("invalid signature for payload: %+v", saConfiguration)
	}

	// Configure the agent
	jwksUrlPattern := saConfiguration.JwksUrlPattern
	if err := c.ccConfig.SetServiceAccountJwksUrl(jwksUrlPattern); err != nil {
		return fmt.Errorf("error adding new jwksUrlPattern to the config: %s", err)
	}
	c.logger.Infof("Successfully configured this agent to allow access to service accounts originating from JWKS URLs following the %s pattern", jwksUrlPattern)
	return nil
}

func (c *ControlChannel) reportHealth() error {
	// Build heartbeat message
	numDataChannels := 0
	for _, conn := range c.connections {
		numDataChannels += conn.NumDataChannels()
	}

	heartbeatMessage := HeartbeatMessage{
		Alive:           true,
		NumDataChannels: uint32(numDataChannels),
	}

	err := c.send(am.HealthCheck, heartbeatMessage)
	if err != nil {
		return err
	}

	// Let bastion know a list of valid cluster users if they have changed
	if c.agentType == agenttype.Kubernetes {
		if err := c.reportClusterUsers(); err != nil {
			c.logger.Errorf("failed to report valid cluster users: %s", err)
		}
	}

	return nil
}

func (c *ControlChannel) reportClusterUsers() error {
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	// Then get all cluster roles
	clusterRoleBindings, err := clientset.RbacV1().ClusterRoleBindings().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	clusterUsers := make(map[string]bool)

	for _, clusterRoleBinding := range clusterRoleBindings.Items {
		// Now loop over the subjects if we can find any user subjects
		for _, subject := range clusterRoleBinding.Subjects {
			if subject.Kind == "User" {
				// We do not consider any system:... or eks:..., basically any system: looking roles as valid. This can be overridden from Bastion
				var systemRegexPatten = regexp.MustCompile(`[a-zA-Z0-9]*:[a-za-zA-Z0-9-]*`)
				if !systemRegexPatten.MatchString(subject.Name) {
					clusterUsers[subject.Name] = true
				}
			}
		}
	}

	// Then get all roles
	roleBindings, err := clientset.RbacV1().RoleBindings("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, roleBindings := range roleBindings.Items {
		// Now loop over the subjects if we can find any user subjects
		for _, subject := range roleBindings.Subjects {
			if subject.Kind == "User" {
				// We do not consider any system:... or eks:..., basically any system: looking roles as valid. This can be overridden from Bastion
				var systemRegexPatten = regexp.MustCompile(`[a-zA-Z0-9]*:[a-za-zA-Z0-9-]*`) // TODO: double check
				if !systemRegexPatten.MatchString(subject.Name) {
					clusterUsers[subject.Name] = true
				}
			}
		}
	}

	// Now build our response
	users := []string{}
	for key := range clusterUsers {
		users = append(users, key)
	}
	sort.Strings(users)

	// If the set of valid users are different from the last time we checked
	// then send an update message
	if !reflect.DeepEqual(users, c.clusterUserCache) {
		c.logger.Info("sending updated valid cluster users in the control channel.")
		msg := ClusterUsersMessage{
			ClusterUsers: users,
		}
		c.send(am.ClusterUsers, msg)

		// update the cached valid cluster users
		c.clusterUserCache = users
	}

	return nil
}

// Helper function so we avoid writing to this map at the same time
func (c *ControlChannel) updateConnectionsMap(id string, newConn AgentDatachannelConnection) {
	c.connectionsLock.Lock()
	c.connections[id] = newConn
	c.connectionsLock.Unlock()
}

func (c *ControlChannel) deleteConnectionsMap(id string) {
	c.connectionsLock.Lock()
	delete(c.connections, id)
	c.connectionsLock.Unlock()
}

func (c *ControlChannel) getConnectionMap(id string) (AgentDatachannelConnection, bool) {
	c.connectionsLock.Lock()
	defer c.connectionsLock.Unlock()
	meta, ok := c.connections[id]
	return meta, ok
}
