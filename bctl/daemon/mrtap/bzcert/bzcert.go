package bzcert

import (
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"

	"bastionzero.com/bzerolib/keypair"
	"bastionzero.com/bzerolib/mrtap/bzcert"
	"bastionzero.com/bzerolib/mrtap/util"

	"bastionzero.com/daemon/mrtap/bzcert/zliconfig"
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
	privatekey := b.config.CertConfig.PrivateKey
	privatekeyBytes, err := base64.StdEncoding.DecodeString(privatekey)
	if err != nil {
		return fmt.Errorf("private key is not base64 encoded: %w", err)
	}

	// The golang ed25519 library only generates and accepts ed25519 certificates that
	// are in the form privatekey + publickey and therefore have length 64. The library
	// we use to generate these in the zli (https://paulmillr.com/noble/) creates them
	// with 32-bytes and so we correct that here
	if len(privatekeyBytes) == 32 {
		publicKeyBytes, err := base64.StdEncoding.DecodeString(b.config.CertConfig.PublicKey)
		if err != nil {
			return fmt.Errorf("public key is not base64 encoded: %w", err)
		}
		privatekey = base64.StdEncoding.EncodeToString(append(privatekeyBytes, publicKeyBytes...))
	}

	privateKey, err := keypair.PrivateKeyFromString(privatekey)
	if err != nil {
		return err
	}

	// Update all of our objects values
	b.InitialIdToken = b.config.CertConfig.InitialIdToken
	b.CurrentIdToken = b.config.TokenSet.CurrentIdToken
	b.ClientPublicKey = b.config.CertConfig.PublicKey
	b.Rand = b.config.CertConfig.CerRand
	b.SignatureOnRand = b.config.CertConfig.CerRandSignature
	b.privateKey = privateKey

	jwksUrlPatterns := []string{}

	// Track the expiration date for our current identity token
	parser := jwt.Parser{SkipClaimsValidation: true}
	claims := jwt.RegisteredClaims{}
	var jwt, _, jwtErr = parser.ParseUnverified(b.CurrentIdToken, &claims)
	if jwtErr != nil {
		return fmt.Errorf("error trying to parse our jwt: %s", err)
	}

	if jku, ok := jwt.Header["jku"]; ok {
		jkuStr := fmt.Sprintf("%v", jku)
		jwksUrlPattern, _, err := util.ExtractJwksUrlPattern(jkuStr)
		if err != nil {
			return fmt.Errorf("error trying to parse JWKS URL pattern from badly formatted JWKS URL: %s", err)
		}
		jwksUrlPatterns = append(jwksUrlPatterns, jwksUrlPattern)
	}

	// Finally also check the bzcert is valid
	if err := b.Verify(b.config.CertConfig.OrgProvider, b.config.CertConfig.OrgIssuerId, jwksUrlPatterns); err != nil {
		return err
	}

	// Track the expiration date for our current identity token
	b.currentIdTokenExpiration = claims.ExpiresAt.UTC().Unix() // Unix UTC timestamp

	return b.HashCert()
}
