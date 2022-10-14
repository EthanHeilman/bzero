package keysplitting

import (
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"bastionzero.com/bctl/v1/bctl/daemon/keysplitting/bzcert"
	rrr "bastionzero.com/bctl/v1/bzerolib/error"
	"bastionzero.com/bctl/v1/bzerolib/keypair"
	commonbzcert "bastionzero.com/bctl/v1/bzerolib/keysplitting/bzcert"
	ksmsg "bastionzero.com/bctl/v1/bzerolib/keysplitting/message"
	"bastionzero.com/bctl/v1/bzerolib/keysplitting/util"
	log "bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/tests"
	"bastionzero.com/bctl/v1/bzerolib/logger"

	"github.com/Masterminds/semver"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDaemonKeysplitting(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Daemon keysplitting suite")
}

var _ = Describe("Daemon keysplitting", func() {
	var logger *log.Logger
	emptyPayload := []byte{}

	const testAction string = "test/action"
	const prePipeliningVersion string = "1.9"
	const preSynAckNonceChangeVersion string = "2.0"

	// Setup keypairs to use for agent and daemon
	agentPublicKey, agentPrivateKey, _ := keypair.GenerateKeyPair()
	GinkgoWriter.Printf("Agent keypair: Private key: %s; Public key: %s\n", agentPrivateKey.String(), agentPublicKey.String())
	daemonPublicKey, daemonPrivateKey, _ := keypair.GenerateKeyPair()
	GinkgoWriter.Printf("Daemon keypair: Private key: %s; Public key: %s\n", daemonPrivateKey.String(), daemonPublicKey.String())

	createFakeBzCert := func() {commonbzcert.BZCert {
		Rand:            "dummyCerRand",
		SignatureOnRand: "dummyCerRandSignature",
		InitialIdToken:  "dummyInitialIdToken",
		CurrentIdToken:  "dummyCurrentIdToken",
		ClientPublicKey: daemonPublicKey.String(),
	}
	

	createSUT := func() (*Keysplitting, error) {
		fakeBZCert := createFakeBzCert()
		// Reset MockDaemonBZCert and set default mock returns
		mockBZCert := &bzcert.MockDaemonBZCert{}
		mockBZCert.On("PrivateKey").Return(&daemonPrivateKey)
		mockBZCert.On("Expired").Return(false)
		mockBZCert.On("Refresh").Return(nil)
		mockBZCert.On("Hash").Return(&fakeBZCert)
		mockBZCert.On("Cert").Return(&fakeBZCert)

		// Init the SUT
		return New(logger, agentPublicKey, mockBZCert)
	}

	getSchemaVersionAsSemVer := func(agentSchemaVersion string) *semver.Version {
		parsedSchemaVersion, _ := semver.NewVersion(agentSchemaVersion)
		return parsedSchemaVersion
	}

	buildSynAckWithNonce := func(syn *ksmsg.KeysplittingMessage, agentSchemaVersion string, nonce string) *ksmsg.KeysplittingMessage {
		synAck, _ := syn.BuildUnsignedSynAck(
			emptyPayload,
			agentPublicKey.String(),
			nonce,
			getSchemaVersionAsSemVer(agentSchemaVersion).String(),
		)
		synAck.Sign(agentPrivateKey)
		return &synAck
	}
	buildSynAckWithVersion := func(syn *ksmsg.KeysplittingMessage, agentSchemaVersion string) *ksmsg.KeysplittingMessage {
		return buildSynAckWithNonce(syn, agentSchemaVersion, util.Nonce())
	}
	buildSynAck := func(syn *ksmsg.KeysplittingMessage) *ksmsg.KeysplittingMessage {
		return buildSynAckWithVersion(syn, ksmsg.SchemaVersion)
	}

	buildDataAckWithVersion := func(data *ksmsg.KeysplittingMessage, agentSchemaVersion string) *ksmsg.KeysplittingMessage {
		dataAck, _ := data.BuildUnsignedDataAck(
			emptyPayload,
agentPublicKey.String(),
			getSchemaVersionAsSemVer(agentSchemaVersion).String(),
		)
		dataAck.Sign(agentPrivateKey)
		return &dataAck
	}
	buildDataAck := func(data *ksmsg.KeysplittingMessage) *ksmsg.KeysplittingMessage {
		return buildDataAckWithVersion(data, ksmsg.SchemaVersion)
	}

	// SUT's logger
	logger = log.MockLogger(GinkgoWriter)

	// Setup keypairs to use for agent and daemon
	agentKeypair, _ = tests.GenerateEd25519Key()
	GinkgoWriter.Printf("Agent keypair: Private key: %s; Public key: %s\n", agentKeypair.Base64EncodedPrivateKey, agentKeypair.Base64EncodedPublicKey)
	daemonKeypair, _ = tests.GenerateEd25519Key()
	GinkgoWriter.Printf("Daemon keypair: Private key: %s; Public key: %s\n", daemonKeypair.Base64EncodedPrivateKey, daemonKeypair.Base64EncodedPublicKey)

	Context("Creation", func() {
		When("creating a new keysplitter", func() {
			var err error

			BeforeEach(func() {
				_, err = createSUT()
			})

			It("creates without error", func() {
				Expect(err).ShouldNot(HaveOccurred())
			})
		})
	})

	Context("Send Syn", func() {
		When("the bzcert is valid", func() {
			var syn *ksmsg.KeysplittingMessage
			var outboxSyn *ksmsg.KeysplittingMessage
			var synErr error

			testPayload := []byte("butt")

			BeforeEach(func() {
				sut, err := createSUT()
				Expect(err).ShouldNot(HaveOccurred())

				syn, synErr = sut.BuildSyn(testAction, testPayload, true)

				Expect(sut.Outbox()).Should(Receive(&outboxSyn))
			})

			It("builds the syn without error", func() {
				Expect(synErr).ToNot(HaveOccurred())
			})

			It("builds the syn correctly", func() {
				By("Setting the correct type")
				Expect(syn.Type).To(Equal(ksmsg.Syn))

				By("Validly signing the message")
				Expect(syn.VerifySignature(daemonPublicKey)).ShouldNot(HaveOccurred())

				By("Creating a SYN payload")
				synPayload, ok := syn.KeysplittingPayload.(ksmsg.SynPayload)
				Expect(ok).To(BeTrue())

				By("Setting the nonce")
				Expect(synPayload.Nonce).NotTo(BeEmpty())

				By("Setting the passed action and action payload")
				Expect(synPayload.Action).To(Equal(testAction))
				Expect(synPayload.ActionPayload).To(BeEquivalentTo(fmt.Sprintf("\"%v\"", base64.StdEncoding.EncodeToString(testPayload))))
			})

			It("sends the syn to the outbox", func() {
				Expect(syn).To(Equal(outboxSyn))
			})
		})

		When("the bzcert returns a bad key", func() {
			var syn *ksmsg.KeysplittingMessage
			var synError error

			BeforeEach(func() {
				fakeBZCert := createFakeBzCert()

				badBZCert := &bzcert.MockDaemonBZCert{}
				badBZCert.On("Refresh").Return(nil)
				badBZCert.On("Cert").Return(&fakeBZCert)
				badBZCert.On("PrivateKey").Return(&keypair.PrivateKey{key:[]byte("badkey")})

				sut, err := New(logger, agentKeypair.Base64EncodedPublicKey, badBZCert)
				Expect(err).ShouldNot(HaveOccurred())

				syn, synError = sut.BuildSyn(testAction, emptyPayload, true)
			})

			It("fails to build the syn", func() {
				Expect(syn).To(BeNil())
				Expect(synError.Error()).To(ContainSubstring(ErrFailedToSign.Error()))
			})
		})

		When("the bzcert fails to refresh", func() {
			var synError error

			BeforeEach(func() {
				badBZCert := &bzcert.MockDaemonBZCert{}
				refreshError := errors.New("refresh error")
				badBZCert.On("Refresh").Return(refreshError)

				sut, err := New(logger, agentPublicKey, badBZCert)
				Expect(err).ShouldNot(HaveOccurred())

				_, synError = sut.BuildSyn(testAction, emptyPayload, true)
			})

			It("fails to build a syn", func() {
				Expect(synError).Should(HaveOccurred())
			})
		})
	})

	Context("Send Data", func() {
		When("the handshake is incomplete", func() {
			var sut *Keysplitting
			var syn *ksmsg.KeysplittingMessage

			BeforeEach(func() {
				var err error
				sut, err = createSUT()
				Expect(err).ShouldNot(HaveOccurred())

				syn, err = sut.BuildSyn(testAction, emptyPayload, true)
				Expect(err).ShouldNot(HaveOccurred())

				// clear our outbox
				sut.outboxQueue = make(chan *ksmsg.KeysplittingMessage, maxPipelineLimit)
			})

			It("does not send the data message", func() {
				done := make(chan interface{})
				go func() {
					defer GinkgoRecover()

					By("Sending a message that causes Inbox() to block because handshake is not complete")
					err := sut.Inbox(testAction, emptyPayload)
					Expect(err).ShouldNot(HaveOccurred(), "sending Data should not error")
					var dataMsg *ksmsg.KeysplittingMessage
					Expect(sut.Outbox()).Should(Receive(&dataMsg), "outbox should receive the Data message sent by Inbox()")

					close(done)
				}()

				// Check that nothing is received on Outbox for some fixed
				// duration
				Consistently(sut.Outbox(), 500*time.Millisecond).ShouldNot(Receive(), "no message should be sent to outbox because the handshake never completed")

				// complete the handshake to release the above Inbox call
				synAck := buildSynAck(syn)
				sut.Validate(synAck)

				Eventually(done).Should(BeClosed(), "done should eventually be closed because agent sent a SynAck, which completes the handshake, and should unblock Inbox()")
			})
		})

		When("the handshake it complete", func() {
			var sut *Keysplitting
			var synAck *ksmsg.KeysplittingMessage

			inboxErr := fmt.Errorf("has not sent our message yet")
			testPayload := []byte("here")

			BeforeEach(func() {
				var err error
				sut, err = createSUT()
				Expect(err).ShouldNot(HaveOccurred())

				// handshake
				_, err = sut.BuildSyn(testAction, emptyPayload, true)
				Expect(err).ShouldNot(HaveOccurred())

				var syn *ksmsg.KeysplittingMessage
				Expect(sut.Outbox()).Should(Receive(&syn))

				synAck = buildSynAck(syn)
				sut.Validate(synAck)

				inboxErr = sut.Inbox(testAction, testPayload)
			})

			It("receives new input without error", func() {
				Expect(inboxErr).ToNot(HaveOccurred())
			})

			It("sends a new data message", func() {
				Expect(sut.Outbox()).Should(Receive(), "the expected data message was never put into the outbox")
			})

			It("builds the data message correctly", func() {
				var data *ksmsg.KeysplittingMessage
				Expect(sut.Outbox()).Should(Receive(&data))

				By("Setting the correct type")
				Expect(data.Type).To(Equal(ksmsg.Data))

				By("Signing with a valid signature")
				Expect(data.VerifySignature(daemonPublicKey)).ShouldNot(HaveOccurred())

				By("Creating the appropriate type of payload")
				dataPayload, ok := data.KeysplittingPayload.(ksmsg.DataPayload)
				Expect(ok).To(BeTrue())
				Expect(dataPayload.Type).To(BeEquivalentTo(ksmsg.Data))

				By("Setting the correct schema version")
				Expect(dataPayload.SchemaVersion).To(Equal(getSchemaVersionAsSemVer(ksmsg.SchemaVersion).String()), "The schema version should match the agreed upon version found in the agent's SynAck")

				By("Setting the hpointer equal to the hash of the previous message")
				Expect(dataPayload.HPointer).Should(Equal(synAck.Hash()), "This Data message's HPointer should point to the syn ack")

				By("Setting the passed action and payload variables")
				Expect(dataPayload.Action).To(Equal(testAction))
				Expect(dataPayload.ActionPayload).To(Equal(testPayload))
			})
		})
	})

	Context("SynAck Validation", func() {
		buildValidSynAck := func(sut *Keysplitting) *ksmsg.KeysplittingMessage {
			syn, _ := sut.BuildSyn(testAction, emptyPayload, true)
			return buildSynAck(syn)
		}

		When("the agent message is not built on a previously sent message", func() {
			var validateErr error

			BeforeEach(func() {
				sut, err := createSUT()
				Expect(err).ShouldNot(HaveOccurred())
				synAck := buildValidSynAck(sut)

				synAckPayload, _ := synAck.KeysplittingPayload.(ksmsg.SynAckPayload)
				synAckPayload.HPointer = "fake"
				synAck.KeysplittingPayload = synAckPayload

				// sign again since we just changed a value
				synAck.Sign(agentPrivateKey)

				validateErr = sut.Validate(synAck)
			})

			It("fails to validate with unknown hpointer error", func() {
				Expect(validateErr).Should(MatchError(ErrUnknownHPointer))
			})
		})

		When("the schema version cannot be parsed", func() {
			var validateErr error

			BeforeEach(func() {
				sut, err := createSUT()
				Expect(err).ShouldNot(HaveOccurred())
				synAck := buildValidSynAck(sut)

				synAckPayload, _ := synAck.KeysplittingPayload.(ksmsg.SynAckPayload)
				synAckPayload.SchemaVersion = "bad-version"
				synAck.KeysplittingPayload = synAckPayload

				// sign again since we just changed a value
				synAck.Sign(agentPrivateKey)

				validateErr = sut.Validate(synAck)
			})

			It("fails to validate with failed to parse version error", func() {
				Expect(validateErr).Should(MatchError(ErrFailedToParseVersion))
			})
		})

		When("the agent message is unsigned", func() {
			var validateErr error

			BeforeEach(func() {
				sut, err := createSUT()
				Expect(err).ShouldNot(HaveOccurred())
				synAck := buildValidSynAck(sut)

				synAck.Signature = ""

				validateErr = sut.Validate(synAck)
			})

			It("fails to validate with invalid signature error", func() {
				Expect(validateErr).Should(MatchError(ErrInvalidSignature))
			})
		})

		When("the agent message is signed", func() {
			var validateErr error

			BeforeEach(func() {
				sut, err := createSUT()
				Expect(err).ShouldNot(HaveOccurred())

				synAck := buildValidSynAck(sut)

				validateErr = sut.Validate(synAck)
			})

			It("validates successfully", func() {
				Expect(validateErr).ToNot(HaveOccurred())
			})
		})

		When("the agent message is signed by a legacy agent (CWC-1553)", func() {
			var validateErr error

			BeforeEach(func() {
				sut, err := createSUT()
				Expect(err).ShouldNot(HaveOccurred())

				diffPublicKey, _, _ := keypair.GenerateKeyPair()
				sut.agentPubKey = diffPublicKey

				synAck := buildValidSynAck(sut)
				validateErr = sut.Validate(synAck)
			})

			It("validates without error", func() {
				Expect(validateErr).ShouldNot(HaveOccurred())
			})
		})
	})

	Context("DataAck Validation", func() {
		buildValidDataAck := func(sut *Keysplitting) *ksmsg.KeysplittingMessage {
			_, err := sut.BuildSyn(testAction, emptyPayload, true)
			Expect(err).ShouldNot(HaveOccurred())

			var syn *ksmsg.KeysplittingMessage
			Expect(sut.Outbox()).Should(Receive(&syn))

			synAck := buildSynAck(syn)
			sut.Validate(synAck)

			err = sut.Inbox(testAction, emptyPayload)
			Expect(err).ShouldNot(HaveOccurred())

			var data *ksmsg.KeysplittingMessage
			Expect(sut.Outbox()).Should(Receive(&data))
			return buildDataAck(data)
		}

		When("the agent message is signed", func() {
			var validateErr error

			BeforeEach(func() {
				sut, err := createSUT()
				Expect(err).ShouldNot(HaveOccurred())

				dataAck := buildValidDataAck(sut)

				validateErr = sut.Validate(dataAck)
			})

			It("validates successfully", func() {
				Expect(validateErr).ToNot(HaveOccurred())
			})
		})

		When("the agent message is not built on a previously sent message", func() {
			var validateErr error

			BeforeEach(func() {
				sut, err := createSUT()
				Expect(err).ShouldNot(HaveOccurred())

				dataAck := buildValidDataAck(sut)
				dataAckPayload, _ := dataAck.KeysplittingPayload.(ksmsg.DataAckPayload)
				dataAckPayload.HPointer = "fake"
				dataAck.KeysplittingPayload = dataAckPayload

				// sign again since we just changed a value
				dataAck.Sign(agentPrivateKey)

				validateErr = sut.Validate(dataAck)
			})

			It("fails to validate with unknown hpointer error", func() {
				Expect(validateErr).Should(MatchError(ErrUnknownHPointer))
			})
		})

		When("the agent message is unsigned", func() {
			var validateErr error

			BeforeEach(func() {
				sut, err := createSUT()
				Expect(err).ShouldNot(HaveOccurred())

				dataAck := buildValidDataAck(sut)
				dataAck.Signature = ""

				validateErr = sut.Validate(dataAck)
			})

			It("fails to validate with invalid signature error", func() {
				Expect(validateErr).Should(MatchError(ErrInvalidSignature))
			})
		})
	})

	Describe("pipelining", func() {
		const timeToPollNothingReceivedOnOutbox time.Duration = 500 * time.Millisecond

		sendData := func(sut *Keysplitting, payload []byte) *ksmsg.KeysplittingMessage {
			err := sut.Inbox(testAction, payload)
			Expect(err).ShouldNot(HaveOccurred())

			var data *ksmsg.KeysplittingMessage
			Expect(sut.Outbox()).Should(Receive(&data))
			return data
		}

		performHandshakeWithVersion := func(sut *Keysplitting, agentVersion string) *ksmsg.KeysplittingMessage {
			_, err := sut.BuildSyn(testAction, emptyPayload, true)
			Expect(err).ShouldNot(HaveOccurred())

			var syn *ksmsg.KeysplittingMessage
			Expect(sut.Outbox()).Should(Receive(&syn))

			synAck := buildSynAckWithVersion(syn, agentVersion)

			err = sut.Validate(synAck)
			Expect(err).ShouldNot(HaveOccurred())
			return synAck
		}

		// performHandshake completes the keysplitting handshake by sending a Syn
		// and receiving a valid SynAck. Returns the synAck message received.
		performHandshake := func(sut *Keysplitting) *ksmsg.KeysplittingMessage {
			return performHandshakeWithVersion(sut, ksmsg.SchemaVersion)
		}

		assertDataMsgIsCorrect := func(dataMsg *ksmsg.KeysplittingMessage, expectedPayload []byte, expectedPrevMessage *ksmsg.KeysplittingMessage, expectedSchemaVersion string) {
			dataPayload, ok := dataMsg.KeysplittingPayload.(ksmsg.DataPayload)
			Expect(ok).To(BeTrue(), "passed in message must be a Data msg")
			Expect(dataPayload.HPointer).Should(Equal(expectedPrevMessage.Hash()), fmt.Sprintf("This Data msg's HPointer should point to the previously received message: %#v", expectedPrevMessage))
			Expect(dataPayload.ActionPayload).To(Equal(expectedPayload), "The Data's payload should match the expected payload")
			Expect(dataPayload.SchemaVersion).To(Equal(getSchemaVersionAsSemVer(expectedSchemaVersion).String()), "The schema version should match the agreed upon version found in the agent's SynAck")
		}

		// Remove this context when CWC-1820 is resolved
		Context("pipelining is disabled (CWC-1820)", func() {

			When("a Data has been sent and we're waiting for a DataAck", func() {
				var sut *Keysplitting
				var synAck *ksmsg.KeysplittingMessage
				var dataMsg *ksmsg.KeysplittingMessage

				BeforeEach(func() {
					var err error
					sut, err = createSUT()
					Expect(err).ShouldNot(HaveOccurred())

					synAck = performHandshakeWithVersion(sut, prePipeliningVersion)
					dataMsg = sendData(sut, emptyPayload)
				})

				It("creates a valid data message", func() {
					// Payload contains extra quotes because this is pre-pipelining
					assertDataMsgIsCorrect(dataMsg, []byte("\"\""), synAck, prePipeliningVersion)
				})

				It("doesn't send a new Data message until the DataAck is received", func() {
					done := make(chan interface{})
					var dataAck *ksmsg.KeysplittingMessage
					go func() {
						defer GinkgoRecover()

						By("Sending a message that causes Inbox() to block")
						dataSentAfterUnblocking := sendData(sut, emptyPayload)

						// dataAck is initialized after unblocking
						By("Asserting Data is correct after being unblocked")
						assertDataMsgIsCorrect(dataSentAfterUnblocking, []byte("\"\""), dataAck, prePipeliningVersion)

						close(done)
					}()

					// Check that nothing is received on Outbox for some fixed
					// duration
					Consistently(sut.Outbox(), timeToPollNothingReceivedOnOutbox).ShouldNot(Receive(), "no message should be sent to outbox because there is an outstanding DataAck")

					// Validate the DataAck so the goroutine spawned above can
					// unblock and terminate
					dataAck = buildDataAckWithVersion(dataMsg, prePipeliningVersion)
					err := sut.Validate(dataAck)
					Expect(err).ShouldNot(HaveOccurred())

					Eventually(done).Should(BeClosed(), "done should eventually be closed because agent sent DataAck to unblock pipeline")
				})
			})
		})

		Context("pipelining is enabled", func() {
			var sut *Keysplitting

			BeforeEach(func() {
				var err error
				sut, err = createSUT()
				Expect(err).ShouldNot(HaveOccurred())
				performHandshake(sut)
			})

			It("sends Data messages without having received DataAcks for all previous Data messages", func() {
				for i := 0; i <= maxPipelineLimit-1; i++ {
					err := sut.Inbox(testAction, []byte("payload"))
					Expect(err).ShouldNot(HaveOccurred())
				}
				Expect(len(sut.Outbox())).To(Equal(maxPipelineLimit))
			})
		})

		Context("recovery", func() {
			buildErrorMessage := func(hPointer string) rrr.ErrorMessage {
				return rrr.ErrorMessage{
					SchemaVersion: rrr.CurrentVersion,
					Timestamp:     time.Now().Unix(),
					Type:          string(rrr.KeysplittingValidationError),
					Message:       "agent error message",
					HPointer:      hPointer,
				}
			}

			When("recovery handshake does not complete", func() {
				var sut *Keysplitting
				var synMsg *ksmsg.KeysplittingMessage

				BeforeEach(func() {
					var err error
					sut, err = createSUT()
					Expect(err).ShouldNot(HaveOccurred())
					performHandshake(sut)

					// Send Data, so that recovery procedure has
					// something to resend
					dataMsg := sendData(sut, emptyPayload)

					// starting recovery procedure without error
					agentErrorMessage := buildErrorMessage(dataMsg.Hash())
					err = sut.Recover(agentErrorMessage)
					Expect(err).ShouldNot(HaveOccurred())

					// Grab the Syn that Recover() pushes to outbox, so
					// that outbox remains empty for this context
					Expect(sut.Outbox()).Should(Receive(&synMsg))
					Expect(synMsg.Type).Should(Equal(ksmsg.Syn))
				})

				It("cannot send Data", func() {
					done := make(chan interface{})
					go func() {
						defer GinkgoRecover()

						By("Sending a message that causes Inbox() to block because recovery handshake is not complete")
						err := sut.Inbox(testAction, emptyPayload)
						Expect(err).ShouldNot(HaveOccurred())

						close(done)
					}()

					// Check that nothing is received on Outbox for some fixed duration
					Consistently(sut.Outbox(), timeToPollNothingReceivedOnOutbox).ShouldNot(Receive(), "no message should be sent to outbox because the recovery handshake never completed")

					// Complete the handshake by validating a SynAck so
					// the goroutine spawned above can unblock and terminate
					synAck := buildSynAck(synMsg)
					sut.Validate(synAck)

					Eventually(done).Should(BeClosed(), "done should eventually be closed because agent sent a SynAck, which completes the recovery handshake, and should unblock Inbox()")
				})
			})

			When("recovery handshake completes", func() {
				type sentKeysplittingData struct {
					sentPayload []byte
					sentMsg     *ksmsg.KeysplittingMessage
				}

				// Holds *all* payloads and Data messages sent prior to recovery
				var sentData []*sentKeysplittingData
				var amountOfDataMsgsToSend int = 3
				getSentPayloads := func() [][]byte {
					sentPayloads := make([][]byte, 0)
					for _, sentDataMsg := range sentData {
						sentPayloads = append(sentPayloads, sentDataMsg.sentPayload)
					}
					return sentPayloads
				}

				assertRecoveryResendsData := func(sut *Keysplitting, sliceFromIndex int, recoverySynAck *ksmsg.KeysplittingMessage, agentSchemaVersion string) {
					// prevMsg is set after first iteration of for loop below
					var prevMsg *ksmsg.KeysplittingMessage
					for i, payload := range getSentPayloads()[sliceFromIndex:] {
						var dataMsg *ksmsg.KeysplittingMessage
						Expect(sut.Outbox()).Should(Receive(&dataMsg))
						Expect(dataMsg.Type).Should(Equal(ksmsg.Data))

						By(fmt.Sprintf("Asserting Data msg containing payload %q is resent", payload))
						if i == 0 {
							// The first data message points to the recovery
							// syn ack
							assertDataMsgIsCorrect(dataMsg, payload, recoverySynAck, agentSchemaVersion)
						} else {
							// All other data messages point to predicted
							// DataAck for prevMsg
							predictedDataAck := buildDataAckWithVersion(prevMsg, agentSchemaVersion)
							assertDataMsgIsCorrect(dataMsg, payload, predictedDataAck, agentSchemaVersion)
						}

						// Update pointer
						prevMsg = dataMsg
					}

					// There should be no more Data on the outbox
					// because we should have read them all in the for
					// loop above. If there are extra Data messages, it
					// means recovery sent extra payloads that should
					// not have been resent.
					By("Asserting no other Data messages are pushed to the outbox")
					Consistently(sut.Outbox(), timeToPollNothingReceivedOnOutbox).ShouldNot(Receive())
				}

				triggerRecovery := func(sut *Keysplitting) *ksmsg.KeysplittingMessage {
					// Initalize slice to prevent specs from leaking into one another
					sentData = make([]*sentKeysplittingData, 0)

					// Send some Data, so that recovery procedure has something to resend
					for i := 0; i < amountOfDataMsgsToSend; i++ {
						payload := []byte(fmt.Sprintf("Data msg - #%v", i))
						By(fmt.Sprintf("Sending Data(%v)", i))
						dataMsg := sendData(sut, payload)

						sentData = append(sentData, &sentKeysplittingData{
							sentPayload: payload,
							sentMsg:     dataMsg,
						})
					}

					// Build error message that refers to first Data msg
					// sent. There is *no* requirement to have the error
					// message refer to a specific Data message because we
					// also control the SynAck (and nonce) which governs
					// which Data messages to resend. We only need the error
					// message to refer to some Data message that still
					// exists in pipelineMap, so that calling Recover()
					// succeeds without error.
					agentErrorMessage := buildErrorMessage(sentData[0].sentMsg.Hash())
					// Starts the recovery procedure by sending a new Syn
					By("Starting recovery procedure without error")
					err := sut.Recover(agentErrorMessage)
					Expect(err).ShouldNot(HaveOccurred())

					// Recover() sends a Syn
					By("Pushing the Syn message created during recovery to the outbox")
					var recoverySyn *ksmsg.KeysplittingMessage
					Expect(sut.Outbox()).Should(Receive(&recoverySyn))
					Expect(recoverySyn.Type).Should(Equal(ksmsg.Syn))

					return recoverySyn
				}

				When("recovery SynAck's nonce references message not known by daemon's pipelineMap", func() {
					var sut *Keysplitting
					var recoverySynAck *ksmsg.KeysplittingMessage
					var recoverySyn *ksmsg.KeysplittingMessage

					validateDataAckForFirstMsg := func() {
						// This sets hashLastAckedDataMsg, and removes first
						// message from pipeline. Required otherwise, we will
						// hit "Not resending any messages in pipeline because
						// lastAckedData msg is nil" error
						By("Validating DataAck for first message sent")
						dataAck := buildDataAck(sentData[0].sentMsg)
						By("Validating agent's recovery SynAck")
						err := sut.Validate(dataAck)
						Expect(err).ShouldNot(HaveOccurred())
					}

					BeforeEach(func() {
						var err error
						sut, err = createSUT()
						Expect(err).ShouldNot(HaveOccurred())

						performHandshake(sut)
						recoverySyn = triggerRecovery(sut)
					})

					When("nonce is unknown and new agent", func() {
						BeforeEach(func() {
							validateDataAckForFirstMsg()

							By("Building agent's recovery SynAck without error")
							// The default SynAck created by BuildSynAck()
							// uses a random nonce
							recoverySynAck = buildSynAck(recoverySyn)
						})

						It("no Data is resent", func() {
							By("Validating agent's recovery SynAck")
							err := sut.Validate(recoverySynAck)
							Expect(err).ShouldNot(HaveOccurred())

							Consistently(sut.Outbox(), timeToPollNothingReceivedOnOutbox).ShouldNot(Receive(), "because if nonce does not equal hash of last acked Data msg, nothing should be resent")
							Expect(sut.IsPipelineEmpty()).Should(BeTrue(), "because pipeline should be empty as nothing was resent")
						})
					})

					When("nonce is unknown and old agent (CWC-2093)", func() {
						BeforeEach(func() {
							By("Building agent's recovery SynAck with old agent version")
							// The default SynAck created by BuildSynAck()
							// uses a random nonce
							recoverySynAck = buildSynAckWithVersion(recoverySyn, preSynAckNonceChangeVersion)
						})

						// We still resend everything for old agents to
						// preserve backwards compatablity
						//
						// TODO: CWC-2093
						It("Resends all previously pipelined data messages", func() {
							By("Validating agent's recovery SynAck")
							err := sut.Validate(recoverySynAck)
							Expect(err).ShouldNot(HaveOccurred())

							assertRecoveryResendsData(sut, 0, recoverySynAck, preSynAckNonceChangeVersion)
						})
					})

					When("nonce is equal to last valid Data msg", func() {
						BeforeEach(func() {
							validateDataAckForFirstMsg()
						})

						When("agent's recovery SynAck's schema version is the same as the one during handshake", func() {
							BeforeEach(func() {
								By("Building agent's recovery SynAck with nonce equal to last valid Data msg")
								recoverySynAck = buildSynAckWithNonce(recoverySyn, ksmsg.SchemaVersion, sentData[0].sentMsg.Hash())
							})

							// We already received ack for first message.
							// Therefore, we should see all messages after the
							// first one resent
							It("Resends every except the first previously pipelined data messages", func() {
								By("Validating agent's recovery SynAck")
								err := sut.Validate(recoverySynAck)
								Expect(err).ShouldNot(HaveOccurred())

								assertRecoveryResendsData(sut, 1, recoverySynAck, ksmsg.SchemaVersion)
							})
						})

						When("agent's recovery SynAck has a different schema version than before", func() {
							var agentSchemaVersion string

							BeforeEach(func() {
								By("Building agent's recovery SynAck with a different schema version than before and nonce equal to last valid Data msg")
								agentSchemaVersion = ksmsg.SchemaVersion + "-different"
								recoverySynAck = buildSynAckWithNonce(recoverySyn, agentSchemaVersion, sentData[0].sentMsg.Hash())
							})

							It("Resends every except the first previously pipelined data messages", func() {
								By("Validating agent's recovery SynAck")
								err := sut.Validate(recoverySynAck)
								Expect(err).ShouldNot(HaveOccurred())

								assertRecoveryResendsData(sut, 1, recoverySynAck, agentSchemaVersion)
							})
						})
					})
				})

				When("referenced message is first Data sent", func() {
					var sut *Keysplitting
					var recoverySynAck *ksmsg.KeysplittingMessage

					BeforeEach(func() {
						var err error
						sut, err = createSUT()
						Expect(err).ShouldNot(HaveOccurred())
						performHandshake(sut)

						recoverySyn := triggerRecovery(sut)

						By("Building agent's recovery SynAck with nonce equal to first Data message sent")
						recoverySynAck = buildSynAckWithNonce(recoverySyn, ksmsg.SchemaVersion, sentData[0].sentMsg.Hash())

						By("Validating agent's recovery SynAck")
						err = sut.Validate(recoverySynAck)
						Expect(err).ShouldNot(HaveOccurred())
					})

					// Pass index 1 because the first payload (index 0)
					// sent should not be resent
					It("Resends every except the first previously pipelined data messages", func() {
						assertRecoveryResendsData(sut, 1, recoverySynAck, ksmsg.SchemaVersion)
					})
				})

				When("referenced message is last Data sent", func() {
					var sut *Keysplitting

					BeforeEach(func() {
						var err error
						sut, err = createSUT()
						Expect(err).ShouldNot(HaveOccurred())
						performHandshake(sut)

						recoverySyn := triggerRecovery(sut)

						By("Building agent's recovery SynAck with nonce equal to last Data message sent")
						recoverySynAck := buildSynAckWithNonce(recoverySyn, ksmsg.SchemaVersion, sentData[len(sentData)-1].sentMsg.Hash())

						By("Validating agent's recovery SynAck")
						err = sut.Validate(recoverySynAck)
						Expect(err).ShouldNot(HaveOccurred())
					})

					It("no Data is resent", func() {
						Consistently(sut.Outbox(), timeToPollNothingReceivedOnOutbox).ShouldNot(Receive(), "because we resend messages starting with the one immediately after the referenced one")
						Expect(sut.IsPipelineEmpty()).Should(BeTrue(), "because pipeline should be empty as nothing was resent")
					})
				})
			})

			When("recovery fails", func() {
				sendDataAndBuildErrorMessage := func(sut *Keysplitting) rrr.ErrorMessage {
					By("Sending data and building an agent error message")
					dataMsg := sendData(sut, []byte("agent fail on this data message"))
					return buildErrorMessage(dataMsg.Hash())
				}

				When("the agent error message hpointer is empty", func() {
					var sut *Keysplitting
					var err error

					BeforeEach(func() {
						sut, err = createSUT()
						Expect(err).ShouldNot(HaveOccurred())

						performHandshake(sut)

						agentErrorMessage := buildErrorMessage("")
						err = sut.Recover(agentErrorMessage)
					})

					It("doesn't set a syn", func() {
						Consistently(sut.Outbox(), timeToPollNothingReceivedOnOutbox).ShouldNot(Receive())
					})

					It("doesn't go into recovery", func() {
						Expect(sut.Recovering()).Should(BeFalse())
					})

					It("errors", func() {
						Expect(err).Should(HaveOccurred())
					})
				})

				When("the agent error message hpointer refers to message not sent by daemon", func() {
					var sut *Keysplitting

					BeforeEach(func() {
						var err error
						sut, err = createSUT()
						Expect(err).ShouldNot(HaveOccurred())

						performHandshake(sut)

						agentErrorMessage := buildErrorMessage("unknown")
						err = sut.Recover(agentErrorMessage)
						Expect(err).ShouldNot(HaveOccurred())
					})

					It("no Syn message is sent", func() {
						Consistently(sut.Outbox(), timeToPollNothingReceivedOnOutbox).ShouldNot(Receive())
					})

					It("daemon is not recovering", func() {
						Expect(sut.Recovering()).Should(BeFalse())
					})
				})

				When("the daemon is already recovering", func() {
					var sut *Keysplitting

					BeforeEach(func() {
						var err error
						sut, err = createSUT()
						Expect(err).ShouldNot(HaveOccurred())

						performHandshake(sut)
						agentErrorMessage := sendDataAndBuildErrorMessage(sut)

						// Recover once before we call Recover again in
						// JustBeforeEach()
						err = sut.Recover(agentErrorMessage)
						Expect(err).ShouldNot(HaveOccurred())

						// Grab the Syn message from the outbox, so that we
						// can assert no extra Syn is sent in this Context.
						var synMsg *ksmsg.KeysplittingMessage
						Expect(sut.Outbox()).Should(Receive(&synMsg))
						Expect(synMsg.Type).Should(Equal(ksmsg.Syn))

						err = sut.Recover(agentErrorMessage)
						Expect(err).ShouldNot(HaveOccurred())
					})

					It("no Syn message is sent", func() {
						Consistently(sut.Outbox(), timeToPollNothingReceivedOnOutbox).ShouldNot(Receive())
					})

					It("daemon is still recovering", func() {
						Expect(sut.Recovering()).Should(BeTrue())
					})
				})

				When("recovery has already failed the max number of times", func() {
					var sut *Keysplitting
					var err error

					BeforeEach(func() {
						By("Performing handshake")
						sut, err = createSUT()
						Expect(err).ShouldNot(HaveOccurred())
						performHandshake(sut)

						for i := 0; i < maxErrorRecoveryTries; i++ {
							By(fmt.Sprintf("Recover(): #%v", i))
							agentErrorMessage := sendDataAndBuildErrorMessage(sut)
							err := sut.Recover(agentErrorMessage)
							Expect(err).ShouldNot(HaveOccurred())

							By("Pushing the Syn msg to the outbox")
							var synMsg *ksmsg.KeysplittingMessage
							Expect(sut.Outbox()).Should(Receive(&synMsg))
							Expect(synMsg.Type).Should(Equal(ksmsg.Syn))

							// Call Validate() with a SynAck to have
							// recovering boolean reset allowing us to call
							// Recover() again
							synAck := buildSynAck(synMsg)
							err = sut.Validate(synAck)
							Expect(err).ShouldNot(HaveOccurred())
						}

						agentErrorMessage := sendDataAndBuildErrorMessage(sut)
						err = sut.Recover(agentErrorMessage)
					})

					It("no Syn message is sent", func() {
						Consistently(sut.Outbox(), timeToPollNothingReceivedOnOutbox).ShouldNot(Receive())
					})

					It("errors", func() {
						Expect(err).Should(HaveOccurred())
					})
				})
			})
		})
	})
})
