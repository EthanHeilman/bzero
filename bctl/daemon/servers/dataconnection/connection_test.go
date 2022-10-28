package dataconnection

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"bastionzero.com/bctl/v1/bzerolib/connection"
	"bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/broker"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

func TestDatachannelConnection(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Daemon Datachannel Connection Suite")
}

var _ = Describe("Daemon Datachannel Connection", Ordered, func() {
	validUrl := "localhost:0"

	var mockClient *messenger.MockMessenger
	var conn connection.Connection

	var doneChan chan struct{}
	var inboundChan chan *signalr.SignalRMessage
	var err error

	logger := logger.MockLogger(GinkgoWriter)
	params := url.Values{}
	headers := http.Header{}

	agentConnMsg := AgentConnectedMessage{
		ConnectionId: "1234",
	}
	agentConnMsgBytes, _ := json.Marshal(agentConnMsg)

	testSignalRMessage := &signalr.SignalRMessage{
		Type:         int(signalr.Invocation),
		Target:       agentConnected,
		Arguments:    []json.RawMessage{agentConnMsgBytes},
		InvocationId: "random",
	}

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

	setupHappyConnection := func(mockClient *messenger.MockMessenger) connection.Connection {
		conn, _ := New(logger, validUrl, params, headers, mockClient)
		inboundChan <- testSignalRMessage
		time.Sleep(time.Second)

		return conn
	}

	Context("Connection", func() {
		When("connecting with a valid connection url", func() {

			BeforeEach(func() {
				setupHappyClient()
				conn = setupHappyConnection(mockClient)
			})

			It("instantiates without error", func() {
				Expect(err).ToNot(HaveOccurred())
			})
		})

		When("it has been instantiated", func() {

			BeforeEach(func() {
				setupHappyClient()
				conn = setupHappyConnection(mockClient)
			})

			It("waits for the agent to connect before setting itself to ready", func() {
				time.Sleep(time.Second)
				Expect(conn.Ready()).To(Equal(true), "the connection is not ready")
			})
		})

		When("connecting with an invalid connection url", func() {
			malformedUrl := "this is a malformed url"

			BeforeEach(func() {
				setupHappyClient()
				_, err = New(logger, malformedUrl, params, headers, mockClient)
			})

			It("fails to establish a connection", func() {
				Expect(err).To(HaveOccurred(), "the connection is connected and ready")
			})
		})
	})

	Context("Send", func() {
		When("a datachannel sends messages to the connection", func() {
			testAgentMessage := agentmessage.AgentMessage{
				MessageType: "mrtap",
			}

			BeforeEach(func() {
				setupHappyClient()
				conn = setupHappyConnection(mockClient)

				conn.Send(testAgentMessage)
			})

			It("it forwards those messages to the underlying connection", func() {
				time.Sleep(time.Second) // wait for agent message to make its way through channels
				mockClient.AssertCalled(GinkgoT(), "Send", mock.Anything)
			})
		})
	})

	Context("Receive", func() {
		When("receiving a message from the underlying connection", func() {
			var mockChannel *broker.MockChannel
			testId := "1234"

			testAgentMessage := agentmessage.AgentMessage{
				ChannelId:   testId,
				MessageType: "mrtap",
			}

			testAgentMessageBytes, _ := json.Marshal(testAgentMessage)

			testSignalRMessage := &signalr.SignalRMessage{
				Type:         int(signalr.Invocation),
				Target:       "Test",
				Arguments:    []json.RawMessage{testAgentMessageBytes},
				InvocationId: "random",
			}

			BeforeEach(func() {
				setupHappyClient()
				conn = setupHappyConnection(mockClient)

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
				setupHappyClient()
				conn = setupHappyConnection(mockClient)

				doneChan <- struct{}{}
			})

			It("reconnects", func() {
				time.Sleep(time.Second) // allow connection time to catch death

				Expect(conn.Ready()).To(Equal(false), "the connection is ready before the agent reconnects")
				inboundChan <- testSignalRMessage
				time.Sleep(time.Second) // allow connection time to process agent connected message
				Expect(conn.Ready()).To(Equal(true), "the connection is not ready after the agent connects")
			})
		})

		When("it is closed from above", func() {

			BeforeEach(func() {
				setupHappyClient()
				conn = setupHappyConnection(mockClient)

				conn.Close(fmt.Errorf("felt like it"), 2*time.Second)
			})

			It("dies", func() {
				time.Sleep(time.Second) // allow connection time to catch death
				Expect(conn.Ready()).To(Equal(false), "the connection is still alive")
			})
		})
	})
})
