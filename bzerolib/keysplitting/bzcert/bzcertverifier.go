package bzcert

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	ed "crypto/ed25519"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/crypto/sha3"
	"gopkg.in/square/go-jose.v2"
)

const (
	googleUrl    = "https://accounts.google.com"
	microsoftUrl = "https://login.microsoftonline.com"

	// this is the tenant id Microsoft uses when the account is a personal account (not a work/school account)
	// https://docs.microsoft.com/en-us/azure/active-directory/develop/id-tokens#payload-claims)
	microsoftPersonalAccountTenantId = "9188040d-6c67-4c5b-b112-36a304b66dad"
	bzCustomTokenLifetime            = time.Hour * 24 * 365 * 5 // 5 years
)

type IBZCertVerifier interface {
	VerifyIdToken(idtoken string, skipExpiry bool, verifyNonce bool) (time.Time, error)
	AddServiceAccountJwksRootUrl(jwksrootUrl string)
}

type BZCertVerifier struct {
	orgId               string
	orgProvider         ProviderType
	issUrl              string
	cert                *BZCert
	allowedJwksUrlRoots map[string]bool // The allow list of JWKS URL roots configured for the agent. This is used for service accounts.
}

type ProviderType string

const (
	Google    ProviderType = "google"
	Microsoft ProviderType = "microsoft"
	Okta      ProviderType = "okta"
	// Custom    ProviderType = "custom" // TODO: support custom IdPs
	None ProviderType = "None"
)

func NewBZCertVerifier(bzcert *BZCert, idpProvider string, idpOrgId string) (IBZCertVerifier, error) {
	// customIss := os.Getenv("CUSTOM_IDP")

	issUrl := ""
	switch ProviderType(idpProvider) {
	case Google:
		issUrl = googleUrl
	case Microsoft:
		issUrl = getMicrosoftIssUrl(idpOrgId)
	case Okta:
		issUrl = "https://" + idpOrgId + ".okta.com"
	// case Custom:
	// 	issUrl = customIss
	default:
		return nil, fmt.Errorf("unrecognized OIDC provider: %s", idpProvider)
	}

	return &BZCertVerifier{
		orgId:               idpOrgId,
		orgProvider:         ProviderType(idpProvider),
		issUrl:              issUrl,
		cert:                bzcert,
		allowedJwksUrlRoots: map[string]bool{},
	}, nil
}

func getMicrosoftIssUrl(orgId string) string {
	// Handles personal accounts by using microsoftPersonalAccountTenantId as the tenantId
	// see https://github.com/coreos/go-oidc/issues/121
	tenantId := ""
	if orgId == "None" {
		tenantId = microsoftPersonalAccountTenantId
	} else {
		tenantId = orgId
	}

	return microsoftUrl + "/" + tenantId + "/v2.0"
}

