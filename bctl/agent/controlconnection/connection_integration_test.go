package controlconnection

import (
	"net/http"
	"net/url"
	"time"

	agentidentity "bastionzero.com/agent/bastion/agentidentity/mocks"
	"bastionzero.com/bzerolib/connection"
	"bastionzero.com/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bzerolib/keypair"
	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/tests"
	"bastionzero.com/bzerolib/tests/connectionnode"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

var (
	// We omit the status codes 100, 102, and 103 because those status codes will
	// cause the http request to hang for the various reasons and make this test
	// too long, but they should be corrected by the http request timeout
	BadStatusCodes = [...]int{101, 300, 301, 302, 303, 304, 305, 400, 401, 402, 403,
		404, 405, 406, 407, 408, 409, 410, 411, 412, 413, 414, 415, 416, 417,
		418, 421, 422, 523, 424, 425, 426, 428, 429, 431, 451, 500, 501, 502,
		503, 504, 505, 506, 507, 508, 510, 511}
)

// responds with error codes until all error codes are exhausted and then
// responds using the defaultHandler
func respondWithErrorCodes(logger *logger.Logger, defaultHandler http.HandlerFunc) http.HandlerFunc {
	// create a copy of the array
	badStatusCodes := BadStatusCodes

	// convert the array to slice so it can be mutated
	errorCodesToRespond := badStatusCodes[:]

	// Cycle through every bad status code until there are none, then use the final handler instead
	return func(w http.ResponseWriter, r *http.Request) {
		if len(errorCodesToRespond) > 0 {
			logger.Infof("Bad status codes remaining: #%d, setting status code to: %d", len(errorCodesToRespond), errorCodesToRespond[0])
			w.WriteHeader(errorCodesToRespond[0])
			errorCodesToRespond = errorCodesToRespond[1:]
		} else {
			defaultHandler(w, r)
		}
	}
}

func waitForConnectionReady(conn connection.Connection) <-chan struct{} {
	doneChan := make(chan struct{})
	go func() {
		for {
			if conn.Ready() {
				close(doneChan)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	return doneChan
}

var _ = Describe("Agent Control Connection Integration", func() {
	logger := logger.MockLogger(GinkgoWriter)

	headers := http.Header{}
	params := url.Values{
		"public_key": {"publicKey"},
		"version":    {"agentVersion"},
		"target_id":  {"targetId"},
	}

	_, privateKey, _ := keypair.GenerateKeyPair()

	mockAgentIdentityToken := &agentidentity.MockAgentIdentityToken{}
	mockAgentIdentityToken.On("Get", mock.Anything).Return("fake-agent-identity-token", nil)

	createConnectionWithBastion := func(cnUrl string) connection.Connection {
		websocket.WebsocketUrlScheme = websocket.HttpWebsocketScheme
		wsLogger := logger.GetComponentLogger("Websocket")
		srLogger := logger.GetComponentLogger("SignalR")

		client := signalr.New(srLogger, websocket.New(wsLogger))
		conn, _ := New(logger, cnUrl, privateKey, params, headers, client, mockAgentIdentityToken)

		return conn
	}

	Context("Connecting", func() {

		When("The Bastion throws an error while trying to connect", func() {
			var mockCN *connectionnode.MockConnectionNode
			var conn connection.Connection
			var done <-chan struct{}

			BeforeEach(func() {
				mockCN = connectionnode.New(logger, controlChannelHubEndpoint)
				mockCO := setupMockConnectionOrchestrator(mockGetControlChannelHandler(mockCN.Url))
				mockBastion := setupMockBastion(respondWithErrorCodes(logger, mockGetConnectionServiceUrlHandler(mockCO.Url)))

				maxBackoffInterval = 50 * time.Millisecond

				conn = createConnectionWithBastion(mockBastion.Url)
				done = waitForConnectionReady(conn)
			})

			AfterEach(func() {
				mockCN.Close()
				conn.Close(tests.EndOfTest, time.Second)
			})

			It("retries to connect until it is able to successfully connect", func() {
				Eventually(done).WithTimeout(5*time.Second).Should(BeClosed(), "Connection never connected")
				Expect(retryCount).To(Equal(len(BadStatusCodes)+1), "Connect flow did not cycle through all bad status codes before connecting")
			})
		})

		When("The Connection Orchestrator throws an error while trying to connect", func() {
			var mockCN *connectionnode.MockConnectionNode
			var conn connection.Connection

			BeforeEach(func() {
				mockCN = connectionnode.New(logger, controlChannelHubEndpoint)
				mockCO := setupMockConnectionOrchestrator(respondWithErrorCodes(logger, mockGetControlChannelHandler(mockCN.Url)))
				mockBastion := setupMockBastion(mockGetConnectionServiceUrlHandler(mockCO.Url))

				maxBackoffInterval = 50 * time.Millisecond

				// put this in its own routine because trying to connect is blocking
				conn = createConnectionWithBastion(mockBastion.Url)
			})

			AfterEach(func() {
				mockCN.Close()
				conn.Close(tests.EndOfTest, time.Second)
			})

			It("retries to connect until it is able to successfully connect", func() {
				// time.Sleep(3 * time.Second)
				done := waitForConnectionReady(conn)
				Eventually(done).WithTimeout(5*time.Second).Should(BeClosed(), "Connection never connected")
				Expect(retryCount).To(Equal(len(BadStatusCodes)+1), "Connect flow did not cycle through all bad status codes before connecting")
			})
		})
	})

	Context("Closing the connection", func() {
		When("The Connection Node breaks the connection unexpectedly", func() {
			var mockCN *connectionnode.MockConnectionNode
			var conn connection.Connection

			BeforeEach(func() {
				mockCN = connectionnode.New(logger, controlChannelHubEndpoint)
				mockCO := setupMockConnectionOrchestrator(mockGetControlChannelHandler(mockCN.Url))
				mockBastion := setupMockBastion(mockGetConnectionServiceUrlHandler(mockCO.Url))

				conn = createConnectionWithBastion(mockBastion.Url)
				done := waitForConnectionReady(conn)
				Eventually(done).WithTimeout(time.Second).Should(BeClosed())

				mockCN.BreakWebsocket()
			})

			AfterEach(func() {
				mockCN.Close()
				conn.Close(tests.EndOfTest, time.Second)
			})

			It("reconnects", func() {
				time.Sleep(time.Second)
				Expect(conn.Ready()).To(Equal(true))
			})
		})

		When("The Connection Node closes the connection normally", func() {
			var mockCN *connectionnode.MockConnectionNode
			var conn connection.Connection

			BeforeEach(func() {
				mockCN = connectionnode.New(logger, controlChannelHubEndpoint)
				mockCO := setupMockConnectionOrchestrator(mockGetControlChannelHandler(mockCN.Url))
				mockBastion := setupMockBastion(mockGetConnectionServiceUrlHandler(mockCO.Url))

				conn = createConnectionWithBastion(mockBastion.Url)

				done := waitForConnectionReady(conn)
				Eventually(done).WithTimeout(time.Second).Should(BeClosed())

				mockCN.CloseWebsocket()
			})

			AfterEach(func() {
				mockCN.Close()
				conn.Close(tests.EndOfTest, time.Second)
			})

			It("reconnects", func() {
				time.Sleep(time.Second)
				Expect(conn.Ready()).To(Equal(true))
			})
		})
	})
})
