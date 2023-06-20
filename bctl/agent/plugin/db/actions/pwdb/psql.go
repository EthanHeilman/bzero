package pwdb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"net"

	"bastionzero.com/bzerolib/logger"
	"github.com/lib/pq/scram"
	"github.com/rueian/pgbroker/backend"
	"github.com/rueian/pgbroker/message"
	"github.com/rueian/pgbroker/proxy"
	"google.golang.org/grpc/test/bufconn"

	_ "github.com/lib/pq"
)

// We support authentication delegation for both SCRAM and plaintext
//  password PSQL authentication protocols. AWS has configured RDS so
//  that AWS IAM Tokens are only sent as plaintext passwords. Thus,
//  when authenticating with IAM Role User the PSQL server will
//  request the authentication be sent as a plaintext password. We
//  handle this using the first case labeled
//  AuthenticationCleartextPassword. If we wish to authenticate
//  using PSQL account that isn't configured to an AWS Role User,
//  RDS requires that we use SCRAM Authentication.
//
// The PSQL SCRAM interaction is based this code from libpq:
//   https://github.com/lib/pq/blob/381d253611d666974d43dfa634d29fe16ea9e293/conn.go#L1290
//  The extract message format for these pSQL messages appears to
//  only be avaliable in the pSQL source code avaliable here:
//   https://github.com/postgres/postgres/blob/08235203ddefde1d0bfb6a1e8bb6ff546a2c7e8c/src/interfaces/libpq/fe-auth-scram.c#L725
//  Good golang source code for understanding the SCRAM flow is here:
//   https://github.com/rueian/pgbroker/issues/1#issuecomment-1107462885
//
// SCRAM Protocol works as follows
// 1. Server sends Request Auth:
//    client <-[AuthenticationSASL]-- server
// 2. Client responds with "lets do SCRAM Auth"
//    client --[SASLInitialResponse]-> server
// 3. Server sends SCRAM challenge
//    client <--[AuthenticationSASLContinue]- server
// 4. Client responds to SCRAM challenge
//    client --[SASLResponse]-> server
// 5. Server is convinced, server convinces client with SCRAM signature
//    client --[AuthenticationSASLFinal]-> server
// 6. Server sends Auth OK to let client know client can send queries
//    client <-AuthenticationOk-- server
// 7. Client is now convinced server is authentication is complete

// Used to pass the Scram session via a context
type scramCtxKey struct{}

