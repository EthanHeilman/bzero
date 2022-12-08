package exec

import (
	"io"
)

// Stdin
type StdReader struct {
	StreamType   string
	RequestId    string
	stdinChannel chan []byte
	doneChannel  chan bool
}

func NewStdReader(streamType string, requestId string, stdinChannel chan []byte) *StdReader {
	stdin := &StdReader{
		StreamType:   streamType,
		RequestId:    requestId,
		stdinChannel: stdinChannel,
		doneChannel:  make(chan bool),
	}

	return stdin
}

func (r *StdReader) Close() {
	r.doneChannel <- true
}

func (r *StdReader) Read(p []byte) (int, error) {
	select {
	case stdin, more := <-r.stdinChannel:
		if !more {
			return 0, io.EOF
		} else {
			n := copy(p, stdin)
			return n, nil
		}
	case <-r.doneChannel:
		return 0, io.EOF
	}
}
