package bzcert

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	"bastionzero.com/bzerolib/keypair"
	"bastionzero.com/bzerolib/mrtap/util"
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

	initialIdTokenLifetime = time.Hour * 24 * 365 * 5 // 5 years
)

type IBZCertVerifier interface {
	Verify(bzcert *BZCert) (exp time.Time, err error)
}

type BZCertVerifier struct {
	orgId                  string
	ssoProvider            *oidc.Provider
	providerType           ProviderType
	allowedJwksUrlPatterns map[string]bool // The allow list of JWKS URL patterns configured for the agent. This is used for service accounts.
}

// the user claims we care about checking
type idTokenClaims struct {
	HD         string `json:"hd"`    // Google Org ID
	Nonce      string `json:"nonce"` // BastionZero-issued structured nonce
	TID        string `json:"tid"`   // Microsoft Tenant ID
	IssuedAt   int64  `json:"iat"`   // Unix datetime of issuance
	Expiration int64  `json:"exp"`   // Unix datetime of token expiry
}

// the service account claims we care about checking
type saIdTokenClaims struct {
	OrganizationId string `json:"org_id"` // BastionZero Organization ID
	Nonce          string `json:"nonce"`  // OIDC Nonce that commits to MRTAP values
	Expiration     int64  `json:"exp"`    // Unix datetime of token expiry
	IssuedAt       int64  `json:"iat"`    // Unix datetime of issuance
}

type ProviderType string

const (
	Google    ProviderType = "google"
	Microsoft ProviderType = "microsoft"
	Okta      ProviderType = "okta"
	OneLogin  ProviderType = "onelogin"
	// Custom    ProviderType = "custom" // plan for custom IdP support
)

