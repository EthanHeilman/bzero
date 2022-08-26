// Copyright 2022 BastionZero Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may not
// use this file except in compliance with the License. A copy of the
// License is located at
//
// https://www.apache.org/licenses/LICENSE-2.0
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
// either express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package datachannel

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/keysplitting"
	am "bastionzero.com/bctl/v1/bzerolib/channels/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/channels/websocket"
	"bastionzero.com/bctl/v1/bzerolib/keysplitting/bzcert"
	ksmsg "bastionzero.com/bctl/v1/bzerolib/keysplitting/message"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	bzshell "bastionzero.com/bctl/v1/bzerolib/plugin/shell"

	"github.com/stretchr/testify/assert"
	"gopkg.in/tomb.v2"
)

type MocktKeysplitting struct{ keysplitting.Keysplitting }

func (m *MocktKeysplitting) GetHpointer() string                                 { return "fake" }
func (m *MocktKeysplitting) Validate(ksMessage *ksmsg.KeysplittingMessage) error { return nil }
func (m *MocktKeysplitting) BuildAck(ksMessage *ksmsg.KeysplittingMessage,
	action string,
	actionPayload []byte) (ksmsg.KeysplittingMessage, error) {

	var responseMessage ksmsg.KeysplittingMessage
	switch ksMessage.Type {
	case ksmsg.Syn:
		responseMessage = ksmsg.KeysplittingMessage{
			Type:                ksmsg.SynAck,
			KeysplittingPayload: ksmsg.SynAckPayload{},
		}
	case ksmsg.Data:
		responseMessage = ksmsg.KeysplittingMessage{
			Type:                ksmsg.DataAck,
			KeysplittingPayload: ksmsg.DataAckPayload{},
		}
	}
	return responseMessage, nil
}

// type MocktKeysplittingValidated struct{ keysplitting.Keysplitting }
// func (m *MocktKeysplittingValidated) GetHpointer() string { return "fake" }
// func (m *MocktKeysplittingValidated) Validate(ksMessage *ksmsg.KeysplittingMessage) error {
// 	switch ksMessage.Type {
// 	case ksmsg.Syn:
// 		synPayload := ksMessage.KeysplittingPayload.(ksmsg.SynPayload)

// 		// Verify the BZCert
// 		hash, exp, err := synPayload.BZCert.Verify(m.idpProvider, m.idpOrgId)
// 		if err != nil {
// 			return fmt.Errorf("failed to verify SYN's BZCert: %w", err)
// 		}

// 		// Verify the signature
// 		if err := ksMessage.VerifySignature(synPayload.BZCert.ClientPublicKey); err != nil {
// 			return fmt.Errorf("failed to verify SYN's signature: %w", err)
// 		}

// 		// Extract semver version to determine if different protocol checks must
// 		// be done
// 		v, err := semver.NewVersion(synPayload.SchemaVersion)
// 		if err != nil {
// 			return fmt.Errorf("failed to parse schema version (%v) as semver: %w", synPayload.SchemaVersion, err)
// 		} else {
// 			m.daemonSchemaVersion = v
// 		}

// 		// Daemons with schema version <= 1.0 do not set targetId, so we cannot
// 		// apply this check universally
// 		// TODO: CWC-1553: Always check TargetId once all daemons have updated
// 		if m.shouldCheckTargetId.Check(v) {
// 			// Verify SYN message commits to this agent's cryptographic identity
// 			if synPayload.TargetId != m.publickey {
// 				return fmt.Errorf("SYN's TargetId did not match agent's public key")
// 			}
// 		}

// 		// All checks have passed. Make this BZCert that of the active user
// 		m.bzCert = BZCertMetadata{
// 			Hash:       hash,
// 			Cert:       synPayload.BZCert,
// 			Expiration: exp,
// 		}
// 	case ksmsg.Data:
// 		dataPayload := ksMessage.KeysplittingPayload.(ksmsg.DataPayload)

// 		// Check BZCert matches one we have stored
// 		if m.bzCert.Hash != dataPayload.BZCertHash {
// 			return fmt.Errorf("DATA's BZCert does not match the active user's")
// 		}

