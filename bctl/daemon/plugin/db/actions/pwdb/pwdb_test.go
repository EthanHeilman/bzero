package pwdb

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"testing"
	"time"

	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/plugin"
	"bastionzero.com/bzerolib/plugin/db/actions/pwdb"
	smsg "bastionzero.com/bzerolib/stream/message"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPwdb(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Daemon PWDB Suite")
}

var _ = Describe("Daemon PWDB action", func() {
	logger := logger.MockLogger(GinkgoWriter)
	testUser := "fakeUser"
	targetId := "fakeTargetId"

	initPwdb := func(outbox chan plugin.ActionWrapper, doneChan chan struct{}, conn net.Conn) *Pwdb {
		pwdbTest := New(logger, testUser, targetId, outbox, doneChan)

		go func() {
			// this ends up being phrased awkwardly, but we're testing whether the plugin starts
			// correctly aka returns without error aka the returned error "Should Not Have Occurred".
			Eventually(pwdbTest.Start(conn)).WithTimeout(3 * time.Second).ShouldNot(HaveOccurred())
		}()

		// Receive our initial connect message
		Eventually(outbox).WithTimeout(3 * time.Second).Should(Receive())

		// Fake agent successful reponse to connect message
		pwdbTest.ReceiveMrtap(string(pwdb.Connect), []byte{})

		return pwdbTest
	}

	When("Starting", func() {
		outbox := make(chan plugin.ActionWrapper, 1)
		doneChan := make(chan struct{}, 1)
		lconn, _ := net.Pipe()

		BeforeEach(func() {
			tester := New(logger, testUser, targetId, outbox, doneChan)

			go func() {
				// this ends up being phrased awkwardly, but we're testing whether the start function
				// returns no error aka the error Should Not Have Occurred.
				Eventually(tester.Start(lconn)).WithTimeout(3 * time.Second).ShouldNot(HaveOccurred())
			}()

			tester.ReceiveMrtap(string(pwdb.Connect), []byte{})
		})

		AfterEach(func() {
			lconn.Close()
		})

		It("sends a connection message to the agent", func() {
			var msg plugin.ActionWrapper
			Eventually(outbox).WithTimeout(5 * time.Second).Should(Receive(&msg))
			Expect(msg.Action).To(Equal(string(pwdb.Connect)))
		})
	})

	When("Data is read from the local connection", func() {
		outbox := make(chan plugin.ActionWrapper, 1)
		doneChan := make(chan struct{}, 1)
		reciever, sender := net.Pipe()
		testBytes := []byte("poppy poppy love love")

		BeforeEach(func() {
			initPwdb(outbox, doneChan, reciever)

			// Write to our local connection
			_, err := sender.Write(testBytes)
			Expect(err).ShouldNot(HaveOccurred())
		})

		AfterEach(func() {
			reciever.Close()
			sender.Close()
		})

		It("sends any data from the local connection to the agent", func() {
			var msg plugin.ActionWrapper
			Eventually(outbox).WithTimeout(5 * time.Second).Should(Receive(&msg))
			Expect(msg.Action).To(Equal(string(pwdb.Input)))

			payload, _ := json.Marshal(pwdb.InputPayload{
				Data: base64.StdEncoding.EncodeToString(testBytes),
			})

			Expect(msg.ActionPayload).To(Equal(payload))
		})
	})

	When("Receiving a stream message", func() {
		outbox := make(chan plugin.ActionWrapper, 1)
		doneChan := make(chan struct{}, 1)
		testBytes := []byte("poppy poppy love love")

		// the way pipe works is that whatever is writen to one conn is read from the other and vice versa
		lconn, mockAppConn := net.Pipe()

		BeforeEach(func() {
			pwdbTest := initPwdb(outbox, doneChan, lconn)

			streamMsg := smsg.StreamMessage{
				SchemaVersion: smsg.CurrentSchema,
				Type:          smsg.Stream,
				More:          true,
				Content:       base64.StdEncoding.EncodeToString(testBytes),
			}
			pwdbTest.ReceiveStream(streamMsg)
		})

		AfterEach(func() {
			lconn.Close()
			mockAppConn.Close()
		})

		It("writes stream messages to the local connection", func() {
			buf := make([]byte, chunkSize)
			Eventually(mockAppConn.Read).WithArguments(buf).WithTimeout(3 * time.Second).Should(BeNumerically("==", len(testBytes)))
		})
	})

	Context("Exiting", func() {
		When("Receiving a stream error message", func() {
			outbox := make(chan plugin.ActionWrapper, 1)
			doneChan := make(chan struct{}, 1)
			lconn, _ := net.Pipe()
			errorBytes := []byte("poppy poppy love love")

			BeforeEach(func() {
				pwdbTest := initPwdb(outbox, doneChan, lconn)

				streamMsg := smsg.StreamMessage{
					SchemaVersion: smsg.CurrentSchema,
					Type:          smsg.Error,
					Content:       base64.StdEncoding.EncodeToString(errorBytes),
				}
				pwdbTest.ReceiveStream(streamMsg)
			})

			AfterEach(func() {
				lconn.Close()
			})

			It("exits", func() {
				Eventually(doneChan).WithTimeout(3 * time.Second).Should(BeClosed())
			})
		})

		When("The local connection closes", func() {
			var lconn net.Conn
			outbox := make(chan plugin.ActionWrapper, 1)
			doneChan := make(chan struct{}, 1)

			BeforeEach(func() {
				lconn, _ = net.Pipe()

				initPwdb(outbox, doneChan, lconn)
				time.Sleep(time.Second)
				lconn.Close()
			})

			It("exits correctly", func() {
				By("sending a close message")
				var msg plugin.ActionWrapper
				Eventually(outbox).WithTimeout(5 * time.Second).Should(Receive(&msg))
				Expect(msg.Action).To(Equal(string(pwdb.Close)))

				var closePayload pwdb.ClosePayload
				err := json.Unmarshal(msg.ActionPayload, &closePayload)
				Expect(err).ToNot(HaveOccurred())

				Expect(closePayload.Reason).To(Equal("io: read/write on closed pipe"))

				By("closing the plugin")
				Eventually(doneChan).WithTimeout(3 * time.Second).Should(BeClosed())
			})
		})
	})
})