func NewVerifier(idpProvider string, idpOrgId string, jwksUrlPatterns []string) (*BZCertVerifier, error) {
	// customIss := os.Getenv("CUSTOM_IDP")

	var issuerUrl string
	switch ProviderType(idpProvider) {
	case Google:
		issuerUrl = googleUrl
	case Microsoft:
		issuerUrl = getMicrosoftIssUrl(idpOrgId)
	case Okta:
		issuerUrl = fmt.Sprintf("https://%s.okta.com", idpOrgId)
	case OneLogin:
		issuerUrl = fmt.Sprintf("https://%s.onelogin.com/oidc/2", idpOrgId)
	// case Custom:
	// 	issUrl = customIss
	default:
		return nil, fmt.Errorf("unrecognized SSO provider: %s", idpProvider)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(60*time.Second))
	defer cancel()

	provider, err := oidc.NewProvider(ctx, issuerUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to establish connection with SSO provider %s: %w", idpProvider, err)
	}

	allowedJwksUrlPatterns := make(map[string]bool)
	for i := range jwksUrlPatterns {
		allowedJwksUrlPatterns[jwksUrlPatterns[i]] = true
	}

	return &BZCertVerifier{
		orgId:                  idpOrgId,
		ssoProvider:            provider,
		providerType:           ProviderType(idpProvider),
		allowedJwksUrlPatterns: allowedJwksUrlPatterns,
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

	return fmt.Sprintf("%s/%s/v2.0", microsoftUrl, tenantId)
}

func (v *BZCertVerifier) Verify(bzcert *BZCert) (exp time.Time, err error) {
	idToken := bzcert.CurrentIdToken

	// Check if JWKS Service Account
	// Service accounts cannot refresh a token thus their initial and current token are always the same
	if isServiceAccountToken(idToken) {
		if exp, err := v.VerifyServiceAccountIdToken(bzcert); err != nil {
			return time.Time{}, err
		} else {
			return exp, nil
		}
	}
	if err = v.verifyInitialIdToken(bzcert.InitialIdToken, bzcert); err != nil {
		return exp, &InitialIdTokenError{InnerError: err}
	} else if exp, err = v.verifyCurrentIdToken(bzcert.CurrentIdToken); err != nil {
		return exp, &CurrentIdTokenError{InnerError: err}
	} else {
		return
	}
}

// this function verifies the current id token and will return that token's
// expiration time
func (v *BZCertVerifier) verifyCurrentIdToken(token string) (time.Time, error) {
	config := &oidc.Config{
		SkipClientIDCheck: true,
		SkipExpiryCheck:   false,
	}

	if claims, err := v.getTokenClaims(token, config); err != nil {
		return time.Time{}, err
	} else {
		return time.Unix(claims.Expiration, 0), nil
	}
}

// this function verifies the initial id token which requires checking whether
// the structured nonce is correctly formatted
func (v *BZCertVerifier) verifyInitialIdToken(token string, bzcert *BZCert) error {
	config := &oidc.Config{
		SkipClientIDCheck: true,
		SkipExpiryCheck:   true,
	}

	claims, err := v.getTokenClaims(token, config)
	if err != nil {
		return err
	}

	now := time.Now()
	iat := time.Unix(claims.IssuedAt, 0) // Confirmed both Microsoft and Google use Unix

	// the initial id token expires after a time specified by BastionZero
	if now.After(iat.Add(initialIdTokenLifetime)) {
		return fmt.Errorf("InitialIdToken Expired {Current Time = %v, Token iat = %v}", now, iat)
	}

	// Check if the structured nonce in id token is formatted correctly
	if err = v.verifyAuthNonce(bzcert, claims.Nonce); err != nil {
		return err
	}

	return nil
}

func (v *BZCertVerifier) getTokenClaims(idtoken string, config *oidc.Config) (*idTokenClaims, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(60*time.Second))
	defer cancel()

	// This checks formatting and signature validity
	verifier := v.ssoProvider.Verifier(config)
	token, err := verifier.Verify(ctx, idtoken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify id token with SSO provider: %w", err)
	}

	// Extract claims from token
	var claims idTokenClaims
	if err = token.Claims(&claims); err != nil {
		return nil, fmt.Errorf("error parsing the ID Token: %s", err)
	}

	// Only validate org claim if there is an orgId associated with this agent.
	// This will be "None" for orgs associated with a personal gsuite/microsoft
	// account. We do not need to check against anything for Okta, because Okta
	// creates a specific issuer url for every org meaning that by virtue of
	// getting the claims, we are assured it's for the specific Okta tenant.
	// Okta assumption also applied for OneLogin tenants.
	if v.orgId != "None" {
		switch v.providerType {
		case Google:
			if v.orgId != claims.HD {
				return nil, fmt.Errorf("user's OrgId %s does not match target's expected Google HD %s", claims.HD, v.orgId)
			}
		case Microsoft:
			if v.orgId != claims.TID {
				return nil, fmt.Errorf("user's OrgId %s does not match target's expected Microsoft tid %s", claims.TID, v.orgId)
			}
		}
	}

	return &claims, nil
}

// This function takes in the BZECert, extracts all fields for verifying the AuthNonce (sent as part of
// the ID Token).  Returns nil if nonce is verified, else returns an error.
// Nonce should equal ClientPublicKey + SignatureOnRandomValue + RandomValue, where the signature is valid.
func (v *BZCertVerifier) verifyAuthNonce(bzcert *BZCert, authNonce string) error {
	nonce := bzcert.ClientPublicKey + bzcert.SignatureOnRand + bzcert.Rand
	hash := sha3.Sum256([]byte(nonce))
	nonceHash := base64.StdEncoding.EncodeToString(hash[:])

	// check nonce is equal to what is expected
	if authNonce != nonceHash {
		return fmt.Errorf("nonce in ID token does not match calculated nonce hash")
	}

	decodedRand, err := base64.StdEncoding.DecodeString(bzcert.Rand)
	if err != nil {
		return fmt.Errorf("BZCert Rand is not base64 encoded")
	}

	randHashBits := sha3.Sum256([]byte(decodedRand))

	pubkey, err := keypair.PublicKeyFromString(bzcert.ClientPublicKey)
	if err != nil {
		return err
	}

	if ok := pubkey.Verify(randHashBits[:], bzcert.SignatureOnRand); !ok {
		return fmt.Errorf("failed to verify signature")
	}
	return nil
}

