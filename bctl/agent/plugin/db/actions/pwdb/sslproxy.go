package pwdb

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"

	"bastionzero.com/bzerolib/logger"
	"github.com/jackc/pgproto3/v2"
)

// This is based on the code from the project pgssl (https://github.com/glebarez/pgssl)
//  released under the MIT license.
//  I would have just used pgssl as a dependency but pgssl requires the use of
//  client certs. We can not use client certs in our setting so I needed to
//  modify the code. I thank glebarez for their excellent code.

func StartSslProxy(ln net.Listener, dbHost string, dialer Dialer, logger *logger.Logger) {
	conn, err := ln.Accept()
	if err != nil {
		logger.Errorf("SSL Proxy failed %v", err)
	} else {
		// handle a exactly one connection in goroutine
		err := HandleConn(conn, dbHost, &dialer)
		if err != nil {
			logger.Errorf("Error in SSL Proxy handle connection  %v", err)
		}
	}
}

func HandleConn(clientConn net.Conn, dbHost string, dialer *Dialer) error {
	defer clientConn.Close()

	backend := pgproto3.NewBackend(pgproto3.NewChunkReader(clientConn), clientConn)
	clientStartupMessage, err := backend.ReceiveStartupMessage()
	if err != nil {
		return fmt.Errorf("error on startup message: %w", err)
	}

	switch clientStartupMessage.(type) {
	case *pgproto3.StartupMessage:
		// ok
	case *pgproto3.SSLRequest:
		_, err := clientConn.Write([]byte{'N'})
		if err != nil {
			return fmt.Errorf("error on responding to SSL Request: %v", err)
		}
	default:
		return fmt.Errorf("unknown startup message: %#v", clientStartupMessage)
	}

	pgConn, err := (*dialer).Dial("tcp", dbHost)
	if err != nil {
		return err
	}
	defer func() {
		pgConn.Close()
	}()

	// we pose as a frontend for Postgres connection
	frontend := pgproto3.NewFrontend(pgproto3.NewChunkReader(pgConn), pgConn)

	// send SSL request to postgres
	err = frontend.Send(&pgproto3.SSLRequest{})
	if err != nil {
		return err
	}

	// The server then responds with a single byte containing S or N, indicating that it is willing or unwilling to perform SSL, respectively.
	// If additional bytes are available to read at this point, it likely means that a man-in-the-middle is attempting to perform a buffer-stuffing attack (CVE-2021-23222).
	buf := make([]byte, 2)
	n, err := pgConn.Read(buf)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("server returned more than 1 byte to SSLrequest, this is not expected")
	}

	if buf[0] == 'S' {
		// We take TLS configuration settings from how TLS conf is built here: https://github.com/lib/pq/blob/381d253611d666974d43dfa634d29fe16ea9e293/ssl.go#L18
		tlsConf := tls.Config{
			InsecureSkipVerify: true,
			Renegotiation:      tls.RenegotiateFreelyAsClient,
		}

		// upgrade connection to TLS
		pgTLSconn := tls.Client(pgConn, &tlsConf)

		// upgrade frontend
		frontend = pgproto3.NewFrontend(pgproto3.NewChunkReader(pgTLSconn), pgTLSconn)
		defer frontend.Send(&pgproto3.Terminate{})

		err = pgTLSconn.Handshake()
		if err != nil {
			return fmt.Errorf("handshake error: %v", err)
		}

		// send original startup
		err = frontend.Send(clientStartupMessage)
		if err != nil {
			return err
		}

		// pipe connections
		pgConnErr, clientConnErr := Pipe(clientConn, pgTLSconn)
		if pgConnErr != nil {
			return fmt.Errorf("postgres connection error: %s", pgConnErr)
		}
		if clientConnErr != nil {
			return fmt.Errorf("client connection error: %s", clientConnErr)
		}
	} else if buf[0] == 'N' {
		// send original startup
		err = frontend.Send(clientStartupMessage)
		if err != nil {
			return err
		}

		// pipe connections
		pgConnErr, clientConnErr := Pipe(clientConn, pgConn)
		if pgConnErr != nil {
			return fmt.Errorf("postgres connection error: %s", pgConnErr)
		}
		if clientConnErr != nil {
			return fmt.Errorf("client connection error: %s", clientConnErr)
		}
	} else {
		return fmt.Errorf("unexpected response to SSLrequest: %v", buf[0])
	}
	return nil
}

type readResult struct {
	data []byte
	err  error
}

// chanFromConn and Pipe are taken verbatim from pgssl

// chanFromConn creates a channel from a Conn object, and sends everything it
// Read()s from the socket to the channel.
// channel delivers {[]byte, nil} after successfull read.
// channel delivers {nil,err} in case of error.
// channel is closed when EOF is received.
func chanFromConn(conn net.Conn) chan readResult {
	c := make(chan readResult)

	go func() {
		// make buffer to receive data
		buf := make([]byte, 1024)

		for {
			n, err := conn.Read(buf)
			if n > 0 {
				res := make([]byte, n)
				// Copy the buffer so it doesn't get changed while read by the recipient.
				copy(res, buf[:n])
				c <- readResult{res, nil}
			}
			if err == io.EOF {
				close(c)
				return
			}
			if err != nil {
				c <- readResult{nil, err}
				break
			}
		}
	}()

	return c
}

// Pipe creates a full-duplex pipe between the two sockets and transfers data from one to the other.
func Pipe(conn1 net.Conn, conn2 net.Conn) (e1, e2 error) {
	chan1 := chanFromConn(conn1)
	chan2 := chanFromConn(conn2)

	for {
		select {
		case b1, ok := <-chan1:
			if !ok {
				return // connection was closed
			}
			if b1.err != nil {
				e1 = b1.err
				return
			} else {
				conn2.Write(b1.data)
			}
		case b2, ok := <-chan2:
			if !ok {
				return // connection was closed
			}
			if b2.err != nil {
				e2 = b2.err
				return
			} else {
				conn1.Write(b2.data)
			}
		}
	}
}
