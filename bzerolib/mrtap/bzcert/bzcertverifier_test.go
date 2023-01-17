package bzcert

import (
	"context"
	"fmt"
	"testing"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBzcertVerifier(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bzcert Verifier Suite")
}

var _ = Describe("NewBZCertVerifier.AttemptJwksVerification", Ordered, func() {

	Context("AttemptJwksVerification verification logic and errors", func() {

		mrtapValues := mrtapParams{
			pubkeyMrtap:    "aOx9mfXvmQeaJmpIK1KxH/ghsciTa42O3IDcuNfZMtk=",
			nonceMrtap:     "/RZbHd5AdEHX7LwMpMwu32iv80Lppeu0tL9ZBwcwBFg=",
			randMrtap:      "DJz3yRJmTSTLoDj4SE7KcKBzR4O8KwkYYngNNoS0bW0=",
			sigOnRandMrtap: "HlZxmpGN5mS0RGBnZfJ/1VdeF2MSXQS2F8fTh1fPsgXCEvV1spLBo+lJQQYjt4dbULTIFfFgvcYeIxM/QfxDAQ==",
		}

		urlprefix := "abcdef"
		emailplaceholder := "example.com" //TODO: document this format in design doc
		serviceAccountEmail := "aliceserviceaccount@example.com"
		idpOrgId := "exampleCo"
		idpProvider := "https://accounts.google.com"
		exp := time.Now().Add(time.Hour).Unix()

		keyPair, err := newMockRSAKeypair()
		Expect(err).To(BeNil())

		s := newJwksMockServer(keyPair)

		jwksUrlPattern := s.URL + "/" + urlprefix + "/*" + emailplaceholder
		jwksUrl := s.URL + "/" + urlprefix + "/" + serviceAccountEmail

		When("Supplied with a valid service account MRZAP Bzcert", func() {
			bzCert, err := createMockServiceAccountBzcert(keyPair, jwksUrl, serviceAccountEmail, idpOrgId, exp, mrtapValues)
			Expect(err).To(BeNil())

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(20*time.Second))
			defer cancel()

			// TODO: Mock out the idpProvider URL so it doesn't depend on an external webpage. Also would speed up test.
			provider, err := oidc.NewProvider(ctx, idpProvider)
			Expect(err).To(BeNil())

			otherJwksUrlPattern := "https://example.com/abcd/project2/*ab.org"
			jwksUrlPatterns := []string{otherJwksUrlPattern, jwksUrlPattern}
			allowedJwksUrlPatterns := make(map[string]bool)
			for i := range jwksUrlPatterns {
				allowedJwksUrlPatterns[jwksUrlPatterns[i]] = true
			}

			verifier := BZCertVerifier{
				orgId:                  idpOrgId,
				ssoProvider:            provider,
				allowedJwksUrlPatterns: allowedJwksUrlPatterns,
			}

			It("performs Bzcert verification check and should succeed", func() {
				By("calling AttemptJwksVerification directly")
				_, err := verifier.VerifyServiceAccountIdToken(bzCert)
				Expect(err).To(BeNil())
			})
		})

		// TODO: Add this back when we start checking again orgId
		// When("Supplied with a service account MRTAP Bzcert with an invalid orgId", func() {
		// 	wrongIdpOrgId := "eveCorp"
		// 	Expect(idpOrgId).ToNot(BeEquivalentTo(wrongIdpOrgId))

		// 	zCer, err := createMockServiceAccountBzcert(keyPair, jwksUrl, serviceAccountEmail, wrongIdpOrgId, exp, mrtapValues)
		// 	Expect(err).To(BeNil())

		// 	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(20*time.Second))
		// 	defer cancel()

		// 	provider, err := oidc.NewProvider(ctx, idpProvider)
		// 	Expect(err).To(BeNil())

		// 	jwksUrlPatterns := []string{jwksUrlPattern}
		// 	allowedJwksUrlPatterns := make(map[string]bool)
		// 	for i := range jwksUrlPatterns {
		// 		allowedJwksUrlPatterns[jwksUrlPatterns[i]] = true
		// 	}

		// 	verifier := BZCertVerifier{
		// 		orgId:                  idpOrgId,
		// 		ssoProvider:            provider,
		// 		allowedJwksUrlPatterns: allowedJwksUrlPatterns,
		// 	}

		// 	It("performs Bzcert verification check and should fail", func() {
		// 		By("calling AttemptJwksVerification directly")
		// 		_, err := verifier.VerifyServiceAccountIdToken(zCer)
		// 		Expect(err).ToNot(BeNil())
		// 	})
		// })

		When("Supplied with a service account MRZAP Bzcert with an invalid pubkey", func() {
			var bzCert *BZCert
			var err error
			var wrongKeypair *joseKeypair
			var verifier BZCertVerifier

			BeforeEach(func() {
				wrongKeypair, err = newMockRSAKeypair()
				Expect(err).To(BeNil())

				bzCert, err = createMockServiceAccountBzcert(wrongKeypair, jwksUrl, serviceAccountEmail, idpOrgId, exp, mrtapValues)
				Expect(err).To(BeNil())

				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(20*time.Second))
				defer cancel()

				provider, err := oidc.NewProvider(ctx, idpProvider)
				Expect(err).To(BeNil())

				jwksUrlPatterns := []string{jwksUrlPattern}
				allowedJwksUrlPatterns := make(map[string]bool)
				for i := range jwksUrlPatterns {
					allowedJwksUrlPatterns[jwksUrlPatterns[i]] = true
				}

				verifier = BZCertVerifier{
					orgId:                  idpOrgId,
					ssoProvider:            provider,
					allowedJwksUrlPatterns: allowedJwksUrlPatterns,
				}
			})

			It("performs Bzcert verification check and should fail", func() {
				By("calling AttemptJwksVerification directly")
				_, err := verifier.VerifyServiceAccountIdToken(bzCert)
				Expect(err).ToNot(BeNil())
				Expect(err).To(BeEquivalentTo(fmt.Errorf("ID Token verification error: failed to verify signature: failed to verify id token signature")))
			})
		})

		When("Supplied with a service account jku jwksUrl that doesn't match the jwks pattern", func() {
			wrongJwksUrl := s.URL + "/" + "evilProject" + "/" + serviceAccountEmail

			zCer, err := createMockServiceAccountBzcert(keyPair, wrongJwksUrl, serviceAccountEmail, idpOrgId, exp, mrtapValues)
			Expect(err).To(BeNil())

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(20*time.Second))
			defer cancel()

			provider, err := oidc.NewProvider(ctx, idpProvider)
			Expect(err).To(BeNil())

			jwksUrlPatterns := []string{jwksUrlPattern}
			allowedJwksUrlPatterns := make(map[string]bool)
			for i := range jwksUrlPatterns {
				allowedJwksUrlPatterns[jwksUrlPatterns[i]] = true
			}

			verifier := BZCertVerifier{
				orgId:                  idpOrgId,
				ssoProvider:            provider,
				allowedJwksUrlPatterns: allowedJwksUrlPatterns,
			}

			It("performs Bzcert verification check and should fail", func() {
				By("calling AttemptJwksVerification directly")
				_, err := verifier.VerifyServiceAccountIdToken(zCer)
				Expect(err).ToNot(BeNil())
			})
		})

		When("Supplied with a service account Bzcert with a invalid nonce", func() {
			wrongJwksUrl := s.URL + "/" + "evilProject" + "/" + serviceAccountEmail

			zCer, err := createMockServiceAccountBzcert(keyPair, wrongJwksUrl, serviceAccountEmail, idpOrgId, exp, mrtapValues)
			Expect(err).To(BeNil())

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(20*time.Second))
			defer cancel()

			provider, err := oidc.NewProvider(ctx, idpProvider)
			Expect(err).To(BeNil())

			jwksUrlPatterns := []string{jwksUrlPattern}
			allowedJwksUrlPatterns := make(map[string]bool)
			for i := range jwksUrlPatterns {
				allowedJwksUrlPatterns[jwksUrlPatterns[i]] = true
			}

			verifier := BZCertVerifier{
				orgId:                  idpOrgId,
				ssoProvider:            provider,
				allowedJwksUrlPatterns: allowedJwksUrlPatterns,
			}

			It("performs Bzcert verification check and should fail", func() {
				By("calling AttemptJwksVerification directly")
				_, err := verifier.VerifyServiceAccountIdToken(zCer)
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("jku value in ID Token is incorrect"))
			})
		})

		When("Supplied with a service account Bzcert that has expired nonce", func() {

			// Set the idTokens expiry for 1 hour in the past
			expiredExpiry := time.Now().Add(-time.Hour).Unix()

			zCer, err := createMockServiceAccountBzcert(keyPair, jwksUrl, serviceAccountEmail, idpOrgId, expiredExpiry, mrtapValues)
			Expect(err).To(BeNil())

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(20*time.Second))
			defer cancel()

			provider, err := oidc.NewProvider(ctx, idpProvider)
			Expect(err).To(BeNil())

			jwksUrlPatterns := []string{jwksUrlPattern}
			allowedJwksUrlPatterns := make(map[string]bool)
			for i := range jwksUrlPatterns {
				allowedJwksUrlPatterns[jwksUrlPatterns[i]] = true
			}

			verifier := BZCertVerifier{
				orgId:                  idpOrgId,
				ssoProvider:            provider,
				allowedJwksUrlPatterns: allowedJwksUrlPatterns,
			}

			It("performs Bzcert verification check and should fail", func() {
				By("calling AttemptJwksVerification directly")
				_, err := verifier.VerifyServiceAccountIdToken(zCer)
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("token is expired"))
			})
		})
	})

})

