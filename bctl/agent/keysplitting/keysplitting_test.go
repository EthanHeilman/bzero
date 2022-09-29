package keysplitting_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	agentKs "bastionzero.com/bctl/v1/bctl/agent/keysplitting"
	"bastionzero.com/bctl/v1/bctl/agent/keysplitting/mocks"
	"bastionzero.com/bctl/v1/bzerolib/keysplitting/bzcert"
	ksmsg "bastionzero.com/bctl/v1/bzerolib/keysplitting/message"
	"bastionzero.com/bctl/v1/bzerolib/keysplitting/util"
	"bastionzero.com/bctl/v1/bzerolib/tests"

	"github.com/Masterminds/semver"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

func TestAgentKeysplitting(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agent keysplitting suite")
}

var _ = Describe("Agent keysplitting", func() {
	var sut *agentKs.Keysplitting

	var agentKeypair *tests.Ed25519KeyPair
	var daemonKeypair *tests.Ed25519KeyPair

	var mockAgentKeysplittingConfig *mocks.IKeysplittingConfig

	var daemonSchemaVersion string
	var daemonBZCert *bzcert.BZCert
	const testAction string = "test/action"

	GetDaemonSchemaVersionAsSemVer := func() *semver.Version {
		parsedSchemaVersion, err := semver.NewVersion(daemonSchemaVersion)
		Expect(err).ShouldNot(HaveOccurred())
		return parsedSchemaVersion
	}

	// Helper build daemon message funcs
	BuildSynWithPayload := func(payload []byte) *ksmsg.KeysplittingMessage {
		// Build the keysplitting message
		synPayload := ksmsg.SynPayload{
			SchemaVersion: GetDaemonSchemaVersionAsSemVer().String(),
			Type:          string(ksmsg.Syn),
			Action:        testAction,
			ActionPayload: payload,
			TargetId:      agentKeypair.Base64EncodedPublicKey,
			Nonce:         util.Nonce(),
			BZCert:        *daemonBZCert,
		}

		return &ksmsg.KeysplittingMessage{
			Type:                ksmsg.Syn,
			KeysplittingPayload: synPayload,
		}
	}
	BuildSyn := func() *ksmsg.KeysplittingMessage {
		return BuildSynWithPayload([]byte{})
	}
	BuildDataWithPayload := func(ackMsg *ksmsg.KeysplittingMessage, payload []byte) *ksmsg.KeysplittingMessage {
		bzcertHash, err := daemonBZCert.HashCert()
		Expect(err).ShouldNot(HaveOccurred())

		dataMsg, err := ackMsg.BuildUnsignedData(
			testAction,
			payload,
			bzcertHash,
			GetDaemonSchemaVersionAsSemVer().String(),
		)
		Expect(err).ShouldNot(HaveOccurred())
		return &dataMsg
	}
	BuildData := func(ackMsg *ksmsg.KeysplittingMessage) *ksmsg.KeysplittingMessage {
		return BuildDataWithPayload(ackMsg, []byte{})
	}
	SignDaemonMsg := func(daemonMsg *ksmsg.KeysplittingMessage) {
		err := daemonMsg.Sign(daemonKeypair.Base64EncodedPrivateKey)
		Expect(err).ShouldNot(HaveOccurred())
	}
	// Use this helper method to quickly validate messages, so another message
	// can be received. Please prefer to call Validate() directly (and not use
	// this function) when the It() is explictly asserting validation.
	ValidateDaemonMsg := func(daemonMsg *ksmsg.KeysplittingMessage) {
		err := sut.Validate(daemonMsg)
		Expect(err).ShouldNot(HaveOccurred())
	}

	BuildSynAndValidate := func() *ksmsg.KeysplittingMessage {
		synMsg := BuildSyn()
		SignDaemonMsg(synMsg)
		ValidateDaemonMsg(synMsg)
		return synMsg
	}

	BeforeEach(func() {
		// Setup keypairs to use for agent and daemon
		var err error
		agentKeypair, err = tests.GenerateEd25519Key()
		GinkgoWriter.Printf("Agent keypair: Private key: %v; Public key: %v\n", agentKeypair.Base64EncodedPrivateKey, agentKeypair.Base64EncodedPublicKey)
		Expect(err).ShouldNot(HaveOccurred())
		daemonKeypair, err = tests.GenerateEd25519Key()
		GinkgoWriter.Printf("Daemon keypair: Private key: %v; Public key: %v\n", daemonKeypair.Base64EncodedPrivateKey, daemonKeypair.Base64EncodedPublicKey)
		Expect(err).ShouldNot(HaveOccurred())

		// Set schema version to use when building daemon messages
		daemonSchemaVersion = ksmsg.SchemaVersion

		// Set daemon's BZCert to use throughout the test. An individual test
		// (or context) can modify this to test something other than the default
		// behavior configured here.
		//
		// We only need to set ClientPublicKey to have a valid BZCert because
		// that is the only field the agent uses after the verifier successfully
		// validates.
		daemonBZCert = &bzcert.BZCert{
			ClientPublicKey: daemonKeypair.Base64EncodedPublicKey,
		}

		// Setup common mocks here
		mockAgentKeysplittingConfig = &mocks.IKeysplittingConfig{}

		// Configure default behavior for mocks here. An individual test (or
		// context) can clear these by setting mock.ExpectedCalls to nil
		mockAgentKeysplittingConfig.On("GetPublicKey").Return(agentKeypair.Base64EncodedPublicKey)
		mockAgentKeysplittingConfig.On("GetPrivateKey").Return(agentKeypair.Base64EncodedPrivateKey)
	})

	AfterEach(func() {
		mockAgentKeysplittingConfig.AssertExpectations(GinkgoT())
	})

	Context("Build agent acks", func() {
		var agentSchemaVersion string
		var expectedSchemaVersion string
		var mockBZCertVerifier *mocks.BZCertVerifier

		BeforeEach(func() {
			// All tests in this Context need the BZCertVerifier to always
			// validate
			mockBZCertVerifier = &mocks.BZCertVerifier{}

			// Create a verified BzCert
			//
			// Set expiration time to to the future, so tests don't fail stating
			// the BzCert is expired
			verifiedBZCert, err := bzcert.NewVerifiedBZCert(daemonBZCert, time.Now().Add(1*time.Hour))
			Expect(err).ShouldNot(HaveOccurred())

			// And return it when Verify is called with the test suite's
			// preconfigured, daemon BzCert
			mockBZCertVerifier.On("Verify", daemonBZCert).Return(verifiedBZCert, nil)
		})

		CommonAssertBehavior := func() {
			var daemonMsg *ksmsg.KeysplittingMessage

			When("the daemon message is a Syn", func() {
				var validatedDataMessage *ksmsg.KeysplittingMessage

				BeforeEach(func() {
					// Must re-init, so parallel specs don't leak into each
					// other
					validatedDataMessage = nil
				})

				AssertBehavior := func() {
					It("SynAck is built correctly", func() {
						payload := []byte{}
						synAck, err := sut.BuildAck(daemonMsg, testAction, payload)
						Expect(err).ShouldNot(HaveOccurred())

						By("Asserting the keysplitting message is correct")
						Expect(synAck.Type).To(Equal(ksmsg.SynAck))
						Expect(synAck.Signature).NotTo(BeEmpty())
						synAckPayload, ok := synAck.KeysplittingPayload.(ksmsg.SynAckPayload)
						Expect(ok).To(BeTrue())

						By("Asserting the keysplitting message payload details are correct")
						Expect(synAckPayload.SchemaVersion).To(Equal(expectedSchemaVersion))
						Expect(synAckPayload.Type).To(BeEquivalentTo(ksmsg.SynAck))
						Expect(synAckPayload.Action).To(Equal(testAction))
						Expect(synAckPayload.ActionResponsePayload).To(Equal(payload))
						Expect(synAckPayload.Timestamp).NotTo(BeEmpty())
						Expect(synAckPayload.TargetPublicKey).To(Equal(agentKeypair.Base64EncodedPublicKey))
						if validatedDataMessage == nil {
							Expect(synAckPayload.Nonce).NotTo(BeEmpty())
						} else {
							// The RSynAck's nonce is defined to equal the
							// hash of the last validated Data messaage in
							// order to preserve the hash chain.
							Expect(synAckPayload.Nonce).Should(Equal(validatedDataMessage.Hash()), "because the hash chain should be maintained when data has already been validated")
						}
						Expect(synAckPayload.HPointer).Should(Equal(daemonMsg.Hash()), fmt.Sprintf("The HPointer should point to the daemon message that was validated: %#v", daemonMsg))

						By("Asserting the message signature validates")
						Expect(synAck.VerifySignature(agentKeypair.Base64EncodedPublicKey)).ShouldNot(HaveOccurred())
					})
				}

				When("no data messages have been validated", func() {
					BeforeEach(func() {
						daemonMsg = BuildSynAndValidate()
					})

					AssertBehavior()
				})

				When("one data message has been validated", func() {
					BeforeEach(func() {
						validatedSynMessage := BuildSynAndValidate()

						// Build Data message and validate
						By("Building SynAck without error")
						var err error
						synAck, err := sut.BuildAck(validatedSynMessage, testAction, []byte{})
						Expect(err).ShouldNot(HaveOccurred())
						By("Building Data message without error")
						dataMsg := BuildData(&synAck)
						SignDaemonMsg(dataMsg)
						By("Validating Data message without error")
						err = sut.Validate(dataMsg)
						Expect(err).ShouldNot(HaveOccurred())

						daemonMsg = BuildSynAndValidate()
						validatedDataMessage = dataMsg
					})

					AssertBehavior()
				})
			})

			When("the daemon message is a Data", func() {
				BeforeEach(func() {
					// We must validate a Syn message before we can build an ack
					// for Data, otherwise daemonSchemaVersion will be nil
					validatedSyn := BuildSynAndValidate()

					// We need some Ack (SynAck in this case) in order to build
					// Data
					synAck, err := sut.BuildAck(validatedSyn, testAction, []byte{})
					Expect(err).ShouldNot(HaveOccurred())

					daemonMsg = BuildData(&synAck)
				})

				It("DataAck is built correctly", func() {
					payload := []byte{}
					dataAck, err := sut.BuildAck(daemonMsg, testAction, payload)
					Expect(err).ShouldNot(HaveOccurred())

					By("Asserting the keysplitting message is correct")
					Expect(dataAck.Type).To(Equal(ksmsg.DataAck))
					Expect(dataAck.Signature).NotTo(BeEmpty())
					dataAckPayload, ok := dataAck.KeysplittingPayload.(ksmsg.DataAckPayload)
					Expect(ok).To(BeTrue())

					By("Asserting the keysplitting message payload details are correct")
					Expect(dataAckPayload.SchemaVersion).To(Equal(expectedSchemaVersion))
					Expect(dataAckPayload.Type).To(BeEquivalentTo(ksmsg.DataAck))
					Expect(dataAckPayload.Action).To(Equal(testAction))
					Expect(dataAckPayload.Timestamp).To(Equal(""))
					Expect(dataAckPayload.TargetPublicKey).To(Equal(agentKeypair.Base64EncodedPublicKey))
					Expect(dataAckPayload.HPointer).Should(Equal(daemonMsg.Hash()), fmt.Sprintf("The HPointer should point to the daemon message that was validated: %#v", daemonMsg))
					Expect(dataAckPayload.ActionResponsePayload).To(Equal(payload))

					By("Asserting the message signature validates")
					Expect(dataAck.VerifySignature(agentKeypair.Base64EncodedPublicKey)).ShouldNot(HaveOccurred())
				})
			})
		}

		When("daemon schema version is less than agent schema version", func() {
			BeforeEach(func() {
				daemonSchemaVersion = "1.0.0"
				agentSchemaVersion = "2.0.0"

				expectedSchemaVersion = daemonSchemaVersion

				// Init the SUT with the agent schema version
				var err error
				sut, err = agentKs.New(agentKs.KeysplittingParameters{Config: mockAgentKeysplittingConfig, SchemaVersion: agentSchemaVersion, Verifier: mockBZCertVerifier})
				Expect(err).ShouldNot(HaveOccurred())
			})

			CommonAssertBehavior()
		})

		When("daemon schema version is not less than agent schema version", func() {
			BeforeEach(func() {
				daemonSchemaVersion = "2.1.0"
				agentSchemaVersion = "2.0.0"

				expectedSchemaVersion = agentSchemaVersion

				// Init the SUT with the agent schema version
				var err error
				sut, err = agentKs.New(agentKs.KeysplittingParameters{Config: mockAgentKeysplittingConfig, SchemaVersion: agentSchemaVersion, Verifier: mockBZCertVerifier})
				Expect(err).ShouldNot(HaveOccurred())
			})

			CommonAssertBehavior()
		})
	})

	Context("Validate daemon messages", func() {
		var msgUnderTest *ksmsg.KeysplittingMessage
		var mockBZCertVerifier *mocks.BZCertVerifier

		AssertBehavior := func() {
			It("validate succeeds when the message is signed", func() {
				By(fmt.Sprintf("Signing %v without error", msgUnderTest.Type))
				SignDaemonMsg(msgUnderTest)

				By("Validating without error")
				err := sut.Validate(msgUnderTest)
				Expect(err).ShouldNot(HaveOccurred())
			})
		}

		BeforeEach(func() {
			// Reset mock per test
			mockBZCertVerifier = &mocks.BZCertVerifier{}

			// Init the SUT
			var err error
			sut, err = agentKs.New(agentKs.KeysplittingParameters{Config: mockAgentKeysplittingConfig, Verifier: mockBZCertVerifier})
			Expect(err).ShouldNot(HaveOccurred())
		})

		When("the message is the wrong type", func() {
			var validateError error

			AssertFailedBehavior := func() {
				It("errors", func() {
					Expect(validateError).Should(HaveOccurred())
				})
			}

			JustBeforeEach(func() {
				validateError = sut.Validate(msgUnderTest)
			})

			When("the message is a SynAck", func() {
				BeforeEach(func() {
					msgUnderTest = &ksmsg.KeysplittingMessage{Type: ksmsg.SynAck}
				})

				AssertFailedBehavior()
			})

			When("the message is a DataAck", func() {
				BeforeEach(func() {
					msgUnderTest = &ksmsg.KeysplittingMessage{Type: ksmsg.DataAck}
				})

				AssertFailedBehavior()
			})
		})

		When("the message is a Data-->SynAck-->Syn", func() {
			var synMsg *ksmsg.KeysplittingMessage
			var bzCertExpirationTime time.Time

			BeforeEach(func() {
				// Reset bzCertExpirationTime to default
				bzCertExpirationTime = time.Now().Add(1 * time.Hour)
			})

			// Common setup behavior for all tests in this When block
			SetupBehavior := func() {
				// Create a verified BzCert
				verifiedBZCert, err := bzcert.NewVerifiedBZCert(daemonBZCert, bzCertExpirationTime)
				Expect(err).ShouldNot(HaveOccurred())

				// And return it when Verify is called with the test suite's
				// preconfigured, daemon BzCert
				mockBZCertVerifier.On("Verify", daemonBZCert).Return(verifiedBZCert, nil)

				// We must build a Syn, validate it, and build a SynAck, so that
				// we can send Data successfully
				By("Building and validating a Syn message without error")
				synMsg = BuildSynAndValidate()
				// Sets expected HPointer which our Data must set correctly in
				// order to validate
				synAck, err := sut.BuildAck(synMsg, testAction, []byte{})
				Expect(err).ShouldNot(HaveOccurred(), "because we should be able to build a SynAck from a valid Syn message")

				msgUnderTest = BuildData(&synAck)
			}

			When("the happy path", func() {
				BeforeEach(func() {
					SetupBehavior()
					// There is nothing extra to configure
				})

				AssertBehavior()
			})

			When("failure modes", func() {
				var validateError error

				AssertFailedBehavior := func() {
					It("RSynAck nonce should not refer to invalid Data message", func() {
						By("Building RSynAck without error")
						synAck, err := sut.BuildAck(synMsg, testAction, []byte{})
						Expect(err).ShouldNot(HaveOccurred())

						invalidDataMsgHash := msgUnderTest.Hash()

						Expect(synAck.KeysplittingPayload.(ksmsg.SynAckPayload).Nonce).ShouldNot(Equal(invalidDataMsgHash), "because if the Data message failed to validate, the RSynAck's nonce should not refer to an invalid Data message")
					})
				}

				JustBeforeEach(func() {
					validateError = sut.Validate(msgUnderTest)
				})

				When("the BZCert hash does not match the agent's stored BZCert hash", func() {
					BeforeEach(func() {
						SetupBehavior()

						By("Modifying BZCert hash not to match")
						dataPayload, _ := msgUnderTest.KeysplittingPayload.(ksmsg.DataPayload)
						dataPayload.BZCertHash = "does not match"
						msgUnderTest.KeysplittingPayload = dataPayload

						By(fmt.Sprintf("Signing %v without error", msgUnderTest.Type))
						SignDaemonMsg(msgUnderTest)
					})

					AssertFailedBehavior()

					It("errors", func() {
						Expect(validateError).Should(MatchError(agentKs.ErrBZCertMismatch))
					})
				})

				When("the message is unsigned", func() {
					BeforeEach(func() {
						SetupBehavior()
						msgUnderTest.Signature = ""
					})

					AssertFailedBehavior()

					It("errors", func() {
						Expect(validateError).Should(MatchError(agentKs.ErrInvalidSignature))
					})
				})

				When("the BZCert has expired", func() {
					BeforeEach(func() {
						// Set expiration time to the past
						bzCertExpirationTime = time.Now().Add(-1 * time.Hour)
						SetupBehavior()

						By(fmt.Sprintf("Signing %v without error", msgUnderTest.Type))
						SignDaemonMsg(msgUnderTest)
					})

					AssertFailedBehavior()

					It("errors", func() {
						Expect(validateError).Should(MatchError(agentKs.ErrBZCertExpired))
					})
				})

				When("the HPointer points to the wrong message", func() {
					BeforeEach(func() {
						SetupBehavior()

						By("Modifying HPointer to point to the wrong message")
						dataPayload, _ := msgUnderTest.KeysplittingPayload.(ksmsg.DataPayload)
						dataPayload.HPointer = "wrong message hash"
						msgUnderTest.KeysplittingPayload = dataPayload

						By(fmt.Sprintf("Signing %v without error", msgUnderTest.Type))
						SignDaemonMsg(msgUnderTest)
					})

					AssertFailedBehavior()

					It("errors", func() {
						Expect(validateError).Should(MatchError(agentKs.ErrUnexpectedHPointer))
					})
				})
			})
		})

		When("the message is a Syn", func() {
			BeforeEach(func() {
				By("Building a Syn message without error")
				msgUnderTest = BuildSyn()

				// Create a verified BzCert
				verifiedBZCert, err := bzcert.NewVerifiedBZCert(daemonBZCert, time.Now().Add(1*time.Hour))
				Expect(err).ShouldNot(HaveOccurred())

				// And return it when Verify is called with the test suite's
				// preconfigured, daemon BzCert
				mockBZCertVerifier.On("Verify", daemonBZCert).Return(verifiedBZCert, nil)
			})

			When("the happy path", func() {
				// There is nothing extra to setup
				AssertBehavior()

				// Remove this test once CWC-1553 is addressed
				It("validate succeeds when the message is signed by a legacy daemon (CWC-1553)", func() {
					By("Modifying schema version to be invalid")
					synPayload, _ := msgUnderTest.KeysplittingPayload.(ksmsg.SynPayload)
					// Change schema version to version prior to targetId check
					synPayload.SchemaVersion = "1.0"
					synPayload.TargetId = "does not match"
					msgUnderTest.KeysplittingPayload = synPayload

					By(fmt.Sprintf("Signing %v without error", msgUnderTest.Type))
					SignDaemonMsg(msgUnderTest)

					By("Validating without error")
					err := sut.Validate(msgUnderTest)
					Expect(err).ShouldNot(HaveOccurred())
				})
			})

			When("failure modes", func() {
				var validateError error

				AssertFailedBehavior := func() {
					It("cannot validate Data messages", func() {
						By("Building SynAck so that we can build a Data message")
						synAck, err := msgUnderTest.BuildUnsignedSynAck([]byte{}, agentKeypair.Base64EncodedPublicKey, util.Nonce(), ksmsg.SchemaVersion)
						Expect(err).ShouldNot(HaveOccurred())
						dataMsg := BuildData(&synAck)

						err = sut.Validate(dataMsg)
						Expect(err).Should(HaveOccurred(), "because if the Syn failed to validate, then the agent should refuse to accept Data messages as the handshake never completed")
					})
				}

				JustBeforeEach(func() {
					validateError = sut.Validate(msgUnderTest)
				})

				When("the BZCert is invalid", func() {
					var bzCertVerifierError error

					BeforeEach(func() {
						// Reset the mock for this context because it already
						// has an expected call defined in an outer context
						mockBZCertVerifier.ExpectedCalls = nil
						bzCertVerifierError = errors.New("BZCert error")
						mockBZCertVerifier.On("Verify", mock.Anything).Return(nil, bzCertVerifierError)
					})

					AssertFailedBehavior()

					It("errors", func() {
						Expect(validateError).Should(MatchError(bzCertVerifierError))
					})
				})

				When("the message is unsigned", func() {
					BeforeEach(func() {
						msgUnderTest.Signature = ""
					})

					AssertFailedBehavior()

					It("errors", func() {
						Expect(validateError).Should(MatchError(agentKs.ErrInvalidSignature))
					})
				})

				When("the schema version cannot be parsed", func() {
					BeforeEach(func() {
						By("Modifying schema version to be invalid")
						synPayload, _ := msgUnderTest.KeysplittingPayload.(ksmsg.SynPayload)
						synPayload.SchemaVersion = "bad-version"
						msgUnderTest.KeysplittingPayload = synPayload

						By(fmt.Sprintf("Signing %v without error", msgUnderTest.Type))
						SignDaemonMsg(msgUnderTest)
					})

					AssertFailedBehavior()

					It("errors", func() {
						Expect(validateError).Should(MatchError(agentKs.ErrFailedToParseVersion))
					})
				})

				When("the target ID does not match the agent's public key", func() {
					BeforeEach(func() {
						By("Modifying target ID to not match the agent's public key")
						synPayload, _ := msgUnderTest.KeysplittingPayload.(ksmsg.SynPayload)
						synPayload.TargetId = "does not match"
						msgUnderTest.KeysplittingPayload = synPayload

						By(fmt.Sprintf("Signing %v without error", msgUnderTest.Type))
						SignDaemonMsg(msgUnderTest)
					})

					AssertFailedBehavior()

					It("errors", func() {
						Expect(validateError).Should(MatchError(agentKs.ErrTargetIdMismatch))
					})
				})
			})
		})
	})
})
