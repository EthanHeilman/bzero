package pwdb

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"fmt"
	"log"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	"bastionzero.com/bctl/v1/bctl/agent/plugin/db/pwdb/client"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"github.com/bastionzero/go-toolkit/certificate"
	"github.com/bastionzero/go-toolkit/certificate/ca"
	"github.com/bastionzero/go-toolkit/certificate/splitclient"
	"github.com/bastionzero/go-toolkit/certificate/template"
)

const (
	rsaKeyLength = 2048
)

func tlsKeyPair(logger *logger.Logger, serviceUrl string, keyData data.KeyEntry, targetUser string) (tls.Certificate, error) {
	ret := tls.Certificate{}

	start := time.Now()

	// Load CA with agent's key shard
	agentCA, err := ca.Load(keyData.CaCertPem, keyData.KeyShardPem)
	if err != nil {
		return ret, fmt.Errorf("malformed ca certificate: %s", err)
	}

	// Generate key pair for our client certificate
	certKey, err := rsa.GenerateKey(rand.Reader, rsaKeyLength)
	if err != nil {
		return ret, fmt.Errorf("we fucked up generating the key: %s", err)
	}

	// Create a split certificate
	clientCertificateTemplate, _ := template.ClientCertificate(targetUser, time.Hour)
	clientCert, err := splitclient.Generate(rand.Reader, clientCertificateTemplate, agentCA.X509(), &certKey.PublicKey, agentCA.SplitPrivateKey())
	if err != nil {
		return ret, fmt.Errorf("failed to create new client certificate: %s", err)
	}

	logger.Infof("It took us %s to generate the client certificate with key size %d", time.Since(start).String(), rsaKeyLength)

	if err := clientCert.VerifySignature(agentCA.SplitPrivateKey().PublicKey); err != nil {
		log.Printf("this failed and we're glad it did")
	}

	signedCert, err := client.RequestSignature(serviceUrl, targetUser, clientCert, certKey.PublicKey, *agentCA.SplitPrivateKey())
	if err != nil {
		return ret, fmt.Errorf("failed to get bastion signature on client certificate: %s", err)
	}

	certPem, err := signedCert.PEM()
	if err != nil {
		return ret, fmt.Errorf("received signed certificate was not pem-encodable: %s", err)
	}

	keyPem, err := certificate.EncodeRSAPrivateKeyPEM(certKey)
	if err != nil {
		return ret, fmt.Errorf("failed to pem-encode the rsa private key: %s", err)
	}

	return tls.X509KeyPair([]byte(certPem), []byte(keyPem))
}
