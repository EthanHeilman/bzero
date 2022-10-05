package controlchannel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"sync"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/controlchannel/agentidentity"
	"bastionzero.com/bctl/v1/bctl/agent/controlchannel/dataconnection"
	"bastionzero.com/bctl/v1/bctl/agent/keysplitting"
	"bastionzero.com/bctl/v1/bctl/agent/vault"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/messagesigner"
	"bastionzero.com/bctl/v1/bzerolib/telemetry"

	"gopkg.in/tomb.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	HeartRate    = 1 * time.Minute
	closeTimeout = 10 * time.Second

	ManualRestartMsg = "received manual restart from user"
)

type ControlChannel struct {
	conn   connection.Connection
	logger *logger.Logger
	tmb    tomb.Tomb
	id     string

	ksConfig              keysplitting.IKeysplittingConfig
	agentIdentityProvider agentidentity.IAgentIdentityProvider
	messageSigner         messagesigner.IMessageSigner

	// variables for opening connections
	serviceUrl string

	// accepts input from a connection
	inputChan chan am.AgentMessage

	// regularly notifies the agent we are still functioning
	agentPongChan chan bool

	// notifies the agent of significant-but-non-fatal runtime errors
	runtimeErrChan chan error

	targetType string

	// struct for keeping track of all connections key'ed with connectionId (connections with associated datachannels)
	connections     map[string]connection.Connection
	connectionsLock sync.Mutex

	SocketLock sync.Mutex // Ref: https://github.com/gorilla/websocket/issues/119#issuecomment-198710015

	isSendingPongs bool

	ClusterUserCache []string

	// used for collecting stats and sending in heartbeats
	connectionsOpened int
	connectionsClosed int
	start             time.Time
}

func Start(logger *logger.Logger,
	id string,
	conn connection.Connection, // control channel connection
	serviceUrl string,
	targetType string,
	agentIdentityProvider agentidentity.IAgentIdentityProvider,
	messageSigner messagesigner.IMessageSigner,
	ksConfig keysplitting.IKeysplittingConfig,
) (*ControlChannel, error) {

	control := &ControlChannel{
		conn:                  conn,
		logger:                logger,
		id:                    id,
		serviceUrl:            serviceUrl,
		targetType:            targetType,
		agentIdentityProvider: agentIdentityProvider,
		messageSigner:         messageSigner,
		ksConfig:              ksConfig,
		inputChan:             make(chan am.AgentMessage, 25),
		connections:           make(map[string]connection.Connection),
		agentPongChan:         make(chan bool),
		runtimeErrChan:        make(chan error),
		isSendingPongs:        conn.Ready(),
		ClusterUserCache:      []string{},
		start:                 time.Now(),
	}

	// Since the CC has its own websocket and Bastion doesn't know what it is, there's no point
	// subscribing to the connection with a unique id. As long as we use the same one when we unsubscribe
	conn.Subscribe("", control)

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
					if err := control.processInput(agentMessage); err != nil {
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
			ChannelId:      c.id,
			MessageType:    string(messageType),
			SchemaVersion:  am.CurrentVersion,
			MessagePayload: messageBytes,
		}

		// Push message to websocket channel output
		c.conn.Send(agentMessage)
		return nil
	}
}

func (c *ControlChannel) openWebsocket(message OpenWebsocketMessage) error {
	subLogger := c.logger.GetConnectionLogger(message.ConnectionId)

	wsLogger := subLogger.GetComponentLogger("Websocket")
	srLogger := subLogger.GetComponentLogger("SignalR")

	client := signalr.New(srLogger, websocket.New(wsLogger))
	headers := http.Header{}
	params := url.Values{}
	if conn, err := dataconnection.New(subLogger, message.ConnectionServiceUrl, message.ConnectionId, c.ksConfig, c.agentIdentityProvider, c.messageSigner, params, headers, client); err != nil {
		return fmt.Errorf("could not create new connection: %s", err)
	} else {
		c.connectionsOpened++

		// add the connection to our connections dictionary
		c.logger.Infof("Created connection with id: %s", message.ConnectionId)
		c.updateConnectionsMap(message.ConnectionId, conn)

		// wait for this connection to close and then delete it from the map
		<-conn.Done()
		c.logger.Infof("Connection %s closed: %s.", message.ConnectionId, conn.Err())
		c.deleteConnectionsMap(message.ConnectionId)
	}
	return nil
}

