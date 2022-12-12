package certificate

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"

	"bastionzero.com/bctl/v1/bzerolib/connection/httpclient"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"github.com/bastionzero/go-toolkit/certificate/ca"
	"github.com/bastionzero/go-toolkit/certificate/splitcertificate"
	"github.com/bastionzero/go-toolkit/certificate/template"
	"golang.org/x/crypto/sha3"
)

const (
	certificateServiceEndpoint = "https://lucie-certificate-service.bastionzero.com/generate/client"
	rootCertHash               = "v3R6c5gJMEUCGrh743C7GfV9TjaGi1odcz0anP03zbA="
	rsaKeyLength               = 4096
)

type Certificate struct {
	logger     *logger.Logger
	CACert     []byte
	ClientCert []byte
	ClientKey  []byte
}

type ClientCertificateRequest struct {
	TargetUser        string
	ClientCertificate splitcertificate.SplitSignCertificate
	PublicKey         rsa.PublicKey
	KeyShardHash      string
}

type ClientCertificateResponse struct {
	ClientCertificate splitcertificate.SplitSignCertificate
}

func (c *Certificate) TLSKeyPair() (tls.Certificate, error) {
	c.logger.Infof("%s", string(c.ClientCert))
	c.logger.Infof("%s", string(c.ClientKey))

	return tls.X509KeyPair(c.ClientCert, c.ClientKey)
}

func New(logger *logger.Logger, targetUser string) (*Certificate, error) {
	// Load CA with agent's key shard
	agentCA, err := ca.Load(caPem, agentShardPem)
	if err != nil {
		return nil, fmt.Errorf("failed to mock out agent's ca: %s", err)
	}

	// Generate key pair for our client certificate
	certKey, err := rsa.GenerateKey(rand.Reader, rsaKeyLength)
	if err != nil {
		return nil, fmt.Errorf("we fucked up generating the key: %s", err)
	}

	// Create a split certificate
	clientCertificateTemplate, _ := template.ClientCertificate("postgres")
	clientCert, err := splitcertificate.New(rand.Reader, clientCertificateTemplate, agentCA.X509(), &certKey.PublicKey, agentCA.PrivateKey())
	if err != nil {
		return nil, fmt.Errorf("failed to create new client certificate: %s", err)
	}

	if err := clientCert.VerifySignature(agentCA.PrivateKey().PublicKey); err != nil {
		log.Printf("this failed and we're glad it did")
	}

	// just use the hash of the certificate for testing
	agentKeyPem, err := agentCA.PrivateKey().EncodePEM()
	if err != nil {
		return nil, fmt.Errorf("failed to encode split private key: %s", err)
	}

	hash := sha3.Sum256([]byte(agentKeyPem))
	agentKeyHash := base64.StdEncoding.EncodeToString(hash[:])

	req := ClientCertificateRequest{
		TargetUser:        targetUser,
		ClientCertificate: *clientCert,
		PublicKey:         *agentCA.PrivateKey().PublicKey,
		KeyShardHash:      agentKeyHash,
	}

	logger.Infof("target user: %s", targetUser)

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("error marshalling request to sign client certificate request: %s", err)
	}

	client, err := httpclient.New(certificateServiceEndpoint, httpclient.HTTPOptions{
		Body: bytes.NewBuffer(reqBytes),
	})
	if err != nil {
		return nil, fmt.Errorf("error while instantiating http client: %s", err)
	}

	rsp, err := client.Post(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to get signed certificate: %s", err)
	}

	var certResponse ClientCertificateResponse
	if err := json.NewDecoder(rsp.Body).Decode(&certResponse); err != nil {
		return nil, fmt.Errorf("malformed certificate response: %s", err)
	}

	// logger.Infof("Client Certificate: %s", string(certResponse.ClientCertificate))
	// logger.Infof("Client key: %s", string(certResponse.ClientKey))
	xcert, err := certResponse.ClientCertificate.X509()
	if err != nil {
		return nil, fmt.Errorf("failed to convert cert to x509: %s", err)
	}

	return &Certificate{
		logger:     logger,
		CACert:     []byte(caPem),
		ClientCert: []byte(encodeCertificatePEM(xcert)),
		ClientKey:  []byte(encodeRSAPrivateKeyPEM(certKey)),
	}, nil
}

