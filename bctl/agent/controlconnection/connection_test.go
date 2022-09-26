package controlconnection

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/controlchannel/agentidentity"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	"bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/broker"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/messagesigner"
	"bastionzero.com/bctl/v1/bzerolib/tests"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

func TestControlChannelConnection(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agent Control Channel Connection Suite")
}

func setupMockConnectionOrchestrator(getControlChannelEndpointHandler http.HandlerFunc) *tests.MockServer {
	mockConnectionOrchestrator := tests.NewMockServer(tests.MockHandler{
		Endpoint:    controlChannelEndpoint,
		HandlerFunc: getControlChannelEndpointHandler,
	})

	return mockConnectionOrchestrator
}

func mockGetControlChannelHandler(connectionNodeUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		getControlChannelResponse := GetControlChannelResponse{
			ConnectionUrl:    connectionNodeUrl,
			ControlChannelId: uuid.New().String(),
		}
		json.NewEncoder(w).Encode(getControlChannelResponse)
	}
}

func setupMockBastion(getConnectionServiceUrlEndpointHandler http.HandlerFunc) *tests.MockServer {
	mockBastion := tests.NewMockServer(tests.MockHandler{
		Endpoint:    connectionServiceEndpoint,
		HandlerFunc: getConnectionServiceUrlEndpointHandler,
	})

	return mockBastion
}

func mockGetConnectionServiceUrlHandler(connectionOrchestratorUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connectionServiceUrlResponse := GetConnectionServiceResponse{
			ConnectionServiceUrl: connectionOrchestratorUrl,
		}
		json.NewEncoder(w).Encode(connectionServiceUrlResponse)
	}
}

var _ = Describe("Agent Control Channel Connection", Ordered, func() {
	var conn connection.Connection
	var mockClient *messenger.MockMessenger
	var mockBastion *tests.MockServer

	var doneChan chan struct{}
	var inboundChan chan *signalr.SignalRMessage
	var err error

	fakeKeyPair, _ := tests.GenerateEd25519Key()
	fakePrivateKey := fakeKeyPair.Base64EncodedPrivateKey
	fakeMessageSigner, _ := messagesigner.New(fakeKeyPair.PrivateKey)

	logger := logger.MockLogger(GinkgoWriter)
	params := url.Values{
		"public_key": {"publicKey"},
		"version":    {"agentVersion"},
		"target_id":  {"targetId"},
	}
	headers := http.Header{}

	mockAgentIdentityProvider := &agentidentity.MockAgentIdentityProvider{}
	mockAgentIdentityProvider.On("GetToken", mock.Anything).Return("fake-agent-identity-token", nil)

	setupHappyClient := func() {
		doneChan = make(chan struct{})
		inboundChan = make(chan *signalr.SignalRMessage, 1)

		mockClient = &messenger.MockMessenger{}

		mockClient.On("Connect").Return(nil)
		mockClient.On("Send").Return(nil)
		mockClient.On("Close").Return()
		mockClient.On("Done").Return(doneChan)
		mockClient.On("Inbound").Return(inboundChan)
	}

	setupHappyServers := func() {
		mockConnectionNode := tests.NewMockServer()
		mockConnectionOrchestrator := setupMockConnectionOrchestrator(mockGetConnectionServiceUrlHandler(mockConnectionNode.Url))
		mockBastion = setupMockBastion(mockGetConnectionServiceUrlHandler(mockConnectionOrchestrator.Url))
	}

	Context("Connection", func() {
		When("connecting with a valid connection url", func() {

			BeforeEach(func() {
				setupHappyServers()
				setupHappyClient()
				conn, err = New(logger, mockBastion.Url, fakePrivateKey, params, headers, mockClient, mockAgentIdentityProvider, fakeMessageSigner)
			})

			It("instantiates without error", func() {
				Expect(err).ToNot(HaveOccurred(), "connection did not fail to instantiate")
			})

			It("connects successfully", func() {
				time.Sleep(time.Second)
				Expect(conn.Ready()).To(Equal(true), "connection failed to connect")
			})
		})

		When("connecting with an invalid connection url", func() {
			malformedUrl := "this is a malformed url"

			BeforeEach(func() {
				setupHappyClient()
				_, err = New(logger, malformedUrl, fakePrivateKey, params, headers, mockClient, mockAgentIdentityProvider, fakeMessageSigner)
			})

			It("fails to establish a connection", func() {
				Expect(err).To(HaveOccurred(), "connection successfully instantiated")
			})
		})
	})

	Context("Send", func() {
		When("a datachannel sends messages to the connection", func() {

			testAgentMessage := agentmessage.AgentMessage{
				MessageType: "keysplitting",
			}

			BeforeEach(func() {
				setupHappyServers()
				setupHappyClient()
				conn, _ = New(logger, mockBastion.Url, fakePrivateKey, params, headers, mockClient, mockAgentIdentityProvider, fakeMessageSigner)
				conn.Send(testAgentMessage)
			})

			It("it forwards those messages to the underlying connection", func() {
				time.Sleep(time.Second) // wait for agent message to make its way through channels
				mockClient.AssertCalled(GinkgoT(), "Send", mock.Anything)
			})
		})
	})

	Context("Receive", func() {
		When("receiving a mesage from the underlying connection", func() {
			var mockChannel *broker.MockChannel
			testId := "1234"

			testAgentMessage := agentmessage.AgentMessage{
				ChannelId:   testId,
				MessageType: "keysplitting",
			}

			testAgentMessageBytes, _ := json.Marshal(testAgentMessage)

			testSignalRMessage := &signalr.SignalRMessage{
				Type:         int(signalr.Invocation),
				Target:       "Test",
				Arguments:    []json.RawMessage{testAgentMessageBytes},
				InvocationId: "random",
			}

			BeforeEach(func() {
				setupHappyServers()
				setupHappyClient()
				conn, _ = New(logger, mockBastion.Url, fakePrivateKey, params, headers, mockClient, mockAgentIdentityProvider, fakeMessageSigner)

				mockChannel = new(broker.MockChannel)
				mockChannel.On("Receive").Return()
				conn.Subscribe(testId, mockChannel)

				// Send a message from the underlying connection
				inboundChan <- testSignalRMessage
			})

			It("forwards that message to the subscribed data channel", func() {
				time.Sleep(time.Second) // let signalr message run its course
				mockChannel.AssertCalled(GinkgoT(), "Receive", mock.Anything)
			})
		})
	})

	Context("Close", func() {
		When("the underlying connection dies", func() {

			BeforeEach(func() {
				setupHappyServers()
				setupHappyClient()
				conn, _ = New(logger, mockBastion.Url, fakePrivateKey, params, headers, mockClient, mockAgentIdentityProvider, fakeMessageSigner)

				doneChan <- struct{}{}
			})

			It("tries to reconnect", func() {
				time.Sleep(time.Second) // allow connection time to catch death
				Expect(conn.Ready()).To(Equal(true), "the connection is dead")
			})
		})

		When("it is closed from above", func() {

			BeforeEach(func() {
				setupHappyServers()
				setupHappyClient()
				conn, _ = New(logger, mockBastion.Url, fakePrivateKey, params, headers, mockClient, mockAgentIdentityProvider, fakeMessageSigner)
				conn.Close(fmt.Errorf("felt like it"), 2*time.Second)
			})

			It("dies", func() {
				time.Sleep(time.Second) // allow connection time to catch death
				Expect(conn.Ready()).To(Equal(false), "the connection is still alive")
			})
		})
	})
})
