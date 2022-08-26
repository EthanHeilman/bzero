package bzcert

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/square/go-jose.v2"
)

var globalKeySet oidc.KeySet

func TestDZcerVerifier(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Zcer Verifier Suite")
}

var _ = Describe("NewBZCertVerifier.AttemptJwksVerification", Ordered, func() {

	Context("AttemptJwksVerification verification logic and errors", func() {

		mrzapValues := MrzapParams{
			pubkeyMrzap:    "aOx9mfXvmQeaJmpIK1KxH/ghsciTa42O3IDcuNfZMtk=",
			nonceMrzap:     "/RZbHd5AdEHX7LwMpMwu32iv80Lppeu0tL9ZBwcwBFg=",
			randMrzap:      "DJz3yRJmTSTLoDj4SE7KcKBzR4O8KwkYYngNNoS0bW0=",
			sigOnRandMrzap: "HlZxmpGN5mS0RGBnZfJ/1VdeF2MSXQS2F8fTh1fPsgXCEvV1spLBo+lJQQYjt4dbULTIFfFgvcYeIxM/QfxDAQ==",
		}

		urlprefix := "abcdef"
		emailplaceholder := "*@example.com" //TODO: document this format in design doc
		serviceAccountEmail := "aliceserviceaccount@example.com"
		idpOrgId := "exampleCo"
		idpProvider := "google"
		exp := int64(time.Now().Add(time.Hour).Unix())

		keyPair, err := newRSAKeypair()
		Expect(err).To(BeNil())

		s := newJwksMockServer(keyPair)

		jwksRootUrl := s.URL + "/" + urlprefix + "/" + emailplaceholder
		jwksUrl := s.URL + "/" + urlprefix + "/" + serviceAccountEmail

		When("Supplied with a valid service account MRZAP Zcer", func() {
			zCer, err := CreateServiceAccountZcer(keyPair, jwksUrl, serviceAccountEmail, idpOrgId, exp, mrzapValues)
			Expect(err).To(BeNil())
			verifier := BZCertVerifier{
				orgId:               idpOrgId,
				orgProvider:         ProviderType(idpProvider),
				issUrl:              "",
				cert:                zCer,
				allowedJwksUrlRoots: map[string]bool{},
			}
			otherJwksRootUrl := "https://example.com/abcd/project2/"
			verifier.AddServiceAccountJwksRootUrl(otherJwksRootUrl)
			verifier.AddServiceAccountJwksRootUrl(jwksRootUrl)

			It("performs Zcer vertification check and should succeed", func() {
				By("calling AttemptJwksVerification directly")
				verified, err := verifier.VerifySericeAccountIdToken(zCer.InitialIdToken)
				Expect(verified).To(BeTrue())
				Expect(err).To(BeNil())
			})
		})

		When("Supplied with a service account MRZAP Zcer with an invalid orgId", func() {
			wrongIdpOrgId := "eveCorp"
			Expect(idpOrgId).ToNot(BeEquivalentTo(wrongIdpOrgId))

			zCer, err := CreateServiceAccountZcer(keyPair, jwksUrl, serviceAccountEmail, wrongIdpOrgId, exp, mrzapValues)
			Expect(err).To(BeNil())
			verifier := BZCertVerifier{
				orgId:               idpOrgId,
				orgProvider:         ProviderType(idpProvider),
				issUrl:              "",
				cert:                zCer,
				allowedJwksUrlRoots: map[string]bool{},
			}
			verifier.AddServiceAccountJwksRootUrl(jwksRootUrl)

			It("performs Zcer vertification check and should fail", func() {
				By("calling AttemptJwksVerification directly")
				verified, err := verifier.VerifySericeAccountIdToken(zCer.InitialIdToken)
				Expect(verified).To(BeFalse())
				Expect(err).ToNot(BeNil())
				Expect(err).To(BeEquivalentTo(fmt.Errorf("user's OrgId does not match target's expected Google HD")))
				// This expect is to check that BeEquivalentTo is granular enough to distinguish between error messages
				Expect(err).ToNot(BeEquivalentTo(fmt.Errorf("other error message")))
			})
		})

		When("Supplied with a service account MRZAP Zcer with an invalid pubkey", func() {
			wrongKeypair, err := newRSAKeypair()
			Expect(err).To(BeNil())

			zCer, err := CreateServiceAccountZcer(wrongKeypair, jwksUrl, serviceAccountEmail, idpOrgId, exp, mrzapValues)
			Expect(err).To(BeNil())
			verifier := BZCertVerifier{
				orgId:               idpOrgId,
				orgProvider:         ProviderType(idpProvider),
				issUrl:              "",
				cert:                zCer,
				allowedJwksUrlRoots: map[string]bool{},
			}
			verifier.AddServiceAccountJwksRootUrl(jwksRootUrl)

			It("performs Zcer vertification check and should fail", func() {
				By("calling AttemptJwksVerification directly")
				verified, err := verifier.VerifySericeAccountIdToken(zCer.InitialIdToken)
				Expect(verified).To(BeFalse())
				Expect(err).ToNot(BeNil())
				Expect(err).To(BeEquivalentTo(fmt.Errorf("ID Token verification error: failed to verify signature: failed to verify id token signature")))
			})
		})

		When("Supplied with a service account jku jwksUrl that doesn't match the jwks root", func() {
			wrongJwksUrl := s.URL + "/" + "evilProject" + "/" + serviceAccountEmail

			zCer, err := CreateServiceAccountZcer(keyPair, wrongJwksUrl, serviceAccountEmail, idpOrgId, exp, mrzapValues)
			Expect(err).To(BeNil())
			verifier := BZCertVerifier{
				orgId:               idpOrgId,
				orgProvider:         ProviderType(idpProvider),
				issUrl:              "",
				cert:                zCer,
				allowedJwksUrlRoots: map[string]bool{},
			}
			verifier.AddServiceAccountJwksRootUrl(jwksRootUrl)

			It("performs Zcer vertification check and should fail", func() {
				By("calling AttemptJwksVerification directly")
				verified, err := verifier.VerifySericeAccountIdToken(zCer.InitialIdToken)
				Expect(verified).To(BeFalse())
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("jku value in ID Token is incorrect"))
			})
		})

		When("Supplied with a service account Zcer with a invalid nonce", func() {
			wrongJwksUrl := s.URL + "/" + "evilProject" + "/" + serviceAccountEmail

			zCer, err := CreateServiceAccountZcer(keyPair, wrongJwksUrl, serviceAccountEmail, idpOrgId, exp, mrzapValues)
			Expect(err).To(BeNil())
			verifier := BZCertVerifier{
				orgId:               idpOrgId,
				orgProvider:         ProviderType(idpProvider),
				issUrl:              "",
				cert:                zCer,
				allowedJwksUrlRoots: map[string]bool{},
			}
			verifier.AddServiceAccountJwksRootUrl(jwksRootUrl)

			It("performs Zcer vertification check and should fail", func() {
				By("calling AttemptJwksVerification directly")
				verified, err := verifier.VerifySericeAccountIdToken(zCer.InitialIdToken)
				Expect(verified).To(BeFalse())
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("jku value in ID Token is incorrect"))
			})
		})

		When("Supplied with a service account Zcer that has expired nonce", func() {

			// Set the idTokens expiry for 1 hour in the past
			expiredExpiry := int64(time.Now().Add(-time.Hour).Unix())

			// exp := int64(time.Now().Add(time.Hour).Unix())

			zCer, err := CreateServiceAccountZcer(keyPair, jwksUrl, serviceAccountEmail, idpOrgId, expiredExpiry, mrzapValues)
			Expect(err).To(BeNil())
			verifier := BZCertVerifier{
				orgId:               idpOrgId,
				orgProvider:         ProviderType(idpProvider),
				issUrl:              "",
				cert:                zCer,
				allowedJwksUrlRoots: map[string]bool{},
			}
			verifier.AddServiceAccountJwksRootUrl(jwksRootUrl)

			It("performs Zcer vertification check and should fail", func() {
				By("calling AttemptJwksVerification directly")
				verified, err := verifier.VerifySericeAccountIdToken(zCer.InitialIdToken)
				Expect(verified).To(BeFalse())
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("token is expired"))
			})
		})
	})

})