// 		// Verify the signature
// 		if err := ksMessage.VerifySignature(k.bzCert.Cert.ClientPublicKey); err != nil {
// 			return err
// 		}

// 		// Check that BZCert isn't expired
// 		if time.Now().After(k.bzCert.Expiration) {
// 			return fmt.Errorf("DATA's referenced BZCert has expired")
// 		}

// 		// Verify received hash pointer matches expected
// 		if dataPayload.HPointer != k.expectedHPointer {
// 			return fmt.Errorf("DATA's hash pointer %s did not match expected hash pointer %s", dataPayload.HPointer, k.expectedHPointer)
// 		}

// 		m.lastDataMessage = ksMessage
// 	default:
// 		return fmt.Errorf("error validating unhandled Keysplitting type")
// 	}

// 	return nil
// }
// func (m *MocktKeysplittingValidated) BuildAck(ksMessage *ksmsg.KeysplittingMessage,
// 	action string,
// 	actionPayload []byte) (ksmsg.KeysplittingMessage, error) {

// 	var responseMessage ksmsg.KeysplittingMessage
// 	switch ksMessage.Type {
// 	case ksmsg.Syn:
// 		responseMessage = ksmsg.KeysplittingMessage{
// 			Type:                ksmsg.SynAck,
// 			KeysplittingPayload: ksmsg.SynAckPayload{},
// 		}
// 	case ksmsg.Data:
// 		responseMessage = ksmsg.KeysplittingMessage{
// 			Type:                ksmsg.DataAck,
// 			KeysplittingPayload: ksmsg.DataAckPayload{},
// 		}
// 	}
// 	return responseMessage, nil
// }

type TestWebsocket struct {
	websocket.Websocket
	MsgsSent []am.AgentMessage
}

func (w *TestWebsocket) Connect() error { return nil }
func (w *TestWebsocket) Send(agentMessage am.AgentMessage) {
	fmt.Println("Send agentMessage: ", string(agentMessage.MessagePayload))
	payload := string(agentMessage.MessagePayload)

	if strings.Contains(payload, "stdout") {
		fmt.Println("Send agentMessage: dropping message", agentMessage.MessageType)
	} else {
		fmt.Println("Send agentMessage: adding message", agentMessage.MessageType)
		w.MsgsSent = append(w.MsgsSent, agentMessage)
	}
	fmt.Println("Send len(ws.MsgsSent): ", len(w.MsgsSent))
}
func (w *TestWebsocket) Unsubscribe(id string)                           {}
func (w *TestWebsocket) Subscribe(id string, channel websocket.IChannel) {}
func (w *TestWebsocket) Close(err error)                                 {}

func (w *TestWebsocket) LenMS() int {
	return len(w.MsgsSent)
}

func CreateSynMsg(t *testing.T) []byte {
	fakebzcert := bzcert.BZCert{}
	// runAsUser := "test"
	runAsUser := "e0"
	// runAsUser := testutils.GetRunAsUser(t)
	// synActionPayload, _ := json.Marshal(bzshell.ShellOpenMessage{TargetUser: runAsUser})
	synActionPayload, _ := json.Marshal(bzshell.ShellActionParams{TargetUser: runAsUser})

	synPayload := ksmsg.SynPayload{
		Timestamp:     fmt.Sprint(time.Now().Unix()),
		SchemaVersion: ksmsg.SchemaVersion,
		Type:          string(ksmsg.Syn),
		Action:        "shell/default/open",
		// Action:        string(bzshell.ShellOpen),
		// Action:        string(bzshell.DefaultShell),
		ActionPayload: synActionPayload,
		TargetId:      "currently unused",
		Nonce:         "fake nonce",
		BZCert:        fakebzcert,
	}

	// var synPayload ksmsg.KeysplittingMessage
	synBytes, _ := json.Marshal(ksmsg.KeysplittingMessage{
		Type:                ksmsg.Syn,
		KeysplittingPayload: synPayload,
		Signature:           "fake signature",
	})

	return synBytes
}