// This is our main process function where incoming messages from the connection will be processed
func (c *ControlChannel) processInput(agentMessage am.AgentMessage) error {
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
	case am.OpenWebsocket:
		var owRequest OpenWebsocketMessage
		if err := json.Unmarshal(agentMessage.MessagePayload, &owRequest); err != nil {
			return fmt.Errorf("malformed open websocket request: %s", err)
		} else {
			return c.openWebsocket(owRequest)
		}
	case am.CloseWebsocket:
		var cwRequest CloseWebsocketMessage
		if err := json.Unmarshal(agentMessage.MessagePayload, &cwRequest); err != nil {
			return fmt.Errorf("malformed close websocket request")
		} else {
			if conn, ok := c.getConnectionMap(cwRequest.ConnectionId); ok {
				c.connectionsClosed--
				c.logger.Infof("Closing connection with id %s", cwRequest.ConnectionId)
				conn.Close(fmt.Errorf("connection was closed through the control channel with reason: %s", cwRequest.Reason), 10*time.Second)
			} else {
				return fmt.Errorf("could not close non existent connection with id: %s", cwRequest.ConnectionId)
			}
		}
	default:
		return fmt.Errorf("unrecognized message type: %s", agentMessage.MessageType)
	}

	return nil
}

func (c *ControlChannel) reportHealth() error {
	connStats := map[string]json.RawMessage{}
	for _, conn := range c.connections {
		connStats[conn.Id()] = conn.Stats()
	}
	jsonConnStats, _ := json.Marshal(connStats)

	// Read our current memory statistics
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	pstats := telemetry.ProcessStats{
		Alloc:         mem.Alloc,
		TotalAlloc:    mem.TotalAlloc,
		Sys:           mem.Sys,
		Mallocs:       mem.Mallocs,
		Frees:         mem.Frees,
		LiveObjects:   mem.Mallocs - mem.Frees,
		NumGoRoutines: runtime.NumGoroutine(),
	}
	jsonProcessStat, _ := json.Marshal(pstats)

	heartbeatMessage := HeartbeatMessage{
		Alive:             true,
		ProcessStats:      jsonProcessStat,
		ConnectionsOpened: c.connectionsOpened,
		ConnectionsClosed: c.connectionsClosed,
		NumConnections:    len(c.connections),
		ConnectionsStats:  jsonConnStats,
	}

	err := c.send(am.HealthCheck, heartbeatMessage)
	if err != nil {
		return err
	}

	// Let bastion know a list of valid cluster users if they have changed
	if vault.InCluster() {
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
	if !reflect.DeepEqual(users, c.ClusterUserCache) {
		c.logger.Info("sending updated valid cluster users in the control channel.")
		msg := ClusterUsersMessage{
			ClusterUsers: users,
		}
		c.send(am.ClusterUsers, msg)

		// update the cached valid cluster users
		c.ClusterUserCache = users
	}

	return nil
}

// Helper function so we avoid writing to this map at the same time
func (c *ControlChannel) updateConnectionsMap(id string, newConn connection.Connection) {
	c.connectionsLock.Lock()
	c.connections[id] = newConn
	c.connectionsLock.Unlock()
}

func (c *ControlChannel) deleteConnectionsMap(id string) {
	c.connectionsLock.Lock()
	delete(c.connections, id)
	c.connectionsLock.Unlock()
}

func (c *ControlChannel) getConnectionMap(id string) (connection.Connection, bool) {
	c.connectionsLock.Lock()
	defer c.connectionsLock.Unlock()
	meta, ok := c.connections[id]
	return meta, ok
}
