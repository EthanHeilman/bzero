package bzcert

import (
	"encoding/base64"

	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"bastionzero.com/bctl/v1/bzerolib/mrtap/util"
	"github.com/stretchr/testify/mock"
	"gopkg.in/square/go-jose.v2"
)

// mocked version of the FileService
type MockBZCert struct {
	mock.Mock
}

func (m MockBZCert) Verify(idpProvider string, idpOrgId string) error {
	args := m.Called()
	return args.Error(0)
}

func (m MockBZCert) Hash() string {
	args := m.Called()
	cert := args.Get(0).(BZCert)
	hashBytes, _ := util.HashPayload(cert)
	return base64.StdEncoding.EncodeToString(hashBytes)
}

func (m MockBZCert) Expired() bool {
	args := m.Called()
	return args.Bool(0)
}

type jwtPayload struct {
	Aud           string `json:"aud"`
	Azp           string `json:"azp"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Exp           int64  `json:"exp"`
	Hd            string `json:"hd"`
	Iss           string `json:"iss"`
	Nonce         string `json:"nonce"`
	Sub           string `json:"sub"`
	Iat           int64  `json:"iat"`
}

type mrtapParams struct {
	pubkeyMrtap    string
	nonceMrtap     string
	randMrtap      string
	sigOnRandMrtap string
}

func createMockServiceAccountBzcert(keyPair *joseKeypair, jwksUrl string, email string, orgHD string, exp int64, mrtapValues mrtapParams) (*BZCert, error) {
	jwtPay := jwtPayload{
		Aud:           "110341072297225095816",
		Azp:           "110341072297225095816",
		Email:         email,
		EmailVerified: true,
		Exp:           exp,
		Hd:            orgHD,
		Iss:           email,
		Nonce:         mrtapValues.nonceMrtap,
		Sub:           "110341072297225095816",
		Iat:           1660762928,
	}

	pay, err := json.Marshal(jwtPay)
	if err != nil {
		return nil, err
	}

	opts := jose.SignerOptions{}
	opts.WithHeader("kid", keyPair.keyID)
	opts.WithHeader("jku", jwksUrl)
	opts.WithHeader("typ", "JWT")
	opts.WithHeader("zrv", "1.0")

	signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: keyPair.alg, Key: keyPair.priv}, &opts)

	jws, _ := signer.Sign(pay)
	signature, _ := jws.CompactSerialize()
	idT := string(signature)

	bzcert := BZCert{
		InitialIdToken:  idT,
		CurrentIdToken:  idT,
		ClientPublicKey: mrtapValues.pubkeyMrtap,
		Rand:            mrtapValues.randMrtap,
		SignatureOnRand: mrtapValues.sigOnRandMrtap,
	}
	return &bzcert, nil
}

// Inspired by the testing patterns used in oidc jwks test found at: https://github.com/coreos/go-oidc/blob/26c50372bf144421f269c09774d0f1324bee30e4/oidc/jwks_test.go#L59
type joseKeypair struct {
	keyID string // optional
	priv  interface{}
	pub   interface{}
	alg   jose.SignatureAlgorithm
}

func newMockRSAKeypair() (*joseKeypair, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 1028)

	if err != nil {
		return nil, err
	}
	return &joseKeypair{"", priv, priv.Public(), jose.RS256}, nil
}

func newJwksMockServer(keyPair *joseKeypair) *httptest.Server {
	webkey := jose.JSONWebKey{Key: keyPair.pub, Use: "sig", Algorithm: string(keyPair.alg), KeyID: keyPair.keyID}
	keySet := jose.JSONWebKeySet{}
	keySet.Keys = append(keySet.Keys, webkey)

	return httptest.NewServer(&jwksHandler{keys: keySet})
}

type jwksHandler struct {
	keys       jose.JSONWebKeySet
	setHeaders func(h http.Header)
}

func (k *jwksHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if k.setHeaders != nil {
		k.setHeaders(w.Header())
	}
	if err := json.NewEncoder(w).Encode(k.keys); err != nil {
		panic(err)
	}
}