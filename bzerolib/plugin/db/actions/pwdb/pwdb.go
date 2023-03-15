package pwdb

import (
	smsg "bastionzero.com/bzerolib/stream/message"
)

type PwdbSubAction string

const (
	Connect PwdbSubAction = "db/pwdb/connect"
	Input   PwdbSubAction = "db/pwdb/input"
	Close   PwdbSubAction = "db/pwdb/close"
)

type ConnectPayload struct {
	TargetUser string `json:"targetUser"`
	TargetId   string `json:"targetId"`

	// (optional) informs Agent what SchemaVersion to use
	StreamMessageVersion smsg.SchemaVersion `json:"streamMessageVersion"`
}

type InputPayload struct {
	Data string `json:"data"`
}

type ClosePayload struct {
	Reason string `json:"reason"`
}
