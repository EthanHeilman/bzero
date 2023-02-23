package pwdb

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/config/keyshardconfig/data"
	"bastionzero.com/bctl/v1/bzerolib/plugin/db"
	"github.com/bastionzero/go-toolkit/certificate"
	"github.com/bastionzero/go-toolkit/certificate/ca"
	"github.com/bastionzero/go-toolkit/certificate/splitclient"
)

const (
	rsaKeyLength = 2048
)

// This certificate is defined by the requirements as used by postgres. Postgres will log you in as whatever user
// you specify as the CommonName, although other databases have different requirements. MongoDB Atlas has you manually
// specify the entire Subject which might include more than just the CommonName (CN).
func generateClientCertificate(username string, lifetime time.Duration) (*x509.Certificate, error) {
	serialNumber, err := certificate.GenerateSerialNumber()
	if err != nil {
		return nil, err
	}

	return &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: username,
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(lifetime),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}, nil
}

func (p *Pwdb) generateClientCert(keyData data.KeyEntry, targetUser string) (tls.Certificate, error) {
	ret := tls.Certificate{}

	start := time.Now()

	// Load CA with agent's key shard
	agentCA, err := ca.Load(keyData.CaCertPem, keyData.KeyShardPem)
	if err != nil {
		return ret, fmt.Errorf("malformed ca certificate: %w", err)
	}

	// Generate key pair for our client certificate
	certKey, err := rsa.GenerateKey(rand.Reader, rsaKeyLength)
	if err != nil {
		return ret, fmt.Errorf("error generating rsa key: %w", err)
	}

	// Generate a template
	ccTemplate, err := generateClientCertificate(targetUser, time.Hour)
	if err != nil {
		return ret, fmt.Errorf("failed to generate client certificate template: %w", err)
	}

	// Use our template to generate a partially-signed split certificate
	clientCert, err := splitclient.Generate(rand.Reader, ccTemplate, agentCA.X509(), &certKey.PublicKey, agentCA.SplitPrivateKey())
	if err != nil {
		return ret, fmt.Errorf("failed to create new client certificate: %w", err)
	}

	p.logger.Infof("Generated SplitCert in %s with key size %d", time.Since(start).Round(time.Millisecond).String(), rsaKeyLength)

	signedCert, err := p.bastion.RequestCosign(targetUser, clientCert, certKey.PublicKey, *agentCA.SplitPrivateKey())
	if err != nil {
		return ret, db.NewClientCertCosignError(err)
	}

	certPem, err := signedCert.PEM()
	if err != nil {
		return ret, fmt.Errorf("received signed certificate was not pem-encodable: %w", err)
	}

	keyPem, err := certificate.EncodeRSAPrivateKeyPEM(certKey)
	if err != nil {
		return ret, fmt.Errorf("failed to pem-encode the rsa private key: %w", err)
	}

	return tls.X509KeyPair([]byte(certPem), []byte(keyPem))
}
