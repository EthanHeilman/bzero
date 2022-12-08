package keypair

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

type PublicKey struct {
	key ed25519.PublicKey
}

func PublicKeyFromString(publickey string) (*PublicKey, error) {
	publickeyBytes, err := base64.StdEncoding.DecodeString(publickey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 string: |%s|", publickey)
	}

	if len(publickeyBytes) != 32 {
		return nil, fmt.Errorf("incorrect public key size: %d", len(publickeyBytes))
	}

	return &PublicKey{
		key: ed25519.PublicKey(publickeyBytes),
	}, nil
}

func (p *PublicKey) IsEmpty() bool {
	if p == nil {
		return true
	}
	return p.key == nil
}

func (p *PublicKey) String() string {
	if p == nil || p.key == nil {
		return ""
	}

	return base64.StdEncoding.EncodeToString([]byte(p.key))
}

func (p *PublicKey) Verify(content []byte, signature string) bool {
	sigBits, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false
	}

	return ed25519.Verify(p.key, content, sigBits)
}

func (p *PublicKey) UnmarshalJSON(data []byte) error {
	// Ignore null, like in the main JSON package.
	if string(data) == "null" || string(data) == `""` {
		return nil
	}

	peeled := strings.TrimSuffix(strings.TrimPrefix(string(data), "\""), "\"")
	key, err := PublicKeyFromString(peeled)
	if err == nil {
		*p = *key
	}
	return err
}

func (p *PublicKey) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.String())
}
