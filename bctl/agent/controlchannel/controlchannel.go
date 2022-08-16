package controlchannel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/controlchannel/agentdatachannelconnection"
	"bastionzero.com/bctl/v1/bctl/agent/datachannel"
	"bastionzero.com/bctl/v1/bctl/agent/keysplitting"
	"bastionzero.com/bctl/v1/bctl/agent/vault"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/broker"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bctl/v1/bzerolib/logger"

	"gopkg.in/tomb.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	heartRate = 20 * time.Second
)

type connectionMeta struct {
	Client       connection.Connection
	DataChannels map[string]broker.IChannel
}

type ControlChannel struct {
	conn   connection.Connection
	logger *logger.Logger
	tmb    tomb.Tomb
	id     string

	// config values needed for keysplitting
	ksConfig keysplitting.IKeysplittingConfig

	// variables for opening connections
	serviceUrl string

	// These are all the types of channels we have available
	inputChan chan am.AgentMessage

	targetType string

	// struct for keeping track of all connections key'ed with connectionId (connections with associated datachannels)
	connections     map[string]connectionMeta
	connectionsLock sync.Mutex

	SocketLock sync.Mutex // Ref: https://github.com/gorilla/websocket/issues/119#issuecomment-198710015
}

func Start(logger *logger.Logger,
	id string,
	conn connection.Connection, // control channel connection
	serviceUrl string,
	targetType string,
	ksConfig keysplitting.IKeysplittingConfig) (*ControlChannel, error) {

	control := &ControlChannel{
		conn:        conn,
		logger:      logger,
		id:          id,
		ksConfig:    ksConfig,
		serviceUrl:  serviceUrl,
		targetType:  targetType,
		inputChan:   make(chan am.AgentMessage, 25),
		connections: make(map[string]connectionMeta),
	}

	// The ChannelId is mostly for distinguishing multiple channels over a single connection but the control channel has
	// its own dedicated connection.  This also makes it so there can only ever be one control channel associated with a
	// given connection at any time.
	// TODO: figure out a way to let control channel know its own id before it subscribes
	conn.Subscribe("", control)

	// Set up our handler to deal with incoming messages
	control.tmb.Go(func() error {

		// send healthcheck messages at every "heartbeat"
		control.tmb.Go(func() error {
			ticker := time.NewTicker(heartRate)
			defer ticker.Stop()
			for {
				select {
				case <-control.tmb.Dying():
					logger.Info("Ceasing heartbeats")
					return nil
				case <-ticker.C:
					// don't bother trying to send heartbeats if we're not connected
					if conn.Ready() {
						if msg, err := control.checkHealth(); err != nil {
							control.logger.Errorf("error creating healthcheck message: %s", err)
						} else {
							control.send(am.HealthCheck, msg)
						}
					}
				}
			}
		})

		for {
			select {
			case <-control.tmb.Dying():
				// We need to close all open client connections if the control channel has been closed
				logger.Info("Closing all agent connections since control channel has been closed")

				for _, connMeta := range control.connections {
					// First send a close message over the agent connection
					connMeta.Client.Send(am.AgentMessage{
						MessageType:    string(am.CloseDaemonWebsocket),
						MessagePayload: []byte{},
						SchemaVersion:  am.CurrentVersion,
						ChannelId:      "-1", // Channel Id does not since this applies to all datachannels
					})

					// Then close the connection
					connMeta.Client.Close(fmt.Errorf("control channel has been closed"))
				}
				return nil
			case agentMessage := <-control.inputChan:
				// Process each message in its own thread
				go func() {
					if err := control.processInput(agentMessage); err != nil {
						logger.Error(err)
					}
				}()
			}
		}
	})

	return control, nil
}

func (c *ControlChannel) Close(reason error) {
	c.tmb.Kill(reason)
	c.tmb.Wait()
}

func (c *ControlChannel) Receive(agentMessage am.AgentMessage) {
	c.inputChan <- agentMessage
}

// Wraps and sends the payload
func (c *ControlChannel) send(messageType am.MessageType, messagePayload interface{}) error {
	c.logger.Debugf("control channel is sending %s message", messageType)
	messageBytes, _ := json.Marshal(messagePayload)
	agentMessage := am.AgentMessage{
		ChannelId:      c.id,
		MessageType:    string(messageType),
		SchemaVersion:  am.CurrentVersion,
		MessagePayload: messageBytes,
	}

	// Push message to connection channel output
	c.conn.Send(agentMessage)
	return nil
}

func (c *ControlChannel) openWebsocket(message OpenWebsocketMessage) error {
	subLogger := c.logger.GetConnectionLogger(message.ConnectionId)

	headers := http.Header{}
	params := url.Values{
		"connection_id":  {message.ConnectionId},
		"token":          {message.Token},
		"connectionType": {message.Type},
	}

	wsLogger := c.logger.GetComponentLogger("Websocket")
	if conn, err := agentdatachannelconnection.New(subLogger, message.ConnectionServiceUrl, params, headers, websocket.New(wsLogger)); err != nil {
		return fmt.Errorf("could not create new connection: %s", err)
	} else {
		// add the connection to our connections dictionary
		c.logger.Infof("Created connection with id: %s", message.ConnectionId)
		meta := connectionMeta{
			Client:       conn,
			DataChannels: make(map[string]broker.IChannel),
		}
		c.updateConnectionsMap(message.ConnectionId, meta)
	}
	return nil
}

