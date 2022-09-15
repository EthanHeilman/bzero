package signalr

import "encoding/json"

type SignalRMessageType int

const (
	// https://docs.microsoft.com/en-us/javascript/api/@microsoft/signalr/messagetype?view=signalr-js-latest
	Invalid SignalRMessageType = iota
	Invocation
	StreamItem
	Completion
	StreamInvocation
	CancelInvocation
	Ping
	Close
)

func (s SignalRMessageType) String() string {
	switch s {
	case Invocation:
		return "Invocation"
	case StreamItem:
		return "StreamItem"
	case Completion:
		return "Completion"
	case StreamInvocation:
		return "StreamInvocation"
	case CancelInvocation:
		return "CancelInvocation"
	case Ping:
		return "Ping"
	case Close:
		return "Close"
	default:
		return "Invalid"
	}
}

type MessageTypeOnly struct {
	Type int `json:"type"`
}

// Ref: https://docs.microsoft.com/en-us/javascript/api/@microsoft/signalr/closemessage?view=signalr-js-latest
type CloseMessage struct {
	Type           int    `json:"type"`
	Error          string `json:"error"`
	AllowReconnect bool   `json:"allowReconnect"`
}

// The pointers are so the fields can be nil because they're not always there
// Ref: https://github.com/aspnet/SignalR/blob/master/specs/HubProtocol.md#completion-message-encoding
type CompletionMessage struct {
	Type         int            `json:"type"`
	InvocationId *string        `json:"invocationId"`
	Result       *ResultMessage `json:"result"`
	Error        *string        `json:"error"`
}

type ResultMessage struct {
	ErrorMessage *string `json:"errorMessage"`
	Error        bool    `json:"error"`
}

// Ref: https://github.com/aspnet/SignalR/blob/master/specs/HubProtocol.md#invocation-message-encoding
type SignalRMessage struct {
	Type         int               `json:"type"`
	Target       string            `json:"target"` // hub name
	Arguments    []json.RawMessage `json:"arguments"`
	InvocationId string            `json:"invocationId"`
}
