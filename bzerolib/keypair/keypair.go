package keypair

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
)

func GenerateKeyPair() (PublicKey, PrivateKey, error) {
	if publicKey, privateKey, err := ed25519.GenerateKey(nil); err != nil {
		return PublicKey{}, PrivateKey{}, err
	} else {
		return PublicKey{publicKey: publicKey}, PrivateKey{privateKey: privateKey}, nil
	}
}

type PrivateKey struct {
	privateKey ed25519.PrivateKey
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
			privateKey: ed25519.PrivateKey(privateKeyBytes),
		}, nil
	} else {
		return nil, fmt.Errorf("malformatted private key of incorrect length: %d", len(privateKeyBytes))
	}
}

func (priv *PrivateKey) Sign(content []byte) string {
	sig := ed25519.Sign(priv.privateKey, content)
	return base64.StdEncoding.EncodeToString(sig)
}

func (priv *PrivateKey) Equals(key PrivateKey) bool {
	return bytes.Equal(priv.privateKey, key.privateKey)
}

func (priv *PrivateKey) String() string {
	return base64.StdEncoding.EncodeToString([]byte(priv.privateKey))
}

type PublicKey struct {
	publicKey ed25519.PublicKey
}

func PublicKeyFromString(publickey string) (*PublicKey, error) {
	publickeyBytes, err := base64.StdEncoding.DecodeString(publickey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 string: %s", publickey)
	}

	return &PublicKey{
		publicKey: ed25519.PublicKey(publickeyBytes),
	}, nil
}

func (pub *PublicKey) IsEmpty() bool {
	return pub.publicKey == nil
}

func (pub *PublicKey) String() string {
	return base64.StdEncoding.EncodeToString([]byte(pub.publicKey))
}

func (pub *PublicKey) Verify(content []byte, signature string) error {
	sigBits, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return err
	}

	if ok := ed25519.Verify(pub.publicKey, content, sigBits); ok {
		return nil
	} else {
		// TODO: should I actually print the content as well? Worried it's gonna be a mess or revealing
		return fmt.Errorf("invalid signature: signature: %s", signature)
	}
}