func CreateOpenShellDataMsg() []byte {
	// dataActionPayload, _ := json.Marshal(bzshell.ShellOpenMessage{TargetUser: "test-user"})
	dataActionPayload, _ := json.Marshal(bzshell.ShellOpenMessage{})

	dataPayload := ksmsg.DataPayload{
		Timestamp:     fmt.Sprint(time.Now().Unix()),
		SchemaVersion: ksmsg.SchemaVersion,
		Type:          string(ksmsg.Data),
		Action:        "shell/open",
		ActionPayload: dataActionPayload,
		TargetId:      "currently unused",
		BZCertHash:    "fake",
	}

	dataBytes, _ := json.Marshal(ksmsg.KeysplittingMessage{
		Type:                ksmsg.Data,
		KeysplittingPayload: dataPayload,
		Signature:           "fake signature",
	})

	return dataBytes
}

func CreateInputShellDataMsg() []byte {
	dataActionPayload, err := json.Marshal(bzshell.ShellInputMessage{
		// Data: base64.StdEncoding.EncodeToString([]byte("e")),
		Data: []byte("e"),
	})

	if err != nil {
		return nil
	}

	dataPayload := ksmsg.DataPayload{
		Timestamp:     fmt.Sprint(time.Now().Unix()),
		SchemaVersion: ksmsg.SchemaVersion,
		Type:          string(ksmsg.Data),
		Action:        "shell/input",
		ActionPayload: dataActionPayload,
		TargetId:      "fake",
		BZCertHash:    "fake",
	}

	dataBytes, _ := json.Marshal(ksmsg.KeysplittingMessage{
		Type:                ksmsg.Data,
		KeysplittingPayload: dataPayload,
		Signature:           "fake signature",
	})

	return dataBytes
}

func CreateAgentMessage(dataBytes []byte) am.AgentMessage {
	agentMsg := am.AgentMessage{
		ChannelId:      "fake",
		MessageType:    "keysplitting",
		SchemaVersion:  ksmsg.SchemaVersion,
		MessagePayload: dataBytes,
	}
	return agentMsg
}

func TestShelllDatachannel(t *testing.T) {

	assert.Contains(t, string("abce"), "ab")

	var tmb tomb.Tomb
	subLogger := logger.MockLogger()
	ws := &TestWebsocket{}
	dcID := "testID-1"

	synBytes := CreateSynMsg(t)
	datachannel, err := New(&tmb, subLogger, ws, &MocktKeysplitting{}, dcID, synBytes)

	assert.Nil(t, err)
	assert.NotNil(t, datachannel)
	time.Sleep(1 * time.Second)

	fmt.Println(strconv.Itoa(ws.LenMS()))
	assert.LessOrEqual(t, 1, len(ws.MsgsSent))

	assert.EqualValues(t, "keysplitting", ws.MsgsSent[0].MessageType)

	var synackMsg ksmsg.KeysplittingMessage
	err = json.Unmarshal(ws.MsgsSent[0].MessagePayload, &synackMsg)
	assert.Nil(t, err)
	assert.EqualValues(t, ksmsg.SynAck, synackMsg.Type)

	dataBytes := CreateOpenShellDataMsg()
	agentMsg := CreateAgentMessage(dataBytes)
	fmt.Println(strconv.Itoa(ws.LenMS()))

	datachannel.processInput(agentMsg)
	fmt.Println(strconv.Itoa(ws.LenMS()))
	time.Sleep(1 * time.Second)
	fmt.Println(strconv.Itoa(ws.LenMS()))

	fmt.Println("MessagePayload: ", string(ws.MsgsSent[1].MessagePayload))
	assert.EqualValues(t, "keysplitting", ws.MsgsSent[1].MessageType)
	assert.LessOrEqual(t, 2, len(ws.MsgsSent))

	var dataAckMsg1 ksmsg.KeysplittingMessage
	err = json.Unmarshal(ws.MsgsSent[1].MessagePayload, &dataAckMsg1)
	assert.Nil(t, err)
	assert.EqualValues(t, ksmsg.DataAck, dataAckMsg1.Type)

	dataBytes = CreateInputShellDataMsg()
	agentMsg = CreateAgentMessage(dataBytes)
	datachannel.processInput(agentMsg)
	time.Sleep(1 * time.Second)

	var dataAckMsg2 ksmsg.KeysplittingMessage
	assert.LessOrEqual(t, 3, len(ws.MsgsSent))
	fmt.Println(string(ws.MsgsSent[2].MessagePayload))
	err = json.Unmarshal(ws.MsgsSent[2].MessagePayload, &dataAckMsg2)
	assert.Nil(t, err)
	assert.EqualValues(t, ksmsg.DataAck, dataAckMsg2.Type)
}

