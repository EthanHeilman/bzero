package signalr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/tests/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSignalR(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SignalR Suite")
}

var _ = Describe("SignalR", Ordered, func() {
	var doneChan chan struct{}
	var inboundChan chan *[]byte
	var negotiateServer *mocks.MockServer
	var mockTransport *transporter.MockTransporter
	var signalR *SignalR

	logger := logger.MockLogger()
	ctx := context.Background()
	testBytes := []byte("whooopie")
	endpoint := "/" + negotiateEndpoint

	testTargetFunc := func(msg agentmessage.AgentMessage) (string, error) {
		return "TestSignalRMethod", nil
	}

	setupHappyTransport := func() {
		// Startup a server to handle negotiation
		negotiateServer = mocks.NewMockServer(mocks.MockHandler{
			Endpoint:    endpoint,
			HandlerFunc: handleNegotiate,
		})

		mockTransport = &transporter.MockTransporter{}
		mockTransport.On("Dial").Return(nil)
		mockTransport.On("Send").Return(nil)
		mockTransport.On("Close").Return()

		doneChan = make(chan struct{})
		mockTransport.On("Done").Return(doneChan)

		inboundChan = make(chan *[]byte, 1)
		mockTransport.On("Inbound").Return(inboundChan)

		signalR = New(logger, mockTransport)
		signalR.Connect(ctx, negotiateServer.Addr, http.Header{}, url.Values{}, testTargetFunc)
	}

	Context("Connection", func() {
		When("It fails to negotiate", func() {
			testUrlWithNoListener := "localhost:0"
			var err error

			BeforeEach(func() {
				mockTransport = &transporter.MockTransporter{}
				signalR = New(logger, mockTransport)

				err = signalR.Connect(ctx, testUrlWithNoListener, http.Header{}, url.Values{}, testTargetFunc)
			})

			It("fails to create the connection", func() {
				Expect(err).To(HaveOccurred(), "SignalR should have failed to connect")
			})
		})

		When("The underlying connection fails to connect", func() {
			var err error

			BeforeEach(func() {
				// Startup a server to handle negotiation
				negotiateServer = mocks.NewMockServer(mocks.MockHandler{
					Endpoint:    endpoint,
					HandlerFunc: handleNegotiate,
				})

				mockTransport = &transporter.MockTransporter{}
				mockTransport.On("Dial").Return(fmt.Errorf("failure"))

				signalR = New(logger, mockTransport)
				err = signalR.Connect(ctx, negotiateServer.Addr, http.Header{}, url.Values{}, testTargetFunc)
			})

			AfterEach(func() {
				negotiateServer.Close()
			})

			It("fails to create the connection", func() {
				Expect(err).To(HaveOccurred(), "SignalR should have failed to connect")
			})
		})
	})

	Context("Sending", func() {
		When("It connects to a legitimate connection", func() {
			var err error

			testAgentMessage := agentmessage.AgentMessage{
				MessageType:    "Test",
				MessagePayload: testBytes,
			}

			BeforeEach(func() {
				setupHappyTransport()
				err = signalR.Send(testAgentMessage)
			})

			AfterEach(func() {
				negotiateServer.Close()
			})

			It("is able to send without error", func() {
				Expect(err).ToNot(HaveOccurred(), "Websocket failed to send to server")
			})
		})
	})

	Context("Receiving", func() {
		var err error

		testAgentMessage := agentmessage.AgentMessage{
			MessageType:    "Test",
			MessagePayload: testBytes,
		}
		testAgentMessageBytes, _ := json.Marshal(testAgentMessage)

		testSignalRMessage := SignalRMessage{
			Type:         int(Invocation),
			Target:       "Test",
			Arguments:    []json.RawMessage{testAgentMessageBytes},
			InvocationId: "123",
		}
		testSignalRMessageBytes, _ := json.Marshal(testSignalRMessage)
		validTestSignalRMessageBytes := append(testSignalRMessageBytes, signalRMessageTerminatorByte)

		When("It connects to a legitimate connection", func() {

			BeforeEach(func() {
				setupHappyTransport()
				inboundChan <- &validTestSignalRMessageBytes
			})

			AfterEach(func() {
				negotiateServer.Close()
			})

			It("is able to receive", func() {
				message := <-signalR.Inbound()

				// This tests an assumption that a lot of our higher-up code relies on that
				// there is a single argument in received messages and that argument is an
				// AgentMessage
				Expect(len(message.Arguments)).To(Equal(1), "SignalR messages should only have one argument, this one had %d", len(message.Arguments))

				var agentMessage agentmessage.AgentMessage
				err = json.Unmarshal(message.Arguments[0], &agentMessage)
				Expect(err).ToNot(HaveOccurred(), "Failed to unmarshal the received agent message: %s", err)

				Expect(agentMessage.MessagePayload).To(Equal(testBytes), "We received a message different from the one we sent: %+v", agentMessage)
			})
		})
	})

	Context("Shutdown", func() {
		When("It is closed from above", func() {

			BeforeEach(func() {
				setupHappyTransport()

				signalR.Close(fmt.Errorf("testing"))
			})

			AfterEach(func() {
				negotiateServer.Close()
			})

			It("closes in a reasonable time", func() {
				select {
				case <-signalR.Done():
				case <-time.After(2 * time.Second):
					Expect(nil).ToNot(BeNil(), "SignalR failed to close!")
				}
			})
		})

		When("It is closed from below", func() {

			BeforeEach(func() {
				setupHappyTransport()

				close(doneChan)
			})

			AfterEach(func() {
				negotiateServer.Close()
			})

			It("closes in a reasonable time", func() {
				select {
				case <-signalR.Done():
				case <-time.After(2 * time.Second):
					Expect(nil).ToNot(BeNil(), "SignalR failed to close!")
				}
			})
		})
	})
})
