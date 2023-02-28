package transparentssh

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gossh "golang.org/x/crypto/ssh"

	"bastionzero.com/bzerolib/bzio"
	"bastionzero.com/bzerolib/filelock"
	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/plugin"
	bzssh "bastionzero.com/bzerolib/plugin/ssh"
	smsg "bastionzero.com/bzerolib/stream/message"
	"bastionzero.com/bzerolib/tests"
)

func startSession(t *TransparentSsh, port string, config *gossh.ClientConfig) (*gossh.Client, *gossh.Session) {
	By("starting without error")
	err := t.Start()
	Expect(err).To(BeNil())

	By("executing the SSH handshake")
	conn, err := gossh.Dial("tcp", fmt.Sprintf("localhost:%s", port), config)
	Expect(err).To(BeNil())

	session, err := conn.NewSession()
	Expect(err).To(BeNil())

	return conn, session
}

// provide pipes for two-way communication with the server
func setupIo(session *gossh.Session) (io.WriteCloser, chan []byte, chan []byte) {
	stdout, err := session.StdoutPipe()
	Expect(err).To(BeNil())
	stderr, err := session.StderrPipe()
	Expect(err).To(BeNil())
	stdin, err := session.StdinPipe()
	Expect(err).To(BeNil())

	stdoutChan := make(chan []byte)
	stderrChan := make(chan []byte)

	go readPipe(stdout, stdoutChan)
	go readPipe(stderr, stderrChan)

	return stdin, stdoutChan, stderrChan
}

func readPipe(pipe io.Reader, outputChan chan []byte) {
	b := make([]byte, 100)
	for {
		if n, err := pipe.Read(b); err != nil {
			return
		} else if n > 0 {
			outputChan <- b[:n]
		}
	}
}

func safeListen(port string) net.Listener {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	Expect(err).To(BeNil())
	return listener
}

func TestTransparentSsh(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Daemon TransparentSsh Suite")
}