func psqlProxy(uname string, pw string, dbEndpoint string, ln net.Listener, dialer Dialer, logger *logger.Logger) (*proxy.Server, *bufconn.Listener) {

	var server proxy.Server

	// Server callbacks will be ignored unless we set a client call back and
	//  pass it to the proxy.Server. So we set clientStreamCallbackFactories
	//  but don't add any call backs to it.
	clientStreamCallbackFactories := proxy.NewStreamCallbackFactories()
	serverStreamCallbackFactories := proxy.NewStreamCallbackFactories()
	serverStreamCallbackFactories.SetFactory('R', func(ctx *proxy.Ctx) proxy.StreamCallback {
		buf := bytes.Buffer{}
		return func(s proxy.Slice) proxy.Slice {
			emptySlice := proxy.Slice{
				Head: true,
				Last: true,
				Data: []byte{},
			}
			if s.Head {
				buf.Reset()
			}
			buf.Write(s.Data)
			if !s.Last {
				return emptySlice
			}
			// ReadAuthentication function assumes the type byte ('R' in this
			//  case) and the message length int have already been processed
			//  and removed. ReadAuthentication will fail if we not remove them.
			msgBytes := buf.Bytes()[5:]

			switch msg := message.ReadAuthentication(msgBytes).(type) {
			case *message.AuthenticationCleartextPassword:
				// Cleartext password is used by AWS for AWS Tokens. To prevent
				//  interception on AWS Tokens they require SSL on all AWS
				//  Token password auth to RDS.
				pwResp := message.PasswordMessage{
					Password: pw,
				}

				pwBytes, err := io.ReadAll(pwResp.Reader())
				if err != nil {
					// If authentication hits an error, do not continue,
					// and teardown the proxy
					logger.Errorf("AuthenticationCleartextPassword error: %v", err)
					server.Shutdown()
					return emptySlice
				}
				ctx.ServerConn.Write(pwBytes)
				return emptySlice
			case *message.AuthenticationSASL: // Client <--- server
				// Initize the SCRAM client each time we receive a SCRAM message.
				sc := scram.NewClient(sha256.New, uname, pw)
				ctx.Context = context.WithValue(ctx.Context, scramCtxKey{}, sc)

				// Client ---> server
				sc.Step(nil)
				if sc.Err() != nil {
					logger.Errorf("AuthenticationSASL SCRAM-SHA-256 error: %s", sc.Err().Error())
					server.Shutdown()
					return emptySlice
				}
				scOut := sc.Out()

				scram1 := message.SASLInitialResponse{
					Mechanism: "SCRAM-SHA-256",
					Response:  message.NewValue(scOut),
				}

				scram1Bytes, err := io.ReadAll(scram1.Reader())
				if err != nil {
					logger.Errorf("SASLInitialResponse SCRAM-SHA-256 error: %s", sc.Err().Error())
					server.Shutdown()
					return emptySlice
				}
				ctx.ServerConn.Write(scram1Bytes)
				return emptySlice

			case *message.AuthenticationSASLContinue: // Client <--- server
				sc := ctx.Context.Value(scramCtxKey{}).(*scram.Client)

				sc.Step(msg.Data)
				if sc.Err() != nil {
					logger.Errorf("AuthenticationSASLContinue SCRAM-SHA-256 error: %s", sc.Err().Error())
					server.Shutdown()
					return emptySlice
				}

				// Client ---> server
				scOut := sc.Out()
				scram3 := message.SASLResponse{Data: scOut}
				scram3Bytes, err := io.ReadAll(scram3.Reader())
				if err != nil {
					logger.Errorf("SASLResponse SCRAM-SHA-256 error: %s", sc.Err().Error())
					server.Shutdown()
					return emptySlice
				}
				ctx.ServerConn.Write(scram3Bytes)
				return emptySlice

			// Client <--- server
			case *message.AuthenticationSASLFinal:
				logger.Infof("AuthenticationSASLFinal: buf=%q,\n", buf.Bytes())
				sc := ctx.Context.Value(scramCtxKey{}).(*scram.Client)
				sc.Step(msg.Data)
				if sc.Err() != nil {
					logger.Errorf("AuthenticationSASLFinal SCRAM-SHA-256 error: %s", sc.Err().Error())
					server.Shutdown()
					return emptySlice
				}
				return emptySlice
			}
			return proxy.Slice{
				Head: true,
				Last: true,
				Data: buf.Bytes(),
			}
		}
	})

	// Use of the bufConn here does not provide security value. Rather we do it
	//  to avoid the potentional of TCP port collision or exhaustion.
	sslProxyLn := bufconn.Listen(4096)

	go StartSslProxy(sslProxyLn, dbEndpoint, dialer, logger)

	server = proxy.Server{
		PGResolver: &BufFConnPGResolver{
			Address:   sslProxyLn.Addr().String(),
			bufConnLn: sslProxyLn,
		},
		ConnInfoStore:                 backend.NewInMemoryConnInfoStore(),
		ClientStreamCallbackFactories: clientStreamCallbackFactories,
		ServerStreamCallbackFactories: serverStreamCallbackFactories,
		OnHandleConnError: func(err error, ctx *proxy.Ctx, conn net.Conn) {
			logger.Errorf("Pgbroker Proxy failed to connect to %s with error %v\n", sslProxyLn.Addr().String(), err)
		},
		Splice: false, // If splice is true, proxy does not intercept messages and fire callbacks
	}
	go server.Serve(ln)

	// We pass the server and the sslProxyLn back to esnure the caller has handles to
	//  shut down both proxies.
	return &server, sslProxyLn
}

type BufFConnPGResolver struct {
	Address   string
	bufConnLn *bufconn.Listener
}

func (r *BufFConnPGResolver) GetPGConn(ctx context.Context, clientAddr net.Addr, parameters map[string]string) (net.Conn, error) {
	return r.bufConnLn.Dial()
}
