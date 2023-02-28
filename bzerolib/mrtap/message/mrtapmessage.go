package message

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"bastionzero.com/bzerolib/keypair"
	"bastionzero.com/bzerolib/mrtap/util"
)

// Type restrictions for MrTAP messages
type PayloadType string

const (
	Syn     PayloadType = "Syn"
	SynAck  PayloadType = "SynAck"
	Data    PayloadType = "Data"
	DataAck PayloadType = "DataAck"
)

const (
	SchemaVersion = "2.2"
)

type MrtapMessage struct {
	Type      PayloadType `json:"type"`
	Payload   interface{} `json:"payload"`
	Signature string      `json:"signature"`
}

func (m *MrtapMessage) Hash() string {
	// grab the hash of the MrTAP message
	if hashBytes, ok := util.HashPayload(m.Payload); !ok {
		return ""
	} else {
		return base64.StdEncoding.EncodeToString(hashBytes)
	}
}

func (m *MrtapMessage) BuildUnsignedSynAck(payload []byte, pubKey string, nonce string, schemaVersion string) (MrtapMessage, error) {
	if msg, ok := m.Payload.(SynPayload); ok {
		if synAckPayload, err := msg.BuildResponsePayload(payload, pubKey, nonce, schemaVersion); err != nil {
			return MrtapMessage{}, err
		} else {
			return MrtapMessage{
				Type:    SynAck,
				Payload: synAckPayload,
			}, nil
		}
	} else {
		return MrtapMessage{}, fmt.Errorf("can't build syn/ack off a message that isn't a syn")
	}
}

func (m *MrtapMessage) BuildUnsignedDataAck(payload []byte, pubKey string, schemaVersion string) (MrtapMessage, error) {
	if msg, ok := m.Payload.(DataPayload); ok {
		if dataAckPayload, err := msg.BuildResponsePayload(payload, pubKey, schemaVersion); err != nil {
			return MrtapMessage{}, err
		} else {
			return MrtapMessage{
				Type:    DataAck,
				Payload: dataAckPayload,
			}, nil
		}
	} else {
		return MrtapMessage{}, fmt.Errorf("can't build data/ack off a message that isn't a data")
	}
}

func (m *MrtapMessage) BuildUnsignedData(action string, actionPayload []byte, bzcertHash string, schemaVersion string) (MrtapMessage, error) {
	switch msg := m.Payload.(type) {
	case SynAckPayload:
		if dataPayload, err := msg.BuildResponsePayload(action, actionPayload, bzcertHash, schemaVersion); err != nil {
			return MrtapMessage{}, err
		} else {
			return MrtapMessage{
				Type:    Data,
				Payload: dataPayload,
			}, nil
		}
	case DataAckPayload:
		if dataPayload, err := msg.BuildResponsePayload(action, actionPayload, bzcertHash, schemaVersion); err != nil {
			return MrtapMessage{}, err
		} else {
			return MrtapMessage{
				Type:    Data,
				Payload: dataPayload,
			}, nil
		}
	default:
		return MrtapMessage{}, fmt.Errorf("can't build data responses for message type: %T", m.Payload)
	}
}

func (m *MrtapMessage) GetHpointer() (string, error) {
	switch msg := m.Payload.(type) {
	case SynPayload:
		return "", fmt.Errorf("syn payloads don't have hpointers")
	case SynAckPayload:
		return msg.HPointer, nil
	case DataPayload:
		return msg.HPointer, nil
	case DataAckPayload:
		return msg.HPointer, nil
	default:
		return "", fmt.Errorf("could not get hpointer for invalid MrTAP message type: %T", m.Payload)
	}
}

func (m *MrtapMessage) GetAction() string {
	switch msg := m.Payload.(type) {
	case SynPayload:
		return msg.Action
	case SynAckPayload:
		return msg.Action
	case DataPayload:
		return msg.Action
	case DataAckPayload:
		return msg.Action
	default:
		return ""
	}
}

func (m *MrtapMessage) GetActionPayload() []byte {
	switch msg := m.Payload.(type) {
	case SynPayload:
		return msg.ActionPayload
	case SynAckPayload:
		return msg.ActionResponsePayload
	case DataPayload:
		return msg.ActionPayload
	case DataAckPayload:
		return msg.ActionResponsePayload
	default:
		return []byte{}
	}
}

func (m *MrtapMessage) VerifySignature(publicKey *keypair.PublicKey) error {
	hashBits, ok := util.HashPayload(m.Payload)
	if !ok {
		return fmt.Errorf("failed to hash the keysplitting payload")
	}

	if ok := publicKey.Verify(hashBits, m.Signature); !ok {
		return fmt.Errorf("invalid signature foer payload: %+v", m.Payload)
	}

	return nil
}

func (m *MrtapMessage) Sign(privateKey *keypair.PrivateKey) bool {
	hashBits, ok := util.HashPayload(m.Payload)
	m.Signature = privateKey.Sign(hashBits)
	return ok
}

func (m *MrtapMessage) UnmarshalJSON(data []byte) error {
	var objmap map[string]*json.RawMessage

	if err := json.Unmarshal(data, &objmap); err != nil {
		return err
	}

	var t, s string
	if err := json.Unmarshal(*objmap["type"], &t); err != nil {
		return err
	} else {
		m.Type = PayloadType(t)
	}

	if err := json.Unmarshal(*objmap["signature"], &s); err != nil {
		return err
	} else {
		m.Signature = s
	}

	// fall back to the legacy payload if we're talking to an older relative
	// TODO: CWC-2183; remove this logic in the far future
	var payload json.RawMessage
	if _, ok := objmap["payload"]; ok {
		payload = *objmap["payload"]
	} else {
		payload = *objmap["keysplittingPayload"]
	}

	switch m.Type {
	case Syn:
		var synPayload SynPayload
		if err := json.Unmarshal(payload, &synPayload); err != nil {
			return fmt.Errorf("malformed Syn Payload")
		} else {
			m.Payload = synPayload
		}
	case SynAck:
		var synAckPayload SynAckPayload
		if err := json.Unmarshal(payload, &synAckPayload); err != nil {
			return fmt.Errorf("malformed SynAck Payload")
		} else {
			m.Payload = synAckPayload
		}
	case Data:
		var dataPayload DataPayload
		if err := json.Unmarshal(payload, &dataPayload); err != nil {
			return fmt.Errorf("malformed Data Payload")
		} else {
			m.Payload = dataPayload
		}
	case DataAck:
		var dataAckPayload DataAckPayload
		if err := json.Unmarshal(payload, &dataAckPayload); err != nil {
			return fmt.Errorf("malformed DataAck Payload")
		} else {
			m.Payload = dataAckPayload
		}
	default:
		return fmt.Errorf("type mismatch in MrTAP message and actual message payload")
	}

	return nil
}

// TODO: CWC-2183; remove this logic in the future
func (m MrtapMessage) MarshalJSON() ([]byte, error) {
	// to inherit MrtapMessage's fields without inheriting its JSON marshaller, we need the alias type
	// ref: https://stackoverflow.com/a/23046869/8414180
	type Alias MrtapMessage
	return json.Marshal(&struct {
		LegacyPayload interface{} `json:"keysplittingPayload"`
		Alias
	}{
		LegacyPayload: m.Payload,
		Alias:         Alias(m),
	})
}