var _ = Describe("Daemon TransparentSsh action", func() {
	logger := logger.MockLogger(GinkgoWriter)
	identityFilePath := "testIdFile"
	knownHostsFilePath := "testKhFile"
	testData := "testData"

	var conn *gossh.Client
	var session *gossh.Session
	var listener net.Listener

	var doneChan chan struct{}
	var outboxQueue chan plugin.ActionWrapper
	var config *gossh.ClientConfig
	var fileLock *filelock.FileLock
	var mockFileService bzio.MockBzFileIo
	var mockIoService bzio.MockBzIo
	var idFile *bzssh.IdentityFile
	var khFile *bzssh.KnownHosts

	BeforeEach(func() {
		doneChan = make(chan struct{})
		outboxQueue = make(chan plugin.ActionWrapper, 1)

		fileLock = filelock.NewFileLock(".test.lock")

		privateBytes, _, err := bzssh.GenerateKeys()
		Expect(err).To(BeNil())
		signer, _ := gossh.ParsePrivateKey(privateBytes)
		config = &gossh.ClientConfig{
			User:            "testUser",
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
			Auth: []gossh.AuthMethod{
				gossh.PublicKeys(signer),
			},
		}

		mockFileService = bzio.MockBzFileIo{}
	})

	AfterEach(func() {
		fileLock.Cleanup()
		conn.Close()
		session.Close()
	})

	Context("rejects unauthorized requests", func() {
		When("it receives an invalid exec request", func() {
			badScp := "scpfake"
			port := "22220"
			badScpErrMsg := bzssh.UnauthorizedCommandError(fmt.Sprintf("'%s'", badScp))

			BeforeEach(func() {
				// provide the action this demo (valid) private key
				mockFileService.On("ReadFile", identityFilePath).Return([]byte(tests.DemoPem), nil)
				idFile = bzssh.NewIdentityFile(identityFilePath, mockFileService)
				// also expect a new entry in known_hosts
				tempFilePath := filepath.Join(GinkgoT().TempDir(), "test-known_hosts")
				tempFile, _ := os.Create(tempFilePath)
				mockFileService.On("OpenFile", knownHostsFilePath).Return(tempFile, nil)
				khFile = bzssh.NewKnownHosts(knownHostsFilePath, []string{"testHost"}, mockFileService)

				mockIoService = bzio.MockBzIo{TestData: testData}
				mockIoService.On("Write", []byte(readyMsg)).Return(len(readyMsg), nil)
				mockIoService.On("WriteErr", []byte(badScpErrMsg)).Return(len(badScpErrMsg), nil)
			})

			It("rejects", func() {
				listener = safeListen(port)
				t := New(logger, outboxQueue, doneChan, mockIoService, listener, fileLock, idFile, khFile)
				conn, session = startSession(t, port, config)

				By("rejecting the invalid request")
				ok, err := session.SendRequest("exec", true, []byte(fmt.Sprintf("\u0000\u0000\u0000\u0007%s", badScp)))
				Expect(err).To(Equal(io.EOF))
				Expect(ok).To(BeFalse())

				mockFileService.AssertExpectations(GinkgoT())
				mockIoService.AssertExpectations(GinkgoT())
			})
		})

		When("it receives an invalid subystem request", func() {
			badSftp := "sftpfake"
			port := "22221"
			badSftpErrMsg := bzssh.UnauthorizedCommandError(fmt.Sprintf("'%s'", badSftp))

			BeforeEach(func() {
				// provide the action this demo (valid) private key
				mockFileService.On("ReadFile", identityFilePath).Return([]byte(tests.DemoPem), nil)
				idFile = bzssh.NewIdentityFile(identityFilePath, mockFileService)
				// also expect a new entry in known_hosts
				tempFilePath := filepath.Join(GinkgoT().TempDir(), "test-known_hosts")
				tempFile, _ := os.Create(tempFilePath)
				mockFileService.On("OpenFile", knownHostsFilePath).Return(tempFile, nil)
				khFile = bzssh.NewKnownHosts(knownHostsFilePath, []string{"testHost"}, mockFileService)

				mockIoService = bzio.MockBzIo{TestData: testData}
				mockIoService.On("Write", []byte(readyMsg)).Return(len(readyMsg), nil)
				mockIoService.On("WriteErr", []byte(badSftpErrMsg)).Return(len(badSftpErrMsg), nil)
			})

			It("rejects", func() {
				listener = safeListen(port)
				t := New(logger, outboxQueue, doneChan, mockIoService, listener, fileLock, idFile, khFile)
				conn, session = startSession(t, port, config)

				By("rejecting the invalid request")
				err := session.RequestSubsystem(badSftp)
				Expect(err).To(Equal(io.EOF))

				mockFileService.AssertExpectations(GinkgoT())
				mockIoService.AssertExpectations(GinkgoT())
			})
		})

		When("it receives any shell request", func() {
			port := "22222"
			shellReqErrMsg := bzssh.UnauthorizedCommandError("shell request")

			BeforeEach(func() {
				// provide the action this demo (valid) private key
				mockFileService.On("ReadFile", identityFilePath).Return([]byte(tests.DemoPem), nil)
				idFile = bzssh.NewIdentityFile(identityFilePath, mockFileService)
				// also expect a new entry in known_hosts
				tempFilePath := filepath.Join(GinkgoT().TempDir(), "test-known_hosts")
				tempFile, _ := os.Create(tempFilePath)
				mockFileService.On("OpenFile", knownHostsFilePath).Return(tempFile, nil)
				khFile = bzssh.NewKnownHosts(knownHostsFilePath, []string{"testHost"}, mockFileService)

				mockIoService = bzio.MockBzIo{TestData: testData}
				mockIoService.On("Write", []byte(readyMsg)).Return(len(readyMsg), nil)
				mockIoService.On("WriteErr", []byte(shellReqErrMsg)).Return(len(shellReqErrMsg), nil)
			})

			It("rejects", func() {
				listener = safeListen(port)
				t := New(logger, outboxQueue, doneChan, mockIoService, listener, fileLock, idFile, khFile)
				conn, session = startSession(t, port, config)

				By("rejecting the invalid request")
				ok, err := session.SendRequest("shell", true, []byte("\u0000\u0000\u0000\u000exterm-256color"))
				Expect(err).To(Equal(io.EOF))
				Expect(ok).To(BeFalse())

				mockFileService.AssertExpectations(GinkgoT())
				mockIoService.AssertExpectations(GinkgoT())
			})
		})
	})

	Context("Happy paths", func() {
		agentReply := "testAgentReply"
		channelInput := "testChannelInput"

		When("Keys exist - scp - stdout - upload", func() {
			scp := "scp -t testFile.txt"
			port := "22223"

			BeforeEach(func() {
				// provide the action this demo (valid) private key
				mockFileService.On("ReadFile", identityFilePath).Return([]byte(tests.DemoPem), nil)
				idFile = bzssh.NewIdentityFile(identityFilePath, mockFileService)
				// also expect a new entry in known_hosts
				tempFilePath := filepath.Join(GinkgoT().TempDir(), "test-known_hosts")
				tempFile, _ := os.Create(tempFilePath)
				mockFileService.On("OpenFile", knownHostsFilePath).Return(tempFile, nil)
				khFile = bzssh.NewKnownHosts(knownHostsFilePath, []string{"testHost"}, mockFileService)

				mockIoService = bzio.MockBzIo{TestData: testData}
				mockIoService.On("Write", []byte(readyMsg)).Return(len(readyMsg), nil)
			})

			It("handles the request from start to finish", func() {
				listener = safeListen(port)
				t := New(logger, outboxQueue, doneChan, mockIoService, listener, fileLock, idFile, khFile)
				conn, session = startSession(t, port, config)

				By("sending an open message to the agent")
				openMessage := <-outboxQueue
				Expect(openMessage.Action).To(Equal(string(bzssh.SshOpen)))
				var openPayload bzssh.SshOpenMessage
				err := json.Unmarshal(openMessage.ActionPayload, &openPayload)
				Expect(err).To(BeNil())

				By("sending a valid exec command to the agent")
				// NOTE: don't forget that unicode codes are base-16, so to express "19" here, we use u+0013
				ok, err := session.SendRequest("exec", true, []byte(fmt.Sprintf("\u0000\u0000\u0000\u0013%s", scp)))
				Expect(err).To(BeNil())
				Expect(ok).To(BeTrue())

				execMessage := <-outboxQueue
				Expect(execMessage.Action).To(Equal(string(bzssh.SshExec)))
				var execPayload bzssh.SshExecMessage
				json.Unmarshal(execMessage.ActionPayload, &execPayload)
				Expect(execPayload.Command).To(Equal(scp))

				By("writing the agent's response to the ssh channel's stdout")
				messageContent := base64.StdEncoding.EncodeToString([]byte(agentReply))
				t.ReceiveStream(smsg.StreamMessage{
					Type:    smsg.StdOut,
					Content: messageContent,
					More:    true,
				})

				stdin, stdoutChan, _ := setupIo(session)
				output := <-stdoutChan
				Expect(string(output)).To(Equal(agentReply))

				By("sending the channel's input to the agent")
				_, err = stdin.Write([]byte(channelInput))
				Expect(err).To(BeNil())

				inputMessage := <-outboxQueue
				Expect(inputMessage.Action).To(Equal(string(bzssh.SshInput)))
				var inputPayload bzssh.SshInputMessage
				json.Unmarshal(inputMessage.ActionPayload, &inputPayload)
				Expect(string(inputPayload.Data)).To(Equal(channelInput))

				By("closing when the local ssh process ends")
				err = stdin.Close()
				Expect(err).To(BeNil())

				closeMessage := <-outboxQueue
				Expect(closeMessage.Action).To(Equal(string(bzssh.SshClose)))
				var closePayload bzssh.SshCloseMessage
				err = json.Unmarshal(closeMessage.ActionPayload, &closePayload)
				Expect(err).To(BeNil())

				mockFileService.AssertExpectations(GinkgoT())
				mockIoService.AssertExpectations(GinkgoT())
			})
		})

		When("keys don't exist - sftp - stderr - download", func() {
			sftp := "sftp"
			port := "22224"

			BeforeEach(func() {
				// provide the action an invalid private key -- this will force it to generate a new one...
				mockFileService.On("ReadFile", identityFilePath).Return([]byte("invalid key"), nil)
				// ...which we expect to be written out
				mockFileService.On("WriteFile", identityFilePath).Return(nil)
				idFile = bzssh.NewIdentityFile(identityFilePath, mockFileService)
				// also expect a new entry in known_hosts
				tempFilePath := filepath.Join(GinkgoT().TempDir(), "test-known_hosts")
				tempFile, _ := os.Create(tempFilePath)
				mockFileService.On("OpenFile", knownHostsFilePath).Return(tempFile, nil)
				khFile = bzssh.NewKnownHosts(knownHostsFilePath, []string{"testHost"}, mockFileService)

				// we will receive a ready message upon startup
				mockIoService = bzio.MockBzIo{TestData: testData}
				mockIoService.On("Write", []byte(readyMsg)).Return(len(readyMsg), nil)
			})

			It("handles the request from start to finish", func() {
				listener = safeListen(port)
				t := New(logger, outboxQueue, doneChan, mockIoService, listener, fileLock, idFile, khFile)
				conn, session = startSession(t, port, config)

				// take the open message for granted since we already tested
				<-outboxQueue

				By("sending a valid exec command to the agent")
				err := session.RequestSubsystem(sftp)
				Expect(err).To(BeNil())

				execMessage := <-outboxQueue
				Expect(execMessage.Action).To(Equal(string(bzssh.SshExec)))
				var execPayload bzssh.SshExecMessage
				json.Unmarshal(execMessage.ActionPayload, &execPayload)
				Expect(execPayload.Command).To(Equal(sftp))
				Expect(execPayload.Sftp).To(BeTrue())

				By("writing the agent's response to the ssh channel's stderr")
				messageContent := base64.StdEncoding.EncodeToString([]byte(agentReply))
				t.ReceiveStream(smsg.StreamMessage{
					Type:    smsg.StdErr,
					Content: messageContent,
					More:    false,
				})

				_, _, stderrChan := setupIo(session)
				output := <-stderrChan
				Expect(string(output)).To(Equal(agentReply))

				By("closing when the remote command finishes")
				closeMessage := <-outboxQueue
				Expect(closeMessage.Action).To(Equal(string(bzssh.SshClose)))
				var closePayload bzssh.SshCloseMessage
				err = json.Unmarshal(closeMessage.ActionPayload, &closePayload)
				Expect(err).To(BeNil())

				mockFileService.AssertExpectations(GinkgoT())
				mockIoService.AssertExpectations(GinkgoT())
			})
		})
	})
})
