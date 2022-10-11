package websocket

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bastionzero.com/bctl/v1/bctl/agent/controlchannel/monitor"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter"
	"bastionzero.com/bctl/v1/bzerolib/logger"
)

func TestWebsocket(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Websocket Suite")
}

var _ = Describe("Websocket", Ordered, func() {
	var server *MockWebsocketServer
	var websocket transporter.Transporter
	var testUrl *url.URL

	logger := logger.MockLogger(GinkgoWriter)
	ctx := context.Background()
	stats := monitor.New(make(<-chan struct{}))

	testSendData := []byte("whooopie")
	WebsocketUrlScheme = HttpWebsocketScheme

	BeforeEach(func() {
		websocket = New(logger, stats)
	})

	Context("Making connections", func() {
		When("Connecting to a legitimate host", func() {
			var err error

			BeforeEach(func() {
				server = NewMockWebsocketServer(logger)
				testUrl, _ = url.Parse(server.Addr)

				err = websocket.Dial(testUrl, http.Header{}, ctx)

				server.Shutdown()
			})

			AfterEach(func() {
				server.Shutdown()
			})

			It("succeeds", func() {
				Expect(err).ShouldNot(HaveOccurred(), "Websocket was unable to connect: %s", err)
			})
		})

		When("Connecting to port with no listener", func() {
			var err error

			BeforeEach(func() {
				testUrl, _ = url.Parse("http://localhost:0")
				err = websocket.Dial(testUrl, http.Header{}, ctx)
			})

			It("fails", func() {
				Expect(err).Should(HaveOccurred(), "It looks like the websocket connected but it shouldn't have")
			})
		})
	})

	Context("Sending messages", func() {
		When("Communicating with a legitimate host", func() {
			var err error

			BeforeEach(func() {
				server = NewMockWebsocketServer(logger)
				testUrl, _ = url.Parse(server.Addr)

				err = websocket.Dial(testUrl, http.Header{}, ctx)
				err = websocket.Send(testSendData)
			})

			AfterEach(func() {
				server.Shutdown()
			})

			It("is received by the server", func() {
				Expect(err).ShouldNot(HaveOccurred(), "Websocket failed to send bytes: %s", err)

				message := <-server.ReceivedBytes
				Expect(message).To(Equal(testSendData), "Server never received the bytes we sent!")
			})
		})
	})

	Context("Receiving messages", func() {
		When("Communicating with a legitimate host", func() {

			BeforeEach(func() {
				server = NewMockWebsocketServer(logger)
				testUrl, _ = url.Parse(server.Addr)

				websocket.Dial(testUrl, http.Header{}, ctx)
				websocket.Send(testSendData)
			})

			AfterEach(func() {
				server.Shutdown()
			})

			It("receives messages", func() {
				// our mock server will write to the connection whatever
				// it receives on that same connection (hence Send() above)
				message := <-websocket.Inbound()
				Expect(*message).To(Equal(testSendData), "Websocket received different bytes from those we expected to be replayed to us")
			})
		})
	})

	Context("Shutdown", func() {
		When("an external object closes", func() {
			BeforeEach(func() {
				server = NewMockWebsocketServer(logger)
				testUrl, _ = url.Parse(server.Addr)

				websocket.Dial(testUrl, http.Header{}, ctx)
				websocket.Close(fmt.Errorf("felt like it"))
			})

			AfterEach(func() {
				server.Shutdown()
			})

			It("closes in a reasonable time", func() {
				select {
				case <-websocket.Done():
				case <-time.After(3 * time.Second):
					Expect(nil).ToNot(BeNil(), "Context failed to close in a reasonable time!")
				}
			})
		})
	})
})
