package bzcert

import (
	"encoding/base64"
	"fmt"
	"time"

	"bastionzero.com/bctl/v1/bzerolib/keysplitting/util"
)

type VerifiedBZCert struct {
	*BZCert

	expiration time.Time
	hash       string
}

func NewVerifiedBZCert(bzCert *BZCert, exp time.Time) (*VerifiedBZCert, error) {
	hashBytes, ok := util.HashPayload(*bzCert)
	if !ok {
		return nil, fmt.Errorf("failed to hash the certificate")
	}

	return &VerifiedBZCert{
		BZCert:     bzCert,
		expiration: exp,
		hash:       base64.StdEncoding.EncodeToString(hashBytes),
	}, nil
}

func (b *VerifiedBZCert) Hash() string {
	return b.hash
}

func (b *VerifiedBZCert) Expired() bool {
	return time.Now().After(b.expiration)
}

type BZCert struct {
	InitialIdToken  string `json:"initialIdToken"`
	CurrentIdToken  string `json:"currentIdToken"`
	ClientPublicKey string `json:"clientPublicKey"`
	Rand            string `json:"rand"`
	SignatureOnRand string `json:"signatureOnRand"`

	// // unexported members
	// expiration time.Time
	// hash       string
}

// func (b *BZCert) Hash() string {
// 	return b.hash
// }

// func (b *BZCert) Expired() bool {
// 	return time.Now().After(b.expiration)
// }

// func (b *BZCert) Verify(idpProvider string, idpOrgId string) error {
// 	// initialize a new verifier for BastionZero certificates
// 	if verifier, err := NewVerifier(idpProvider, idpOrgId); err != nil {
// 		return fmt.Errorf("error initializing certificate verifier: %w", err)
// 	} else if exp, err := verifier.Verify(b); err != nil {
// 		return fmt.Errorf("failed to verify the certificate: %w", err)
// 	} else if err := b.HashCert(); err != nil {
// 		return err
// 	} else {
// 		b.expiration = exp
// 	}

// 	return nil
// }

// func (b *BZCert) HashCert() error {

// }
