package keypair

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

type PrivateKey struct {
	key ed25519.PrivateKey
}

func PrivateKeyFromString(privatekey string) (*PrivateKey, error) {
	privateKeyBytes, err := base64.StdEncoding.DecodeString(privatekey)
	if err != nil {
		return nil, fmt.Errorf("failed to base64 decode private key: %w", err)
	}

	// The golang ed25519 library uses a length 64 private key because the
	// private key is in the concatenated form privatekey = privatekey + publickey.
	if len(privateKeyBytes) == 64 {
		return &PrivateKey{
			key: ed25519.PrivateKey(privateKeyBytes),
		}, nil
	} else {
		return nil, fmt.Errorf("private keys should be in the 64 byte, concatenated privatekey + publickey form, but this key had an incorrect length: %d", len(privateKeyBytes))
	}
}

func (p *PrivateKey) Sign(content []byte) string {
	sig := ed25519.Sign(p.key, content)
	return base64.StdEncoding.EncodeToString(sig)
}

func (p *PrivateKey) Equals(key PrivateKey) bool {
	return bytes.Equal(p.key, key.key)
}

func (p *PrivateKey) String() string {
	if p == nil || p.key == nil {
		return ""
	}

	return base64.StdEncoding.EncodeToString([]byte(p.key))
}

func (p *PrivateKey) UnmarshalJSON(data []byte) error {
	// Ignore null, like in the main JSON package.
	if string(data) == "null" || string(data) == `""` {
		return nil
	}

	peeled := strings.TrimSuffix(strings.TrimPrefix(string(data), "\""), "\"")
	key, err := PrivateKeyFromString(peeled)
	if err == nil {
		*p = *key
	}
	return err
}

func (p *PrivateKey) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.String())
}