var _ = Describe("NewBZCertVerifier", Ordered, func() {
	Context("Verify service accounts Bzcert", func() {
		urlprefix := "abcdef"
		emailplaceholder := "example.com"

		serviceAccountEmail := "aliceserviceaccount@example.com"
		idpOrgId := "exampleCo"
		idpProvider := "google"
		exp := time.Now().Add(time.Hour).Unix()

		mrtapValues := mrtapParams{
			pubkeyMrtap:    "aOx9mfXvmQeaJmpIK1KxH/ghsciTa42O3IDcuNfZMtk=",
			nonceMrtap:     "/RZbHd5AdEHX7LwMpMwu32iv80Lppeu0tL9ZBwcwBFg=",
			randMrtap:      "DJz3yRJmTSTLoDj4SE7KcKBzR4O8KwkYYngNNoS0bW0=",
			sigOnRandMrtap: "HlZxmpGN5mS0RGBnZfJ/1VdeF2MSXQS2F8fTh1fPsgXCEvV1spLBo+lJQQYjt4dbULTIFfFgvcYeIxM/QfxDAQ==",
		}

		keyPair, err := newMockRSAKeypair()
		Expect(err).To(BeNil())

		s := newJwksMockServer(keyPair)

		jwksUrlPattern := s.URL + "/" + urlprefix + "/*" + emailplaceholder
		jwksUrl := s.URL + "/" + urlprefix + "/" + serviceAccountEmail

		When("Supplied with a valid service account MRZAP Bzcert", func() {
			bzCert, err := createMockServiceAccountBzcert(keyPair, jwksUrl, serviceAccountEmail, idpOrgId, exp, mrtapValues)
			Expect(err).To(BeNil())

			It("verifies that the Bzcert is correct and valid", func() {
				By("initializing bzcert verifier")
				jwksUrlPatterns := []string{jwksUrlPattern}
				verifier, err := NewVerifier(idpProvider, idpOrgId, jwksUrlPatterns)
				Expect(err).To(BeNil())

				retTime, err := verifier.Verify(bzCert)
				Expect(err).To(BeNil())
				Expect(retTime).ToNot(BeNil())
			})
		})

		// TODO: Add this back when we start checking again orgId
		// When("Supplied with a invalid service account MRTAP Bzcert with wrong orgId", func() {
		// 	wrongIdpOrgId := "eveCorp"

		// 	bzCert, err := createMockServiceAccountBzcert(keyPair, jwksUrl, serviceAccountEmail, wrongIdpOrgId, exp, mrtapValues)
		// 	Expect(err).To(BeNil())

		// 	It("verifies that the Bzcert is incorrect and fails", func() {
		// 		By("initializing bzcert verifier")
		// 		jwksUrlPatterns := []string{jwksUrlPattern}
		// 		verifier, err := NewVerifier(idpProvider, idpOrgId, jwksUrlPatterns)
		// 		Expect(err).To(BeNil())

		// 		By("calling verifyIdToken")
		// 		_, err = verifier.Verify(bzCert)
		// 		Expect(err).ToNot(BeNil())
		// 	})
		// })
	})

})
