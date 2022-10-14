package keypair

import (
	"crypto/ed25519"
)

func GenerateKeyPair() (*PublicKey, *PrivateKey, error) {
	if publicKey, privateKey, err := ed25519.GenerateKey(nil); err != nil {
		return &PublicKey{}, &PrivateKey{}, err
	} else {
		return &PublicKey{key: publicKey}, &PrivateKey{key: privateKey}, nil
	}
}
