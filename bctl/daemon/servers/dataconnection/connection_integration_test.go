package dataconnection

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"bastionzero.com/bctl/v1/bzerolib/connection"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/tests"
	"bastionzero.com/bctl/v1/bzerolib/tests/connectionnode"
	"bastionzero.com/bctl/v1/bzerolib/tests/server"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Daemon Data Connection Integration", Ordered, func() {
	logger := logger.MockLogger(GinkgoWriter)

	params := url.Values{}
	headers := http.Header{}

	testAgentConnectedMessage := AgentConnectedMessage{
		ConnectionId: "testID",
	}

	createConnectionWithBastion := func(cnUrl string) connection.Connection {
		websocket.WebsocketUrlScheme = websocket.HttpWebsocketScheme
		wsLogger := logger.GetComponentLogger("Websocket")
		srLogger := logger.GetComponentLogger("SignalR")

		client := signalr.New(srLogger, websocket.New(wsLogger))
		conn, _ := New(logger, cnUrl, params, headers, client)

		return conn
	}

	Context("Connecting", func() {

		When("The Connection Node throws errors while trying to connect", func() {
			var websocketServer *server.WebsocketServer
			var mockCN *tests.MockServer
			var conn connection.Connection

			// We omit the status codes 100, 102, and 103 because those status codes will
			// cause the http request to hang for the various reasons and make this test
			// too long, but they should be corrected by the http request timeout
			badStatusCodes := []int{101, 300, 301, 302, 303, 304, 305, 400, 401, 402, 403,
				404, 405, 406, 407, 408, 409, 410, 411, 412, 413, 414, 415, 416, 417,
				418, 421, 422, 523, 424, 425, 426, 428, 429, 431, 451, 500, 501, 502,
				503, 504, 505, 506, 507, 508, 510, 511}

			BeforeEach(func() {
				websocketServer = server.NewWebsocketServer(logger)

				// Cycle through every bad status code until there are none, then make a
				// successful websocket connection
				respondWithError := func(w http.ResponseWriter, r *http.Request) {
					if len(badStatusCodes) > 0 {
						logger.Infof("Bad status codes remaining: #%d, setting status code to: %d", len(badStatusCodes), badStatusCodes[0])
						w.WriteHeader(badStatusCodes[0])
						badStatusCodes = badStatusCodes[1:]
					} else {
						websocketServer.Serve(w, r)
					}
				}

				mockCN = tests.NewMockServer(tests.MockHandler{
					Endpoint:    "/" + daemonHubEndpoint,
					HandlerFunc: respondWithError,
				})

				maxBackoffInterval = 50 * time.Millisecond

				// put this in its own routine because trying to connect is blocking
				go func() {
					conn = createConnectionWithBastion(mockCN.Url)

					messageBytes, _ := json.Marshal(testAgentConnectedMessage)

					signalRMessage := &signalr.SignalRMessage{
						Type:         int(signalr.Invocation),
						Target:       agentConnected,
						Arguments:    []json.RawMessage{messageBytes},
						InvocationId: fmt.Sprintf("%d", rand.Intn(1000)),
					}

					trackedMessageBytes, err := json.Marshal(signalRMessage)
					if err != nil {
						logger.Errorf("error marshalling outgoing SignalR Message: %+v", testAgentConnectedMessage)
						return
					}

					terminatedMessageBytes := append(trackedMessageBytes, signalr.TerminatorByte)
					websocketServer.Write(terminatedMessageBytes)
				}()
			})

			AfterEach(func() {
				websocketServer.Close()
				mockCN.Close()
				conn.Close(fmt.Errorf("end of test"), time.Second)
			})

			It("retries to connect until it is able to successfully connect", func() {
				time.Sleep(3 * time.Second)
				Expect(conn.Ready()).To(Equal(true), "Connection never connected")

				Expect(len(badStatusCodes)).To(Equal(0), "Connect flow did not cycle through all bad status codes before connecting")
			})
		})
	})

	Context("Closing the connection", func() {
		When("The Connection Node breaks the connection unexpectedly", func() {
			var mockCN *connectionnode.MockConnectionNode
			var conn connection.Connection

			BeforeEach(func() {
				mockCN = connectionnode.New(logger, daemonHubEndpoint)
				conn = createConnectionWithBastion(mockCN.Url)

				mockCN.SendSignalRMessage(agentConnected, testAgentConnectedMessage)
				time.Sleep(time.Millisecond)
				mockCN.BreakWebsocket()
			})

			AfterEach(func() {
				mockCN.Close()
				conn.Close(fmt.Errorf("end of test"), time.Second)
			})

			It("will try to reconnect", func() {
				time.Sleep(time.Second)
				Expect(conn.Ready()).To(Equal(false), "connection was ready before agent had reconnected")
				mockCN.SendSignalRMessage(agentConnected, testAgentConnectedMessage)
				time.Sleep(time.Millisecond)
				Expect(conn.Ready()).To(Equal(true), "connection did not reestablish itself after we unexpectedly broke the websocket connection")
			})
		})

		When("The Connection Node closes the connection normally", func() {
			var mockCN *connectionnode.MockConnectionNode
			var conn connection.Connection

			BeforeEach(func() {
				mockCN = connectionnode.New(logger, daemonHubEndpoint)
				conn = createConnectionWithBastion(mockCN.Url)

				mockCN.SendSignalRMessage(agentConnected, testAgentConnectedMessage)
				time.Sleep(time.Second)
				mockCN.CloseWebsocket()
			})

			AfterEach(func() {
				mockCN.Close()
				conn.Close(fmt.Errorf("end of test"), time.Second)
			})

			It("sends all remaining messages in the pipeline", func() {})

			It("will try to reconnect", func() {
				time.Sleep(time.Second)
				Expect(conn.Ready()).To(Equal(false), "connection was ready before agent has reconnected")

				mockCN.SendSignalRMessage(agentConnected, testAgentConnectedMessage)
				time.Sleep(time.Second)
				Expect(conn.Ready()).To(Equal(true), "connection ready after agent has reconnected")
			})
		})

		When("We receive word agent closes its connection to the connection node", func() {
			var mockCN *connectionnode.MockConnectionNode
			var conn connection.Connection
			mockCloseReason := "mock close reason"

			BeforeEach(func() {
				mockCN = connectionnode.New(logger, daemonHubEndpoint)

				mockCloseDaemonWebsocketMessage := CloseDaemonWebsocketMessage{
					Reason: mockCloseReason,
				}

				conn = createConnectionWithBastion(mockCN.Url)

				mockCN.SendSignalRMessage(agentConnected, testAgentConnectedMessage)
				mockCN.SendSignalRMessage(closeConnection, mockCloseDaemonWebsocketMessage)
			})

			AfterEach(func() {
				mockCN.Close()
				conn.Close(fmt.Errorf("end of test"), time.Second)
			})

			It("dies", func() {
				time.Sleep(time.Second)
				Expect(conn.Ready()).To(Equal(false))
				Expect(conn.Err().Error()).To(ContainSubstring((mockCloseReason)))
			})
		})
	})
})