var _ = Describe("NewBZCertVerifier", Ordered, func() {

	Context("Verify service accounts Zcer", func() {
		urlprefix := "abcdef"
		emailplaceholder := "*@example.com" //TODO: document this format in design doc

		serviceAccountEmail := "aliceserviceaccount@example.com"
		idpOrgId := "exampleCo"
		idpProvider := "google"
		exp := int64(time.Now().Add(time.Hour).Unix())

		mrzapValues := MrzapParams{
			pubkeyMrzap:    "aOx9mfXvmQeaJmpIK1KxH/ghsciTa42O3IDcuNfZMtk=",
			nonceMrzap:     "/RZbHd5AdEHX7LwMpMwu32iv80Lppeu0tL9ZBwcwBFg=",
			randMrzap:      "DJz3yRJmTSTLoDj4SE7KcKBzR4O8KwkYYngNNoS0bW0=",
			sigOnRandMrzap: "HlZxmpGN5mS0RGBnZfJ/1VdeF2MSXQS2F8fTh1fPsgXCEvV1spLBo+lJQQYjt4dbULTIFfFgvcYeIxM/QfxDAQ==",
		}

		keyPair, err := newRSAKeypair()
		Expect(err).To(BeNil())

		s := newJwksMockServer(keyPair)

		jwksRootUrl := s.URL + "/" + urlprefix + "/" + emailplaceholder
		jwksUrl := s.URL + "/" + urlprefix + "/" + serviceAccountEmail

		When("Supplied with a valid service account MRZAP Zcer", func() {
			zCer, err := CreateServiceAccountZcer(keyPair, jwksUrl, serviceAccountEmail, idpOrgId, exp, mrzapValues)
			Expect(err).To(BeNil())

			It("verifies that the Zcer is correct and valid", func() {
				By("initializing bzcert verifier")
				verifier, err := NewBZCertVerifier(zCer, idpProvider, idpOrgId)
				Expect(err).To(BeNil())

				verifier.AddServiceAccountJwksRootUrl(jwksRootUrl)

				skipExp := true
				retTime, err := verifier.VerifyIdToken(zCer.InitialIdToken, skipExp, false)
				Expect(err).To(BeNil())
				Expect(retTime).ToNot(BeNil())
			})
		})

		When("Supplied with a invalid service account MRZAP Zcer with wrong orgId", func() {
			wrongIdpOrgId := "eveCorp"

			zCer, err := CreateServiceAccountZcer(keyPair, jwksUrl, serviceAccountEmail, wrongIdpOrgId, exp, mrzapValues)
			Expect(err).To(BeNil())

			It("verifies that the Zcer is incorrect and fails", func() {
				By("initializing bzcert verifier")
				verifier, err := NewBZCertVerifier(zCer, idpProvider, idpOrgId)
				Expect(err).To(BeNil())
				verifier.AddServiceAccountJwksRootUrl(jwksRootUrl)

				By("calling verifyIdToken")
				retTime, err := verifier.VerifyIdToken(zCer.InitialIdToken, false, false)
				Expect(err).ToNot(BeNil())
				Expect(retTime).To(BeEquivalentTo(time.Time{}))

			})
		})
	})

})