// This method verifies and authenicates a supplied JWKS service account. It performs the following checks if any of the checks fail it returns false.
//  1. Ensure that the supplied jku header in the idToken matches one of the allowed JWKS URL roots that have been configured for this agent.
//  2. Ensure that idToken signature verifies under the pubkey at JWKS URL supplied in the jku header in the idToken.
//  3. Ensure that Org in the idToken HD claim is correct. This check isn't strictly neccessary since the token is signed by the service account allowing the service account to choose any value it wants for this field. It is benefital to check this anyways to catch misconfigurations.
//  4. Ensure that idToken hasn't expired. Unlike standard idTokens service account idTokens can be "refreshed" and still contain the nonce. Thus, expiration is much simplier here. We reject an idToken that has expired according to its expiration value.
//  5. Ensure that the the MRZAP nonce verifies i.e., random value and signature committed to in nonce verifies under pubkey committed to in nonce
func (u *BZCertVerifier) VerifySericeAccountIdToken(idtoken string) (bool, error) {
	ctx := context.TODO() // Gives us non-nil empty context
	config := &oidc.Config{
		SkipClientIDCheck: true,
		SkipExpiryCheck:   false,
	}

	jws, err := jose.ParseSigned(idtoken)
	if err != nil {
		return false, err
	}

	jku := jws.Signatures[len(jws.Signatures)-1].Header.ExtraHeaders["jku"]
	if jkuStr, ok := jku.(string); ok {
		if len(jkuStr) > 0 {
			if len(strings.Split(jkuStr, "@")) != 2 {
				return false, fmt.Errorf("jku value in ID Token does not contain exactly one @. Supplied jku value %s", jkuStr)
			}

			tokJku := strings.Split(jkuStr, "/")
			jwksEmail := tokJku[len(tokJku)-1]

			if len(strings.Split(jwksEmail, "@")) != 2 {
				return false, fmt.Errorf("jku value in ID Token does not contain exactly one @ in email address. Supplied jku value %s", jkuStr)
			}

			emailDomain := strings.Split(jwksEmail, "@")[1]
			suppliedJwksURLPattern := strings.Join(tokJku[0:len(tokJku)-1], "/") + "/" + "*" + "@" + emailDomain

			// 1. Ensure that supplied JWKS URL root exists in the configured allow list of JWKS URL roots
			if !u.allowedJwksUrlRoots[suppliedJwksURLPattern] {
				rootsStr := ""
				for k := range u.allowedJwksUrlRoots {
					rootsStr += k
					rootsStr += ", "
				}

				// for i := range v.Data.ServiceAccountUrls {
				// 	urlsSet[v.Data.ServiceAccountUrls[i]] = true
				// }
				return false, fmt.Errorf("jku value in ID Token is incorrect. Supplied jku value %s, Allowed set %s", jkuStr, rootsStr)
			}

			// 2. Ensure that the signature on the idToken verifies under the pubkeys at JWKS URL which was supplied in the jku header
			jwks := oidc.NewRemoteKeySet(ctx, jkuStr)
			if err != nil {
				return false, err
			}
			verifier := oidc.NewVerifier(jwksEmail, jwks, config)

			// This checks formatting and signature validity and
			// 4. Ensures the idToken hasn't expired as SkipExpiryCheck is false
			token, err := verifier.Verify(ctx, idtoken)
			if err != nil {
				return false, fmt.Errorf("ID Token verification error: %s", err)
			}

			var claims struct {
				HD    string `json:"hd"`    // Google Org ID
				Nonce string `json:"nonce"` // OIDC Nonce that commits to MRZAP values
				Death int64  `json:"exp"`   // Unix datetime of token expiry

			}
			if err := token.Claims(&claims); err != nil {
				return false, fmt.Errorf("error parsing the ID Token: %s", err)
			}

			// 3. Ensure the idToken matches the expected org id
			if u.orgId != claims.HD {
				return false, fmt.Errorf("user's OrgId does not match target's expected Google HD")
			}

			// 4. Ensure the idToken hasn't expired
			// now := time.Now()
			// death := time.Unix(claims.Death, 0)
			// if now.After(death) {
			// 	return false, fmt.Errorf("JWKS Service Account IdToken Expired {Current Time = %v, Token death = %v}", now, death)
			// }

			// 5. Ensure the MRZAP values in the nonce verify.
			if err = u.verifyAuthNonce(claims.Nonce); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	return false, nil
}

// This function verifies id_tokens
func (u *BZCertVerifier) VerifyIdToken(idtoken string, skipExpiry bool, verifyNonce bool) (time.Time, error) {
	// If there is no issuer URL, skip id token verification
	// Provider isn't stored for single-player orgs
	if u.issUrl == "" {
		return time.Now().Add(bzCustomTokenLifetime), nil
	}

	ctx := context.TODO() // Gives us non-nil empty context
	config := &oidc.Config{
		SkipClientIDCheck: true,
		SkipExpiryCheck:   skipExpiry,
		// SupportedSigningAlgs: []string{RS256, ES512}, // This might be wildly insecure
	}

	// Check if JWKS Service Account (See Design Doc)
	if IsJWKSServiceAccount(idtoken) {
		// Frist check if this verifies as a JWKS URL based service account,
		//  if it verifies then short circuit and return verified, if verification
		//  fails then continue and attempt to verify IDToken as an SSO user.
		jwksVerified, err := u.VerifySericeAccountIdToken(idtoken)
		if jwksVerified {
			return time.Now().Add(bzCustomTokenLifetime), nil
		} else {
			return time.Time{}, fmt.Errorf("invalid JWKS Service Account ID Token: %s", err)
		}
	}
	fmt.Println("Not GCP service account")

	provider, err := oidc.NewProvider(ctx, u.issUrl)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid OIDC provider: %s", err)
	}

	// This checks formatting and signature validity
	verifier := provider.Verifier(config)
	token, err := verifier.Verify(ctx, idtoken)
	if err != nil {
		return time.Time{}, fmt.Errorf("ID Token verification error: %s", err)
	}

	// Verify Claims

	// the claims we care about checking
	var claims struct {
		HD       string `json:"hd"`    // Google Org ID
		Nonce    string `json:"nonce"` // BastionZero-issued nonce
		TID      string `json:"tid"`   // Microsoft Tenant ID
		IssuedAt int64  `json:"iat"`   // Unix datetime of issuance
		Death    int64  `json:"exp"`   // Unix datetime of token expiry
	}

	if err := token.Claims(&claims); err != nil {
		return time.Time{}, fmt.Errorf("error parsing the ID Token: %s", err)
	}

	// Manual check to see if InitialIdToken is expired
	if skipExpiry {
		now := time.Now()
		iat := time.Unix(claims.IssuedAt, 0) // Confirmed both Microsoft and Google use Unix
		if now.After(iat.Add(bzCustomTokenLifetime)) {
			return time.Time{}, fmt.Errorf("InitialIdToken Expired {Current Time = %v, Token iat = %v}", now, iat)
		}
	}

	// Check if Nonce in ID token is formatted correctly
	if verifyNonce {
		if err = u.verifyAuthNonce(claims.Nonce); err != nil {
			return time.Time{}, err
		}
	}

	// Only validate org claim if there is an orgId associated with this agent. This will be empty
	// for orgs associated with a personal gsuite/microsoft account. We do not need to check against
	// anything for Okta, because Okta creates a specific issuer url for every org meaning that by
	// virtue of getting the claims, we are assured it's for the specific Okta tenant.
	switch u.orgProvider {
	case Google:
		if u.orgId != claims.HD {
			return time.Time{}, fmt.Errorf("user's OrgId does not match target's expected Google HD")
		}
	case Microsoft:
		if u.orgId != claims.TID {
			return time.Time{}, fmt.Errorf("user's OrgId does not match target's expected Microsoft tid")
		}
	}

	return time.Unix(claims.Death, 0), nil
}

// This function takes in the BZECert, extracts all fields for verifying the AuthNonce (sent as part of
// the ID Token).  Returns nil if nonce is verified, else returns an error.
// Nonce should equal ClientPublicKey + SignatureOnRandomValue + RandomValue, where the signature is valid.
func (b *BZCertVerifier) verifyAuthNonce(authNonce string) error {
	nonce := b.cert.ClientPublicKey + b.cert.SignatureOnRand + b.cert.Rand
	hash := sha3.Sum256([]byte(nonce))
	nonceHash := base64.StdEncoding.EncodeToString(hash[:])

	// check nonce is equal to what is expected
	if authNonce != nonceHash {
		return fmt.Errorf("nonce in ID token does not match calculated nonce hash")
	}

	decodedRand, err := base64.StdEncoding.DecodeString(b.cert.Rand)
	if err != nil {
		return fmt.Errorf("BZCert Rand is not base64 encoded")
	}

	randHashBits := sha3.Sum256([]byte(decodedRand))
	sigBits, _ := base64.StdEncoding.DecodeString(b.cert.SignatureOnRand)

	pubKeyBits, _ := base64.StdEncoding.DecodeString(b.cert.ClientPublicKey)
	if len(pubKeyBits) != 32 {
		return fmt.Errorf("public key has invalid length %v", len(pubKeyBits))
	}
	pubkey := ed.PublicKey(pubKeyBits)

	if ok := ed.Verify(pubkey, randHashBits[:], sigBits); ok {
		return nil
	} else {
		return fmt.Errorf("failed to verify signature on rand")
	}
}

func (u *BZCertVerifier) AddServiceAccountJwksRootUrl(jwksUrlRoot string) {
	// Trailing / isn't meaningful and should be not be recorded
	jwksUrlRoot = strings.TrimRight(jwksUrlRoot, "/")
	u.allowedJwksUrlRoots[jwksUrlRoot] = true
}

// Add comment
func IsJWKSServiceAccount(idToken string) bool {
	jws, err := jose.ParseSigned(idToken)
	if err != nil {
		return false
	}
	jku := jws.Signatures[len(jws.Signatures)-1].Header.ExtraHeaders["jku"]
	if jku == nil {
		return false
	}
	if jku, ok := jku.(string); ok {
		if len(jku) > 0 {
			return true
		}
	}
	return false
}
