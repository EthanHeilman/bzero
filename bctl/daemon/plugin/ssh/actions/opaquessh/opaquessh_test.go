package opaquessh

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bastionzero.com/bzerolib/bzio"
	"bastionzero.com/bzerolib/filelock"
	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/plugin"
	bzssh "bastionzero.com/bzerolib/plugin/ssh"
	smsg "bastionzero.com/bzerolib/stream/message"
	"bastionzero.com/bzerolib/tests"
)

func TestOpaqueSsh(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Daemon OpaqueSsh Suite")
}

var _ = Describe("Daemon OpaqueSsh action", func() {
	logger := logger.MockLogger(GinkgoWriter)

	identityFilePath := "testIdFile"
	knownHostsFilePath := "testKhFile"
	testData := "testData"
	testOutput := "testOutput"

	var fileLock *filelock.FileLock

	BeforeEach(func() {
		fileLock = filelock.NewFileLock(".test.lock")
	})

	AfterEach(func() {
		fileLock.Cleanup()
	})

	Context("Happy path I: keys exist, closed by agent", func() {

		doneChan := make(chan struct{})
		outboxQueue := make(chan plugin.ActionWrapper, 1)

		mockFileService := bzio.MockBzFileIo{}
		// provide the action this demo (valid) private key
		mockFileService.On("ReadFile", identityFilePath).Return([]byte(tests.DemoPem), nil)
		idFile := bzssh.NewIdentityFile(identityFilePath, mockFileService)

		mockIoService := bzio.MockBzIo{TestData: testData}
		mockIoService.On("Read").Return(len(testData), nil)
		mockIoService.On("Write", []byte(testOutput)).Return(len(testOutput), nil).Times(2)

		// NOTE: we can't make extensive use of the hierarchy here because we're evaluating messages being passed as state changes
		It("passes the SSH request to the agent and starts communicating with the local SSH process", func() {
			s := New(logger, outboxQueue, doneChan, mockIoService, fileLock, idFile, nil)

			By("starting without error")
			err := s.Start()
			Expect(err).To(BeNil())

			By("sending an SshOpen request to the agent")
			openMsg := <-s.outboxQueue
			Expect(string(openMsg.Action)).To(Equal(string(bzssh.SshOpen)))
			var openPayload bzssh.SshOpenMessage
			err = json.Unmarshal(openMsg.ActionPayload, &openPayload)
			Expect(err).To(BeNil())
			// action should have successfully created a public key from the private one
			Expect(string(openPayload.PublicKey)).To(Equal(tests.DemoPub))

			By("sending whatever it reads from SSH to the agent")
			inputMsg := <-s.outboxQueue
			Expect(string(inputMsg.Action)).To(Equal(string(bzssh.SshInput)))
			var inputPayload bzssh.SshInputMessage
			err = json.Unmarshal(inputMsg.ActionPayload, &inputPayload)
			Expect(err).To(BeNil())
			Expect(string(inputPayload.Data)).To(Equal(testData))

			By("writing everything it receives from the agent back to SSH")
			s.ReceiveStream(smsg.StreamMessage{
				Type:    smsg.StdOut,
				Content: base64.StdEncoding.EncodeToString([]byte(testOutput)),
				More:    true,
			})

			s.ReceiveStream(smsg.StreamMessage{
				Type:    smsg.StdOut,
				Content: base64.StdEncoding.EncodeToString([]byte(testOutput)),
				More:    false,
			})

			<-doneChan

			mockFileService.AssertExpectations(GinkgoT())
			mockIoService.AssertExpectations(GinkgoT())
		})
	})

	Context("Happy path II: keys don't exist, closed by user", func() {
		var doneChan chan struct{}
		var outboxQueue chan plugin.ActionWrapper
		var idFile *bzssh.IdentityFile
		var khFile *bzssh.KnownHosts
		var mockFileService bzio.MockBzFileIo
		var mockIoService bzio.MockBzIo

		BeforeEach(func() {
			doneChan = make(chan struct{})
			outboxQueue = make(chan plugin.ActionWrapper, 1)
			mockFileService = bzio.MockBzFileIo{}
			// provide the action an invalid private key -- this will force it to generate a new one
			mockFileService.On("ReadFile", identityFilePath).Return([]byte("invalid key"), nil)
			// ...which we expect to be written out
			mockFileService.On("WriteFile", identityFilePath).Return(nil)
			// also expect a new entry in known_hosts
			tempFilePath := filepath.Join(GinkgoT().TempDir(), "test-known_hosts")
			tempFile, _ := os.Create(tempFilePath)
			mockFileService.On("OpenFile", knownHostsFilePath).Return(tempFile, nil)

			idFile = bzssh.NewIdentityFile(identityFilePath, mockFileService)
			khFile = bzssh.NewKnownHosts(knownHostsFilePath, []string{"testHost"}, mockFileService)

			mockIoService = bzio.MockBzIo{TestData: testData}
			mockIoService.On("Read").Return(0, io.EOF)
		})

		// NOTE: we can't make extensive use of the hierarchy here because we're evaluating messages being passed as state changes
		It("passes the SSH request to the agent and starts communicating with the local SSH process", func() {
			s := New(logger, outboxQueue, doneChan, mockIoService, fileLock, idFile, khFile)

			By("starting without error")
			err := s.Start()
			Expect(err).To(BeNil())

			By("sending an SshOpen request to the agent")
			openMsg := <-s.outboxQueue
			Expect(string(openMsg.Action)).To(Equal(string(bzssh.SshOpen)))
			var openPayload bzssh.SshOpenMessage
			err = json.Unmarshal(openMsg.ActionPayload, &openPayload)
			Expect(err).To(BeNil())
			// can't check the public key's contents but we can make sure it's there
			Expect(len(openPayload.PublicKey)).Should(BeNumerically(">", 0))

			By("writing the remote host key to bastionzero-known_hosts")
			s.ReceiveStream(smsg.StreamMessage{
				Type:    smsg.Data,
				Content: base64.StdEncoding.EncodeToString([]byte(tests.DemoPub)),
				More:    false,
			})

			By("stopping when the user ends the session")
			closeMsg := <-s.outboxQueue
			Expect(string(closeMsg.Action)).To(Equal(string(bzssh.SshClose)))
			var closePayload bzssh.SshCloseMessage
			err = json.Unmarshal(closeMsg.ActionPayload, &closePayload)
			Expect(err).To(BeNil())
			// action should have successfully created a public key from the private one
			Expect(string(closePayload.Reason)).To(Equal(endedByUser))

			<-doneChan

			mockFileService.AssertExpectations(GinkgoT())
			mockIoService.AssertExpectations(GinkgoT())
		})
	})
})