// This method verifies and authenticates a supplied JWKS service account. It performs the following checks if any of the checks fail it returns false.
//  1. Ensure that the supplied jku header in the idToken matches one of the allowed JWKS URL patterns that have been configured for this agent.
//  2. Ensure that idToken signature verifies under the pubkey at JWKS URL supplied in the jku header in the idToken.
//  3. Ensure that idToken hasn't expired. Unlike standard idTokens service account idTokens cannot be refreshed. Thus, expiration is much simpler here. We reject an idToken that has expired according to its expiration value.
//  4. This check has been omitted for now. Ensure that Org in the idToken HD claim is correct. This check isn't strictly necessary since the token is signed by the service account allowing the service account to choose any value it wants for this field. It is beneficial to check this anyways to catch misconfigurations.
//  5. Ensure that the the MRTAP nonce verifies i.e., random value and signature committed to in nonce verifies under pubkey committed to in nonce
func (v *BZCertVerifier) VerifyServiceAccountIdToken(bzcert *BZCert) (time.Time, error) {
	idtoken := bzcert.CurrentIdToken

	ctx := context.TODO() // Gives us non-nil empty context
	config := &oidc.Config{
		SkipClientIDCheck: true,
	}

	jws, err := jose.ParseSigned(idtoken)
	if err != nil {
		return time.Time{}, err
	}

	if len(jws.Signatures) == 0 {
		return time.Time{}, fmt.Errorf("service account id token does not contain any signatures")
	}

	jku := jws.Signatures[0].Header.ExtraHeaders["jku"]
	if jkuStr, ok := jku.(string); ok && len(jkuStr) > 0 {
		suppliedJwksURLPattern, jwksEmail, err := util.ExtractJwksUrlPattern(jkuStr)
		if err != nil {
			return time.Time{}, err
		}
		if parsedJwksUrl, err := url.ParseRequestURI(suppliedJwksURLPattern); err != nil {
			return time.Time{}, fmt.Errorf("failed to parse as url supplied jwks url %s: %w", suppliedJwksURLPattern, err)
		} else {
			suppliedJwksURLPattern = parsedJwksUrl.String()
		}

		// 1. Ensure that supplied JWKS URL pattern exists in the configured allow list of JWKS URL patterns
		if !v.allowedJwksUrlPatterns[suppliedJwksURLPattern] {
			return time.Time{}, &ServiceAccountError{InnerError: fmt.Errorf("jku value in ID Token is incorrect. Supplied jku value %s, parsed pattern %s, Allowed set %v", jkuStr, suppliedJwksURLPattern, v.allowedJwksUrlPatterns)}
		}

		// 2. Ensure that the signature on the idToken verifies under the pubkeys at JWKS URL which was supplied in the jku header
		jwks := oidc.NewRemoteKeySet(ctx, jkuStr)
		verifier := oidc.NewVerifier(jwksEmail, jwks, config)

		// This checks formatting and signature validity and
		// 3. Ensures the idToken hasn't expired as SkipExpiryCheck is false
		token, err := verifier.Verify(ctx, idtoken)
		if err != nil {
			return time.Time{}, fmt.Errorf("ID Token verification error: %s", err)
		}

		var claims saIdTokenClaims
		if err := token.Claims(&claims); err != nil {
			return time.Time{}, fmt.Errorf("error parsing the ID Token: %s", err)
		}

		// 5. Ensure the MRTAP values in the nonce verify.
		if err = v.verifyAuthNonce(bzcert, claims.Nonce); err != nil {
			return time.Time{}, err
		}
		return time.Unix(claims.Expiration, 0), nil
	} else {
		return time.Time{}, fmt.Errorf("supplied jku %s was not of valid format or length", jkuStr)
	}
}

// Checks whether the provided idToken contains a jku claim which currently signals
// that this is a service account token
func isServiceAccountToken(idToken string) bool {
	jws, err := jose.ParseSigned(idToken)
	if err != nil {
		return false
	}
	if len(jws.Signatures) <= 0 {
		return false
	}
	jku := jws.Signatures[0].Header.ExtraHeaders["jku"]
	if jku == nil {
		return false
	}
	if jkuStr, ok := jku.(string); ok && len(jkuStr) > 0 {
		return true
	}
	return false
}
