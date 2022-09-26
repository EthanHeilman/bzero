package messagesigner

import (
	ed "crypto/ed25519"
	"encoding/base64"
	"fmt"
)

type IMessageSigner interface {
	SignMessage(content []byte) (string, error)
}

type MessageSigner struct {
	privateKey []byte
}

func New(
	privateKey []byte,
) (*MessageSigner, error) {

	if len(privateKey) != 64 {
		return nil, fmt.Errorf("invalid private key length: %v", len(privateKey))
	}

	return &MessageSigner{
		privateKey: ed.PrivateKey(privateKey),
	}, nil
}

func (s *MessageSigner) SignMessage(content []byte) (string, error) {
	sig := ed.Sign(s.privateKey, content[:])

	// Convert the signature to base64 string
	sigBase64 := base64.StdEncoding.EncodeToString(sig)

	return sigBase64, nil
}
