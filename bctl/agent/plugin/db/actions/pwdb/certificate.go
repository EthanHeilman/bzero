package pwdb

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"fmt"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	"bastionzero.com/bctl/v1/bctl/agent/plugin/db/actions/pwdb/client"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/plugin/db"
	"github.com/bastionzero/go-toolkit/certificate"
	"github.com/bastionzero/go-toolkit/certificate/ca"
	"github.com/bastionzero/go-toolkit/certificate/splitclient"
	"github.com/bastionzero/go-toolkit/certificate/template"
)

const (
	rsaKeyLength = 2048
)

func generateClientCert(logger *logger.Logger, bastion *client.BastionClient, keyData data.KeyEntry, targetUser string) (tls.Certificate, error) {
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

	// Create a split certificate
	clientCertificateTemplate, _ := template.ClientCertificate(targetUser, time.Hour)
	clientCertificateTemplate.DNSNames = []string{"localhost"}
	clientCertificateTemplate.Issuer.CommonName = ""
	clientCert, err := splitclient.Generate(rand.Reader, clientCertificateTemplate, agentCA.X509(), &certKey.PublicKey, agentCA.SplitPrivateKey())
	if err != nil {
		return ret, fmt.Errorf("failed to create new client certificate: %w", err)
	}

	logger.Infof("It took %s to generate the client certificate with key size %d", time.Since(start).Round(time.Millisecond).String(), rsaKeyLength)
	logger.Infof("Generated SplitCert client certificate")

	signedCert, err := bastion.RequestCosign(targetUser, clientCert, certKey.PublicKey, *agentCA.SplitPrivateKey())
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
