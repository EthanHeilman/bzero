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

	if len(publickeyBytes) < 32 {
		return nil, fmt.Errorf("public key has invalid size")
	}

	return &PublicKey{
		key: ed25519.PublicKey(publickeyBytes),
	}, nil
}

func (p *PublicKey) IsEmpty() bool {
	return p.key == nil
}

func (p *PublicKey) String() string {
	if p.key == nil {
		return ""
	}

	return base64.StdEncoding.EncodeToString([]byte(p.key))
}

func (p *PublicKey) Verify(content []byte, signature string) error {
	sigBits, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return err
	}

	if ok := ed25519.Verify(p.key, content, sigBits); ok {
		return nil
	} else {
		// TODO: should I actually print the content as well? Worried it's gonna be a mess or revealing
		return fmt.Errorf("invalid signature: signature: %s", signature)
	}
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
	bs, _ := json.Marshal(p.String())
	return bs, nil
}
