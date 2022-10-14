package bzcert

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"

	"bastionzero.com/bctl/v1/bzerolib/keypair"
	"bastionzero.com/bctl/v1/bzerolib/keysplitting/bzcert"
	"bastionzero.com/bctl/v1/bzerolib/keysplitting/bzcert/zliconfig"
)

type IDaemonBZCert interface {
	bzcert.IBZCert
	Cert() *bzcert.BZCert
	PrivateKey() *keypair.PrivateKey
	Refresh() error
}

type DaemonBZCert struct {
	bzcert.BZCert

	// unexported members
	privateKey               *keypair.PrivateKey
	config                   *zliconfig.ZLIConfig
	currentIdTokenExpiration int64
}

func New(
	config *zliconfig.ZLIConfig,
) (*DaemonBZCert, error) {

	cert := &DaemonBZCert{
		config: config,
	}

	// Populate our BZCert with values taken from the zli config file
	if err := cert.populateFromConfig(); err != nil {
		return nil, fmt.Errorf("failed to initialize the BastionZero Certificate: %w", err)
	}

	return cert, nil
}

func (b *DaemonBZCert) Cert() *bzcert.BZCert {
	return &b.BZCert
}

func (b *DaemonBZCert) PrivateKey() *keypair.PrivateKey {
	return b.privateKey
}

func (b *DaemonBZCert) Refresh() error {
	// Only refresh if we have something expired
	if b.currentIdTokenExpiration > time.Now().UTC().Unix() {
		return nil
	}

	// Refresh our idp token values using the zli
	if err := b.config.Refresh(); err != nil {
		return err
	}

	if err := b.populateFromConfig(); err != nil {
		return err
	}

	return nil
}

func (b *DaemonBZCert) populateFromConfig() error {
	privateKey, err := keypair.PrivateKeyFromString(b.config.CertConfig.PrivateKey)
	if err != nil {
		return err
	}

	// Update all of our objects value
	b.InitialIdToken = b.config.CertConfig.InitialIdToken
	b.CurrentIdToken = b.config.TokenSet.CurrentIdToken
	b.ClientPublicKey = b.config.CertConfig.PublicKey
	b.Rand = b.config.CertConfig.CerRand
	b.SignatureOnRand = b.config.CertConfig.CerRandSignature
	b.privateKey = privateKey

	// Finally also check the bzcert is valid
	if err := b.Verify(b.config.CertConfig.OrgProvider, b.config.CertConfig.OrgIssuerId); err != nil {
		return err
	}

	// Track the expiration date for our current identity token
	parser := jwt.Parser{SkipClaimsValidation: true}
	claims := jwt.RegisteredClaims{}
	if _, _, err := parser.ParseUnverified(b.CurrentIdToken, &claims); err != nil {
		return fmt.Errorf("error trying to parse our jwt: %s", err)
	} else {
		b.currentIdTokenExpiration = claims.ExpiresAt.UTC().Unix() // Unix UTC timestamp
	}

	return b.HashCert()
}
