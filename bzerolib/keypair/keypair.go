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

func (priv *PrivateKey) Sign(content []byte) string {
	sig := ed25519.Sign(priv.privateKey, content)
	return base64.StdEncoding.EncodeToString(sig)
}

func (priv *PrivateKey) Equals(key PrivateKey) bool {
	return bytes.Equal(priv.privateKey, key.privateKey)
}

type PublicKey struct {
	publicKey ed25519.PublicKey
}

func (pub *PublicKey) IsEmpty() bool {
	return len([]byte(pub.publicKey)) == 0
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