func encodeRSAPrivateKeyPEM(key *rsa.PrivateKey) string {
	keyPEM := new(bytes.Buffer)
	pem.Encode(keyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return keyPEM.String()
}

func encodeCertificatePEM(cert *x509.Certificate) string {
	certPEM := new(bytes.Buffer)
	pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})

	return certPEM.String()
}

var caPem = `-----BEGIN CERTIFICATE-----
MIIFczCCA1ugAwIBAgIRAO3iEuEjCVN8M3/XrCfs/0wwDQYJKoZIhvcNAQELBQAw
UzEMMAoGA1UEBhMDVVNBMRYwFAYDVQQIEw1NYXNzYWNodXNldHRzMQ8wDQYDVQQH
EwZCb3N0b24xGjAYBgNVBAoTEUJhc3Rpb25aZXJvLCBJbmMuMB4XDTIyMTIwOTIx
MzMyMloXDTIzMTIwOTIxMzMyMlowUzEMMAoGA1UEBhMDVVNBMRYwFAYDVQQIEw1N
YXNzYWNodXNldHRzMQ8wDQYDVQQHEwZCb3N0b24xGjAYBgNVBAoTEUJhc3Rpb25a
ZXJvLCBJbmMuMIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAsntkqD7B
ZTGMGiCjzc+bUYZ1ZNQPh8En/0Xt2E9gpgwGbMufMjMsyagAQ+dLe99SVo7H4YJV
bLR8WQa2v/BnzhT9QDSVNXdx6N+uru5lvB9Btwc+o6q1huIvm/ub3oxeaUZrLr6P
uiRMHXHdcXgp+TxWNzZZP8CXPyhgKw02PTTAa5aaVSJbwa67UFllrbdErDPtnIrD
op4n0FJ4G9uNlD0Uj8a430oc9joaiwB5iVTcffAoWS3cQjMpSveucdLM/XZRxboZ
eDrPvioqj1Lwq7/fnQsmXXLIFtuwD3maz6BEOsxDTY+gYQO/JZ/zlB8tFW01/IDd
Pke4r6V2PBYWu9+g7iBiyMcJDg8ShBpOPiRJ5mpQtBndK/gTJ+W66nreZMpHtLuZ
05Iw2+8Ai6ci/u/2dLG/5WCmfLt0yhD5ZkklHhwO3YCtd8YXfB/DwjO39uCn+iQ9
W5aio0FDxp8hAmRBLHpe25BWPNESBrKXHwADtz6Z5GpK+RFc4r9WoguCjWZs50zE
yCOL2sNvfMPul32BxWzWa3Q54AJwkojXirhcRUDis2iEfK3Pa6dSer8aVeaQjEz2
bPv+9H11nzGSlOKEfbLEfkSCWoKOYyrdAeaDzkKhmXpKYOk3psgl7hcGaIa2y0KJ
1LQrwcZQK/gBrr5gbs4OZU8yJNThbXmlkysCAwEAAaNCMEAwDgYDVR0PAQH/BAQD
AgIEMA8GA1UdEwEB/wQFMAMBAf8wHQYDVR0OBBYEFHPaDYWQuBxAghxQn1bQeY9y
Z0AnMA0GCSqGSIb3DQEBCwUAA4ICAQBUeV3OVSHG6sefv9OvRT9pXax71Zq5w7j5
5i5S5nh4ysv87T2Q6Dk+DQRmUo7TSRS48URDxj/jVw6/uBsxkCbykZq3/8D4YBh3
+QdvF5yKm0EWygDlZjdVlIGlh7Fh0/lHhy+r/reII0129AakCSmr8cmaXaF92Bm3
PdwqTx+J9qUsRQeL87J8/t2taUl18nR9c1s8YerRnbM5/V2RHz5V5bIObPeinRif
qO+YXWp5qyG2JqzZVTQhd4IbkDiIy/qIY1Gs7H0mib87qPyXwv5PL7X21yF7BajT
AF3lpvrjatOPHATyN9vAQwE5EsI1kJYZwXgSQ/b/WD8SCV5D9EhqTw1VRerOL57W
KgN1V5oNAlIsq9YaryesY8OjsclzliWah2xHGMc+S0iOYF/5uWKKmemsbHbNqdT/
+hoEgP+jkwHo9QJKZZqlcFDEHoIS/0E7FcYULpRs0YWCf3gob8/YMTrZWX3US1XU
N5KdiuC9R10hQIJoASjkgm3qblGfqyEoDmEptxGcxsD5oJK5/xJVT88eM4GBl3gZ
ho9k8C9SAVARoqjFwONDiFUE9a03UpsPZAoAKtdRn+maRbgQrcIVS6A6EipHQkRw
3kCnMT3lFcpM/ArIKTOW22A3c3FDu9eDO/VcNMrG4Gw5/65QgU7BKGSZL13zxyzG
2G7eNm40gQ==
-----END CERTIFICATE-----`

