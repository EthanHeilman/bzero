package pwdb

import (
	"fmt"
	"net"
	"testing"

	"bastionzero.com/bzerolib/logger"
	"bastionzero.com/bzerolib/plugin/db/actions/pwdb"
	"github.com/jackc/pgproto3/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/test/bufconn"
)

func TestGCPConnectorsSQL(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agent PWDB SQL Connection")
}

var _ = Describe("Test PWDB Connections", func() {
	logger := logger.MockLogger(GinkgoWriter)

	Context("Starting a GCP Dial", func() {
		When("Connecting to a GCP instance via the GCP dialer", func() {
			// the host is prefixed by gcp, which flags that a gcp dialer should be used
			host := "gcp://fakedb:us-central1:fakedb"
			remotePort := 99999
			targetUser := "alice@fakeproject.iam.gserviceaccount.fake"
			targetId := "faketargetId"
			action := string(pwdb.Connect)
			p := &Pwdb{
				logger:           logger,
				doneChan:         nil,
				keyshardConfig:   nil,
				bastionClient:    nil,
				streamOutputChan: nil,
				remoteHost:       host,
				remotePort:       remotePort,
			}
			err := p.start(targetId, targetUser, action)

			// This isn't the satifying way to test this functionality, but it works. We determine that a GCP
			// connector has been called because it throws an error that idenifies it has been called.
			Expect(err.Error()).To(ContainSubstring("google: could not find default credentials."))
		})
	})

	Context("Starting a RDS PSQL Dial and auth", func() {
		When("Connecting to a RDS PSQL instance via the RDS dialer with AWS Token", func() {

			bufconnLis := bufconn.Listen(4096)

			host := "rds://database-name.fakefakefake.us-fake-1.rds.amazonaws.com"
			remotePort := 99999
			targetUser := "db_userx"
			targetId := "faketargetId"
			action := string(pwdb.Connect)
			p := &Pwdb{
				logger:           logger,
				doneChan:         make(chan struct{}),
				keyshardConfig:   nil,
				bastionClient:    nil,
				streamOutputChan: nil,
				remoteHost:       host,
				remotePort:       remotePort,
				DbDialer:         &BufConnDialer{Ln: bufconnLis},
				RDSTokenBuilder: func(dbEndpoint string, region string, dbUser string) (string, error) {
					return dbEndpoint + "?Action=connect&DBUser=" + dbUser + "&X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=fake&X-Amz-Date=20230614T032432Z&X-Amz-Expires=900&X-Amz-SignedHeaders=host&X-Amz-Signature=23", nil
				},
			}
			err := p.start(targetId, targetUser, action)
			Expect(err).To(BeNil())

			go func() {
				err = FakePostgresClient(p)
				Expect(err).To(BeNil())
			}()

			dbConn, err := bufconnLis.Accept()
			Expect(err).To(BeNil())

			password, err := FakePostgresServer(dbConn)
			Expect(err).To(BeNil())
			Expect(password).To(ContainSubstring("X-Amz-Algorithm=AWS4-HMAC-SHA256"))
			Expect(password).To(ContainSubstring(targetUser))
		})
	})
})

type BufConnDialer struct {
	Ln *bufconn.Listener
}

func (b *BufConnDialer) Dial(network string, dbEndPoint string) (net.Conn, error) {
	return b.Ln.Dial()
}

func FakePostgresClient(pwdb *Pwdb) error {
	frontend := pgproto3.NewFrontend(pgproto3.NewChunkReader(pwdb.remoteConn), pwdb.remoteConn)

	err := frontend.Send(&pgproto3.StartupMessage{
		ProtocolVersion: pgproto3.ProtocolVersionNumber,
		Parameters: map[string]string{
			"client_encoding":    "UTF8",
			"datestyle":          "ISO, MDY",
			"extra_float_digits": "2",
			"user":               "postgres",
		},
	})
	if err != nil {
		return err
	}

	serverMsg, err := frontend.Receive()
	if err != nil {
		return err
	}

	if _, ok := serverMsg.(*pgproto3.AuthenticationOk); ok {
		return nil
	}

	return fmt.Errorf("Unexpected message type: %#v", serverMsg)
}

func FakePostgresServer(dbConn net.Conn) (string, error) {
	backend := pgproto3.NewBackend(pgproto3.NewChunkReader(dbConn), dbConn)

	clientStartupMessage, err := backend.ReceiveStartupMessage()
	if err != nil {
		return "", fmt.Errorf("error receiving StartupMessage: %v", err)
	}
	if _, ok := clientStartupMessage.(*pgproto3.SSLRequest); !ok {
		return "", fmt.Errorf("expected SSLRequest got: %v", clientStartupMessage)
	}
	_, err = dbConn.Write([]byte{'N'})
	if err != nil {
		return "", fmt.Errorf("error responding to SSLRequest: %v", err)
	}

	clientStartupMessage, err = backend.ReceiveStartupMessage()
	if err != nil {
		return "", fmt.Errorf("error receiving StartupMessage: %v", err)
	}
	if _, ok := clientStartupMessage.(*pgproto3.StartupMessage); !ok {
		return "", fmt.Errorf("expected StartupMessage got: %v", clientStartupMessage)
	}

	err = backend.Send(&pgproto3.AuthenticationCleartextPassword{})
	if err != nil {
		return "", fmt.Errorf("error responding to AuthenticationCleartextPassword: %v", err)
	}

	clientMsg, err := backend.Receive()
	if err != nil {
		return "", fmt.Errorf("error on startup message: %w", err)
	}

	if pwMsg, ok := clientMsg.(*pgproto3.PasswordMessage); ok {
		backend.Send(&pgproto3.AuthenticationOk{})
		return pwMsg.Password, nil
	} else {
		return "", fmt.Errorf("Unexpected message type: %#v", clientMsg)
	}
}