// Service account header
type ServiceAccountJWTHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	Kid string `json:"kid"`
	Jku string `json:"jku"`
}

type JWTPayload struct {
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

func ser(v interface{}) ([]byte, error) {
	if jsonV, err := json.Marshal(v); err == nil {
		GinkgoWriter.Println(jsonV)
		return jsonV, nil
	} else {
		return nil, err
	}
}

type MrzapParams struct {
	pubkeyMrzap    string
	nonceMrzap     string
	randMrzap      string
	sigOnRandMrzap string
}

func CreateServiceAccountZcer(keyPair *JoseKeypair, jwksUrl string, email string, orgHD string, exp int64, mrzapValues MrzapParams) (*BZCert, error) {
	jwtPay := JWTPayload{
		Aud:           "110341072297225095816",
		Azp:           "110341072297225095816",
		Email:         email,
		EmailVerified: true,
		Exp:           exp,
		Hd:            orgHD,
		Iss:           email,
		Nonce:         mrzapValues.nonceMrzap,
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

	zcer := BZCert{
		InitialIdToken:  idT,
		CurrentIdToken:  idT,
		ClientPublicKey: mrzapValues.pubkeyMrzap,
		Rand:            mrzapValues.randMrzap,
		SignatureOnRand: mrzapValues.sigOnRandMrzap,
	}
	return &zcer, nil
}

// Inspired by the testing patterns used in oidc jwks test found at: https://github.com/coreos/go-oidc/blob/26c50372bf144421f269c09774d0f1324bee30e4/oidc/jwks_test.go#L59
type JoseKeypair struct {
	keyID string // optional
	priv  interface{}
	pub   interface{}
	alg   jose.SignatureAlgorithm
}

func newRSAKeypair() (*JoseKeypair, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 1028)

	if err != nil {
		return nil, err
	}
	return &JoseKeypair{"", priv, priv.Public(), jose.RS256}, nil
}

func newJwksMockServer(keyPair *JoseKeypair) *httptest.Server {
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
