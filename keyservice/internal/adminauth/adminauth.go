// Package adminauth provides HMAC-signed JWT-like admin tokens with scoped
// permissions and expiry. Tokens are compact, stateless, and verifiable with a
// shared secret — no external IdP dependency.
//
// Token format: base64url(header_json) + "." + base64url(payload_json) + "." + base64url(hmac_sha256)
package adminauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Scope represents a permission scope for admin operations.
type Scope string

const (
	ScopeTenantsWrite Scope = "tenants:write"
	ScopeKeysWrite    Scope = "keys:write"
	ScopeKeysRevoke   Scope = "keys:revoke"
	ScopeUsageRead    Scope = "usage:read"
	ScopeAll          Scope = "admin:all" // superuser — grants all scopes
)

var (
	ErrTokenExpired   = errors.New("token expired")
	ErrTokenMalformed = errors.New("token malformed")
	ErrTokenInvalid   = errors.New("token signature invalid")
	ErrScopeDenied    = errors.New("scope not granted")
)

// Claims is the payload of an admin token.
type Claims struct {
	Subject   string   `json:"sub"`            // who minted the token (e.g., "admin-cli")
	Scopes    []Scope  `json:"scopes"`         // granted permissions
	IssuedAt  int64    `json:"iat"`            // unix seconds
	ExpiresAt int64    `json:"exp"`            // unix seconds
}

// header is fixed — we only support HS256.
var headerB64 = base64Encode([]byte(`{"alg":"HS256","typ":"JWT"}`))

// Mint creates a signed admin token with the given scopes and lifetime.
func Mint(secret string, subject string, scopes []Scope, lifetime time.Duration) string {
	now := time.Now().UTC()
	claims := Claims{
		Subject:   subject,
		Scopes:    scopes,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(lifetime).Unix(),
	}
	payloadJSON, _ := json.Marshal(claims)
	payloadB64 := base64Encode(payloadJSON)

	signingInput := headerB64 + "." + payloadB64
	sig := sign(secret, signingInput)

	return signingInput + "." + base64Encode(sig)
}

// Verify checks the token signature, expiry, and that the required scope is
// present. Returns the claims on success.
func Verify(secret, token string, requiredScope Scope) (Claims, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return Claims{}, ErrTokenMalformed
	}

	signingInput := parts[0] + "." + parts[1]
	sigBytes, err := base64Decode(parts[2])
	if err != nil {
		return Claims{}, ErrTokenMalformed
	}

	expected := sign(secret, signingInput)
	if !hmac.Equal(sigBytes, expected) {
		return Claims{}, ErrTokenInvalid
	}

	payloadJSON, err := base64Decode(parts[1])
	if err != nil {
		return Claims{}, ErrTokenMalformed
	}

	var claims Claims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return Claims{}, ErrTokenMalformed
	}

	if time.Now().UTC().Unix() > claims.ExpiresAt {
		return Claims{}, ErrTokenExpired
	}

	if !hasScope(claims.Scopes, requiredScope) {
		return claims, fmt.Errorf("%w: need %s", ErrScopeDenied, requiredScope)
	}

	return claims, nil
}

func hasScope(scopes []Scope, required Scope) bool {
	for _, s := range scopes {
		if s == ScopeAll || s == required {
			return true
		}
	}
	return false
}

func sign(secret, input string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(input))
	return mac.Sum(nil)
}

func base64Encode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func base64Decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