var agentShardPem = `-----BEGIN RSA SPLIT PRIVATE KEY-----
MIIEETCCAgkEggIAsntkqD7BZTGMGiCjzc+bUYZ1ZNQPh8En/0Xt2E9gpgwGbMuf
MjMsyagAQ+dLe99SVo7H4YJVbLR8WQa2v/BnzhT9QDSVNXdx6N+uru5lvB9Btwc+
o6q1huIvm/ub3oxeaUZrLr6PuiRMHXHdcXgp+TxWNzZZP8CXPyhgKw02PTTAa5aa
VSJbwa67UFllrbdErDPtnIrDop4n0FJ4G9uNlD0Uj8a430oc9joaiwB5iVTcffAo
WS3cQjMpSveucdLM/XZRxboZeDrPvioqj1Lwq7/fnQsmXXLIFtuwD3maz6BEOsxD
TY+gYQO/JZ/zlB8tFW01/IDdPke4r6V2PBYWu9+g7iBiyMcJDg8ShBpOPiRJ5mpQ
tBndK/gTJ+W66nreZMpHtLuZ05Iw2+8Ai6ci/u/2dLG/5WCmfLt0yhD5ZkklHhwO
3YCtd8YXfB/DwjO39uCn+iQ9W5aio0FDxp8hAmRBLHpe25BWPNESBrKXHwADtz6Z
5GpK+RFc4r9WoguCjWZs50zEyCOL2sNvfMPul32BxWzWa3Q54AJwkojXirhcRUDi
s2iEfK3Pa6dSer8aVeaQjEz2bPv+9H11nzGSlOKEfbLEfkSCWoKOYyrdAeaDzkKh
mXpKYOk3psgl7hcGaIa2y0KJ1LQrwcZQK/gBrr5gbs4OZU8yJNThbXmlkysCAwEA
AQSCAgARpSU/x2EyQ4EUlsYhBjMbmFN2gpTcuOuuW/VlHaE+aceEUk32NVMZX1fh
tuKw04oktGV7kpLuS7VazR04BwWFU+TZYi/o85IS5+R0QQRI7NAX0jRXRSt9hNna
Ga4MwJbJaMp8P16VaeQX4ZeQuW3pHYHca9yBQZGiYC/rrfCNfSaNxgQeRGUNJ12S
ulW4Ioai9EhxqOV5Ixe7DYUxovKBJOlWlNxUySTyS9k2srDiA6hQoi/bsj9iFESF
741sagHHXSB10bubVsBwP4Ib+9aBocJdMXFCra6A22C+y8IDFaPhsvR5ez/Fk2Pl
qyfN4KbdTF4UsO4qvkZn3HyVduBFkeX/peJY5nYcuv2GOHdwDi5z5wWd0SQ5O6nc
ZB4B3KScQuKuBvkyR7hyEun+cLTCjeLJj71kwcB30CqKNS0mG8G1JbybYJd9xJuB
v+5vKaTilAk+h/th9tcrDN9LauuV4UehQzkuLrgidO3pz6th/uSnoiugoFRx0tVF
skFnpE2cMcHjyqDOAGGoDO5rFEIysLZ+VtclJ6vsGTSn0DWHpsPQJ85i9UXNpEvB
KJSbbDhtxU/euTaoqQ8EQFVpJEDKUyE0HpZ9STczUB1PMA6kt9OrGjiPIj3EtRUF
3bmC86iIokMZ/AcWuC64Xgog+W+J9xDBfutqeDkNmsN+q9KkNg==
-----END RSA SPLIT PRIVATE KEY-----`
