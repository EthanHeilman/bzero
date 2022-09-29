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
	hash, err := bzCert.HashCert()
	if err != nil {
		return nil, err
	}

	return &VerifiedBZCert{
		BZCert:     bzCert,
		expiration: exp,
		hash:       hash,
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
}

func (b *BZCert) HashCert() (string, error) {
	if hashBytes, ok := util.HashPayload(*b); !ok {
		return "", fmt.Errorf("failed to hash the certificate")
	} else {
		return base64.StdEncoding.EncodeToString(hashBytes), nil
	}
}
