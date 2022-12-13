package pwdb

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"fmt"
	"log"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/plugin/db/pwdb/client"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"github.com/bastionzero/go-toolkit/certificate"
	"github.com/bastionzero/go-toolkit/certificate/ca"
	"github.com/bastionzero/go-toolkit/certificate/splitclient"
	"github.com/bastionzero/go-toolkit/certificate/template"
)

const (
	rsaKeyLength = 4096
)

func tlsKeyPair(logger *logger.Logger, targetUser string) (tls.Certificate, error) {
	ret := tls.Certificate{}

	// Load CA with agent's key shard
	agentCA, err := ca.Load(caPem, agentShardPem)
	if err != nil {
		return ret, fmt.Errorf("failed to mock out agent's ca: %s", err)
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

	if err := clientCert.VerifySignature(agentCA.SplitPrivateKey().PublicKey); err != nil {
		log.Printf("this failed and we're glad it did")
	}

	signedCert, err := client.RequestSignature(targetUser, clientCert, certKey.PublicKey, *agentCA.SplitPrivateKey())
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
