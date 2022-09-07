package datachannelconnection

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
	RunSpecs(t, "Agent Datachannel Connection Suite")
}

var _ = Describe("Agent Datachannel Connection", Ordered, func() {
	validUrl := "localhost:0"

	var conn connection.Connection
	var mockClient *messenger.MockMessenger
	var doneChan chan struct{}
	var inboundChan chan *signalr.SignalRMessage
	var err error

	logger := logger.MockLogger()
	params := url.Values{}
	headers := http.Header{}

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

	Context("Connection", func() {
		When("connecting with a valid connection url", func() {

			BeforeEach(func() {
				setupHappyClient()
				conn, err = New(logger, validUrl, params, headers, mockClient)
			})

			It("instantiates without error", func() {
				Expect(err).ToNot(HaveOccurred(), "connection object creation failed")
			})

			It("connects successfully", func() {
				time.Sleep(time.Second)
				Expect(conn.Ready()).To(Equal(true), "connection failed to connect and/or set itself as ready")
			})
		})

		When("connecting with an invalid connection url", func() {
			malformedUrl := "this is a malformed url"

			BeforeEach(func() {
				setupHappyClient()
				_, err = New(logger, malformedUrl, params, headers, mockClient)
			})

			It("fails to establish a connection", func() {
				Expect(err).To(HaveOccurred(), "there was no error creating our connection object")
			})
		})
	})

	Context("Send", func() {
		When("a datachannel sends messages to the connection", func() {

			testAgentMessage := agentmessage.AgentMessage{
				MessageType: "keysplitting",
			}

			BeforeEach(func() {
				setupHappyClient()
				conn, _ = New(logger, validUrl, params, headers, mockClient)
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
				setupHappyClient()
				conn, _ = New(logger, validUrl, params, headers, mockClient)

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
				conn, _ = New(logger, validUrl, params, headers, mockClient)

				doneChan <- struct{}{}
			})

			It("dies", func() {
				time.Sleep(time.Second) // allow connection time to catch death
				Expect(conn.Ready()).To(Equal(false), "the connection did not disconnect")
			})
		})

		When("it is closed from above", func() {

			BeforeEach(func() {
				setupHappyClient()
				conn, _ = New(logger, validUrl, params, headers, mockClient)
				conn.Close(fmt.Errorf("felt like it"))
			})

			It("dies", func() {
				time.Sleep(time.Second) // allow connection time to catch death
				Expect(conn.Ready()).To(Equal(false), "connectino failed to shut down")
			})
		})
	})
})