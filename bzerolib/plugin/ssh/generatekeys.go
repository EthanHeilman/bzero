package ssh

// functions for handling SSH keypairs
// based on: https://gist.github.com/devinodaniel/8f9b8a4f31573f428f29ec0e884e6673

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"bastionzero.com/bzerolib/filelock"
	"bastionzero.com/bzerolib/logger"
	"golang.org/x/crypto/ssh"
)

const bitSize int = 4096

// create an RSA keypair and marshal them into byte arrays
func GenerateKeys() ([]byte, []byte, error) {
	privateKey, err := generatePrivateKey(bitSize)
	if err != nil {
		return nil, nil, err
	}

	publicKeyBytes, err := GeneratePublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, nil, err
	}

	privateKeyBytes := encodePrivateKeyToPem(privateKey)

	return privateKeyBytes, publicKeyBytes, nil
}

// generatePrivateKey creates a RSA Private Key of specified byte size
func generatePrivateKey(bitSize int) (*rsa.PrivateKey, error) {
	// Private Key generation
	privateKey, err := rsa.GenerateKey(rand.Reader, bitSize)
	if err != nil {
		return nil, err
	}

	// Validate Private Key
	err = privateKey.Validate()
	if err != nil {
		return nil, err
	}

	return privateKey, nil
}

// RSA -> PEM
func encodePrivateKeyToPem(privateKey *rsa.PrivateKey) []byte {
	// Get ASN.1 DER format
	privateDer := x509.MarshalPKCS1PrivateKey(privateKey)

	// pem.Block
	privateBlock := pem.Block{
		Type:    "RSA PRIVATE KEY",
		Headers: nil,
		Bytes:   privateDer,
	}

	// Private key in PEM format
	privatePem := pem.EncodeToMemory(&privateBlock)

	return privatePem
}

// PEM -> RSA
func decodePemToPrivateKey(privatePem []byte) (*rsa.PrivateKey, error) {
	if privateBlock, _ := pem.Decode(privatePem); privateBlock == nil {
		return nil, fmt.Errorf("error decoding private key: No PEM data was found")
	} else {
		return x509.ParsePKCS1PrivateKey(privateBlock.Bytes)
	}
}

// generatePublicKey take a rsa.PublicKey and return bytes suitable for writing to .pub file
// returns in the format "ssh-rsa ..."
func GeneratePublicKey(publicKey *rsa.PublicKey) ([]byte, error) {
	publicRsaKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		return nil, err
	}

	pubKeyBytes := ssh.MarshalAuthorizedKey(publicRsaKey)

	return pubKeyBytes, nil
}

// takes an encoded private key and returns a public key struct
// returns an error if the key cannot be read or is invalid
func ReadPublicKeyRsa(privatePem []byte) (*rsa.PublicKey, error) {
	if privateKey, err := decodePemToPrivateKey(privatePem); err != nil {
		return nil, err
	} else {
		return &privateKey.PublicKey, privateKey.Validate()
	}
}

// tries to return an SSH keypair based on the given identityfile
// if that fails, create a new keypair and update the identityfile
func SetUpKeys(identityFile IIdentityFile, fileLock *filelock.FileLock, logger *logger.Logger) (privateKey []byte, publicKey []byte, err error) {
	lock, err := fileLock.AcquireLock()
	if err != nil {
		return nil, nil, err
	}
	defer lock.Unlock()

	useExistingKeys := false
	// if any of the following steps fail, we need to generate new keys
	if privateKey, err = identityFile.GetKey(); err != nil {
		logger.Errorf("failed to retrieve identity file: %s", err)
	} else if publicKeyRsa, err := ReadPublicKeyRsa(privateKey); err != nil {
		logger.Errorf("failed to decode identity file: %s", err)
	} else if publicKey, err = GeneratePublicKey(publicKeyRsa); err != nil {
		logger.Errorf("failed to decode public key: %s", err)
	} else {
		logger.Debugf("using existing temporary keys")
		useExistingKeys = true
	}
	if !useExistingKeys {
		privateKey, publicKey, err = GenerateKeys()
		if err != nil {
			return nil, nil, fmt.Errorf("error generating temporary keys: %s", err)
		} else if err := identityFile.SetKey(privateKey); err != nil {
			return nil, nil, fmt.Errorf("error writing temporary private key: %s", err)
		}
	}
	return privateKey, publicKey, nil
}
