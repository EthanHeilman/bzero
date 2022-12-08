package util

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/crypto/sha3"
)

func HashPayload(payload interface{}) ([]byte, bool) {
	var payloadMap map[string]interface{}
	rawpayload, err := SafeMarshal(payload)
	if err != nil {
		return []byte{}, false
	}

	json.Unmarshal(rawpayload, &payloadMap)
	lexicon, _ := SafeMarshal(payloadMap) // Make the marshalled json, alphabetical to match client

	// This is because javascript translates CTRL + L as \f and golang translates it as \u000c.
	// Gotta hash matching values to get matching signatures.
	safeLexicon := strings.Replace(string(lexicon), "\\u000c", "\\f", -1)

	hash := sha3.Sum256([]byte(safeLexicon))
	return hash[:], true // This returns type [32]byte but we want a slice so we [:]
}

func SafeMarshal(t interface{}) ([]byte, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(t)
	// Encode adds a newline character to the end that we dont want
	// See https://golang.org/pkg/encoding/json/#Encoder.Encode
	return buffer.Bytes()[:buffer.Len()-1], err
}

func Nonce() string {
	b := make([]byte, 32) // 32-length byte array, to make it same length as hash pointer
	rand.Read(b)          // populate with random bytes
	return base64.StdEncoding.EncodeToString(b)
}

// Parses the provided jku and returns the pattern and the email for this service account 
func ExtractJwksUrlPattern(jku string) (string, string, error) {
	if len(strings.Split(jku, "@")) != 2 {
		return "", "", fmt.Errorf("jku value in ID Token does not contain exactly one @. Supplied jku value %s", jku)
	}

	tokenizedJku := strings.Split(jku, "/")
	if len(tokenizedJku) <= 1 {
		return "", "", fmt.Errorf("jku value in ID Token is not a valid jku. Supplied jku value %s", jku)
	}

	jwksEmail := tokenizedJku[len(tokenizedJku)-1]
	if len(strings.Split(jwksEmail, "@")) != 2 {
		return "", "", fmt.Errorf("jku value in ID Token does not contain exactly one @ in email address. Supplied jku value %s", jku)
	}

	emailDomain := strings.Split(jwksEmail, "@")[1]
	jwksURLPattern := fmt.Sprintf("%s/*%s", strings.Join(tokenizedJku[0:len(tokenizedJku)-1], "/"), emailDomain)
	return jwksURLPattern, jwksEmail, nil
}
