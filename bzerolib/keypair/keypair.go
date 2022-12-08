package keypair

import (
	"crypto/ed25519"
)

func GenerateKeyPair() (*PublicKey, *PrivateKey, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	return &PublicKey{key: publicKey}, &PrivateKey{key: privateKey}, err
}
