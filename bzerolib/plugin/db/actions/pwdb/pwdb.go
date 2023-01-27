package pwdb

import (
	smsg "bastionzero.com/bctl/v1/bzerolib/stream/message"
)

type PwdbSubAction string

const (
	Connect PwdbSubAction = "db/pwdb/connect"
	Input   PwdbSubAction = "db/pwdb/input"
	Close   PwdbSubAction = "db/pwdb/close"
)

type PwdbConnectPayload struct {
	TargetUser string `json:"targetUser"`
	TargetId   string `json:"targetId"`

	// (optional) informs Agent what SchemaVersion to use
	StreamMessageVersion smsg.SchemaVersion `json:"streamMessageVersion"`
}

type PwdbInputPayload struct {
	SequenceNumber int    `json:"sequenceNumber"`
	Data           []byte `json:"data"`
}