func TestShelllSimpleDeserialization(t *testing.T) {
	actionPayloadSafe, _ := json.Marshal(
		// bzshell.ShellInputMessage{Data: base64.StdEncoding.EncodeToString([]byte("e"))})
		bzshell.ShellInputMessage{Data: []byte("e")})

	var shellInput bzshell.ShellInputMessage

	err := json.Unmarshal(actionPayloadSafe, &shellInput)
	assert.Nil(t, err)
}

func CreateServiceAccSynMsg(t *testing.T) ksmsg.KeysplittingMessage {
	pk := "N25HZ+4ookpSu8cNpViPoalYzE8M3dbnl7pF1D4CCwY="
	// pkB, _ := base64.StdEncoding.DecodeString(pk)
	// pkHex := hex.EncodeToString(pkB)
	// pkStr := base64.StdEncoding.EncodeToString([]byte(pkHex))

	sk := "JTQO7YQcMqC4qAcwq4eh+y8Gczq8J8nPaTCkbwQrQRI="
	skB, _ := base64.StdEncoding.DecodeString(sk)
	skHex := hex.EncodeToString(skB)
	skStr := base64.StdEncoding.EncodeToString([]byte(skHex))

	fakebzcert := bzcert.BZCert{
		InitialIdToken:  "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiIxMDgyMDg4Mjk2NTgxNTA5NjkwOTUiLCJhenAiOiIxMDgyMDg4Mjk2NTgxNTA5NjkwOTUiLCJlbWFpbCI6ImV0aGFuYm90QHdlYmFwcHdlYnNoZWxsLWRldi5pYW0uZ3NlcnZpY2VhY2NvdW50LmNvbSIsImVtYWlsX3ZlcmlmaWVkIjp0cnVlLCJleHAiOjIwMDI5Mjc4ODQ2NTI4MDAsImhkIjoiZ2NwLXByb2otbmFtZSIsImlzcyI6ImV0aGFuYm90QHdlYmFwcHdlYnNoZWxsLWRldi5pYW0uZ3NlcnZpY2VhY2NvdW50LmNvbSIsIm5vbmNlIjoiU0JHQVBtL2RPMWpCd2ova2hybFlQa3JnRnpPVmtKdHJpWGJHOXE3WEY5cz0iLCJzdWIiOiIxMDgyMDg4Mjk2NTgxNTA5NjkwOTUiLCJienNhY2MiOnRydWUsImlhdCI6MTY1NTg1OTY5M30.nWdBI2wQLrNTPXwOtmz4VA-wdp9Vxmgw9FAE1Q6N4n7Xnb0OPlS2yc0ncUOLoWzvXy-IDwiFToLZPeoqlZ23TnTs1Kddmsrjuv5YQqHjpOyCadgc4cIU6CNs9HQ5s7YcPFNmI2sF2KCoYaBGgDegeKdj6kN-kvpkUcGJCghDXOk8Mp5iCwkIVKVb5FbveqtO0bPc45kvsy-bNvnFkP3CrSUezAo67QBybs8hADGCXcOlW65t5eW69YMgzv4pEonO2GdIGn6XGj3dDXw-YnnDQCgwcCbQFm1kPMSCJU3JGl4V6oFm5KOq9WP4PZKEmnGM9x_KlKpWoO6AO8U3P0bDiQ",
		CurrentIdToken:  "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiIxMDgyMDg4Mjk2NTgxNTA5NjkwOTUiLCJhenAiOiIxMDgyMDg4Mjk2NTgxNTA5NjkwOTUiLCJlbWFpbCI6ImV0aGFuYm90QHdlYmFwcHdlYnNoZWxsLWRldi5pYW0uZ3NlcnZpY2VhY2NvdW50LmNvbSIsImVtYWlsX3ZlcmlmaWVkIjp0cnVlLCJleHAiOjIwMDI5Mjc4ODQ2NTI4MDAsImhkIjoiZ2NwLXByb2otbmFtZSIsImlzcyI6ImV0aGFuYm90QHdlYmFwcHdlYnNoZWxsLWRldi5pYW0uZ3NlcnZpY2VhY2NvdW50LmNvbSIsIm5vbmNlIjoiU0JHQVBtL2RPMWpCd2ova2hybFlQa3JnRnpPVmtKdHJpWGJHOXE3WEY5cz0iLCJzdWIiOiIxMDgyMDg4Mjk2NTgxNTA5NjkwOTUiLCJienNhY2MiOnRydWUsImlhdCI6MTY1NTg1OTY5M30.nWdBI2wQLrNTPXwOtmz4VA-wdp9Vxmgw9FAE1Q6N4n7Xnb0OPlS2yc0ncUOLoWzvXy-IDwiFToLZPeoqlZ23TnTs1Kddmsrjuv5YQqHjpOyCadgc4cIU6CNs9HQ5s7YcPFNmI2sF2KCoYaBGgDegeKdj6kN-kvpkUcGJCghDXOk8Mp5iCwkIVKVb5FbveqtO0bPc45kvsy-bNvnFkP3CrSUezAo67QBybs8hADGCXcOlW65t5eW69YMgzv4pEonO2GdIGn6XGj3dDXw-YnnDQCgwcCbQFm1kPMSCJU3JGl4V6oFm5KOq9WP4PZKEmnGM9x_KlKpWoO6AO8U3P0bDiQ",
		ClientPublicKey: pk,
		Rand:            "P/uzSMGPWY/01ZvaygytwpCeLGBBYfg+057pWaV9j+g=",
		SignatureOnRand: "/Rx/1kJN32ubJAE8RqT6VIx4osbrGnvPtjlVx7NA38ZXrc/H7EPbXzn7Xp6rro0F8dDpE1qO/UU77XeFRrKhDQ==",
	}

	// runAsUser := "test"
	runAsUser := "e0"
	// runAsUser := testutils.GetRunAsUser(t)
	// synActionPayload, _ := json.Marshal(bzshell.ShellOpenMessage{TargetUser: runAsUser})
	synActionPayload, _ := json.Marshal(bzshell.ShellActionParams{TargetUser: runAsUser})

	synPayload := ksmsg.SynPayload{
		SchemaVersion: ksmsg.SchemaVersion,
		Type:          string(ksmsg.Syn),
		Action:        "shell/default/open",
		ActionPayload: synActionPayload,
		TargetId:      "currently unused",
		Nonce:         "fake nonce",
		BZCert:        fakebzcert,
	}

	synKSMsg := ksmsg.KeysplittingMessage{
		Type:                ksmsg.Syn,
		KeysplittingPayload: synPayload,
	}

	err := synKSMsg.Sign(skStr)
	fmt.Println("err err synKSMsg.signature ", err)

	// Verify the signature
	if err := synKSMsg.VerifySignature(fakebzcert.ClientPublicKey); err != nil {
		fmt.Println("ffffffailed to verify SYN's signature: %w", err)
	}

	assert.Nil(t, err)

	fmt.Println("synKSMsg.signature ", synKSMsg.Signature)

	// // var synPayload ksmsg.KeysplittingMessage
	// synBytes, _ := json.Marshal(synKSMsg)

	return synKSMsg
}

type Kconfig struct{}

func (kc *Kconfig) GetPublicKey() string {
	return ""
}
func (kc *Kconfig) GetPrivateKey() string {
	return ""
}
func (kc *Kconfig) GetIdpProvider() string {
	return "google"
}
func (kc *Kconfig) GetIdpOrgId() string {
	return "abcdefg"
}
func (kc *Kconfig) GetServiceAccountJwksUrls() []string {
	return []string{}
}

func TestServiceAccl(t *testing.T) {

	var ksConfig Kconfig
	subLogger := logger.MockLogger()

	ks, err := keysplitting.New(subLogger, &ksConfig)
	assert.Nil(t, err)

	synMsg := CreateServiceAccSynMsg(t)
	fmt.Println(synMsg)

	err = ks.Validate(&synMsg)
	assert.Nil(t, err)
}
