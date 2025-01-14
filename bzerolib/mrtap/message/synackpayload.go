package message

import (
	"encoding/base64"

	"bastionzero.com/bzerolib/mrtap/util"
)

// Repetition in MrTAP messages is requires to maintain flat
// structure which is important for hashing
type SynAckPayload struct {
	SchemaVersion         string `json:"schemaVersion"`
	Type                  string `json:"type"`
	Action                string `json:"action"`
	ActionResponsePayload []byte `json:"actionResponsePayload"`
	Timestamp             string `json:"timestamp"`

	// Unique to SynAck
	TargetPublicKey string `json:"targetPublicKey"`
	Nonce           string `json:"nonce"`
	HPointer        string `json:"hPointer"`
}

func (s SynAckPayload) BuildResponsePayload(action string, actionPayload []byte, bzCertHash string, schemaVersion string) (DataPayload, error) {
	hashBytes, _ := util.HashPayload(s)
	hash := base64.StdEncoding.EncodeToString(hashBytes)

	return DataPayload{
		SchemaVersion: schemaVersion,
		Type:          string(Data),
		Action:        action,
		TargetId:      s.TargetPublicKey, // TODO: Make this come from storage
		HPointer:      hash,
		ActionPayload: actionPayload,
		BZCertHash:    bzCertHash,
	}, nil
}