func (c *ControlChannel) openDataChannel(message OpenDataChannelMessage) error {
	connectionId := message.ConnectionId
	dcId := message.DataChannelId
	subLogger := c.logger.GetDatachannelLogger(dcId)
	ksSubLogger := c.logger.GetComponentLogger("mrzap")

	// grab the connection
	if connMeta, ok := c.getConnectionMap(connectionId); !ok {
		return fmt.Errorf("agent does not have a connection associated with id %s", connectionId)
	} else if keysplitter, err := keysplitting.New(ksSubLogger, c.ksConfig); err != nil {
		return err
	} else if datachannel, err := datachannel.New(&c.tmb, subLogger, connMeta.Client, keysplitter, dcId, message.Syn); err != nil {
		return err
	} else {
		// add our new datachannel to our connections dictionary
		connMeta.DataChannels[dcId] = datachannel
		return nil
	}
}

// This is our main process function where incoming messages from the connection will be processed
func (c *ControlChannel) processInput(agentMessage am.AgentMessage) error {
	c.logger.Debugf("control channel received %v message", am.MessageType(agentMessage.MessageType))

	switch am.MessageType(agentMessage.MessageType) {
	case am.HealthCheck:
		c.logger.Debugf("as of version 4.2.0 this agent no longer accepts healthcheck messages; ignoring")
		return nil
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
				// this can take a little time, but we don't want it blocking other things
				go func() {
					c.logger.Infof("Closing connection with id %s", cwRequest.ConnectionId)
					conn.Client.Close(errors.New("connection closed on daemon"))
					c.deleteConnectionsMap(cwRequest.ConnectionId)
				}()
			} else {
				return fmt.Errorf("could not close non existent connection with id: %s", cwRequest.ConnectionId)
			}
		}
	case am.OpenDataChannel:
		var odRequest OpenDataChannelMessage
		if err := json.Unmarshal(agentMessage.MessagePayload, &odRequest); err != nil {
			return fmt.Errorf("malformed open datachannel request: %s", err)
		} else {
			if err := c.openDataChannel(odRequest); err != nil {
				return fmt.Errorf("error creating datachannel: %s", err)
			}
		}
	case am.CloseDataChannel:
		var cdRequest CloseDataChannelMessage
		if err := json.Unmarshal(agentMessage.MessagePayload, &cdRequest); err != nil {
			return fmt.Errorf("malformed close datachannel request: %s", err)
		} else {
			if conn, ok := c.getConnectionMap(cdRequest.ConnectionId); ok {
				if datachannel, ok := conn.DataChannels[cdRequest.DataChannelId]; ok {
					datachannel.Close(errors.New("formal datachannel request received"))
					delete(conn.DataChannels, cdRequest.DataChannelId)
				} else {
					return fmt.Errorf("agent does not have a datachannel with id: %s", cdRequest.DataChannelId)
				}
			} else {
				return fmt.Errorf("agent does not have a datachannel with id: %s", cdRequest.DataChannelId)
			}
		}
	default:
		return fmt.Errorf("unrecognized message type: %s", agentMessage.MessageType)
	}

	return nil
}

func (c *ControlChannel) checkHealth() (AliveCheckAgentToBastionMessage, error) {
	// Let bastion know a list of valid cluster roles
	if vault.InCluster() {
		return checkInClusterHealth()
	}

	return AliveCheckAgentToBastionMessage{
		Alive:        true,
		ClusterUsers: []string{},
	}, nil
}

func checkInClusterHealth() (AliveCheckAgentToBastionMessage, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return AliveCheckAgentToBastionMessage{}, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return AliveCheckAgentToBastionMessage{}, err
	}

	// Then get all cluster roles
	clusterRoleBindings, err := clientset.RbacV1().ClusterRoleBindings().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return AliveCheckAgentToBastionMessage{}, err
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
		return AliveCheckAgentToBastionMessage{}, err
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

	return AliveCheckAgentToBastionMessage{
		Alive:        true,
		ClusterUsers: users,
	}, nil
}

// Helper function so we avoid writing to this map at the same time
func (c *ControlChannel) updateConnectionsMap(id string, newConn connectionMeta) {
	c.connectionsLock.Lock()
	c.connections[id] = newConn
	c.connectionsLock.Unlock()
}

func (c *ControlChannel) deleteConnectionsMap(id string) {
	c.connectionsLock.Lock()
	delete(c.connections, id)
	c.connectionsLock.Unlock()
}

func (c *ControlChannel) getConnectionMap(id string) (connectionMeta, bool) {
	c.connectionsLock.Lock()
	defer c.connectionsLock.Unlock()
	meta, ok := c.connections[id]
	return meta, ok
}
