// Package service — shopify_auth.go holds the pure (no-I/O) security primitives
// of the Shopify OAuth install flow and App Bridge session-token verification
// (F3). Keeping them here, dependency-free and table-tested, means the critical
// checks — shop-domain validation, request-HMAC verification, signed state, and
// JWT (HS256) verification — are unit-tested without Gin, a DB, or the network.
//
// Security notes:
//   - All MAC/HMAC comparisons are constant-time (crypto/hmac.Equal / subtle).
//   - JWT verification only accepts HS256; alg=none and any other alg are
//     rejected (algorithm-confusion guard).
//   - The shop domain is strictly validated as a single-label *.myshopify.com
//     host, defeating open-redirect / SSRF / forged-callback attempts.
//   - No secret value is ever logged here.
package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// shopDomainRe matches exactly one DNS label (letters, digits, hyphen; not
// starting/ending with a hyphen is not enforced by Shopify, but no dots are
// allowed) followed by the literal `.myshopify.com`. Anchored at both ends so
// `acme.myshopify.com.evil.com`, userinfo tricks, schemes, and paths all fail.
var shopDomainRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.myshopify\.com$`)

// ValidateShopDomain returns nil iff host is a well-formed <shop>.myshopify.com
// domain (case-insensitive). It is the single gate against open-redirect / SSRF
// from a merchant- or attacker-supplied `shop` parameter.
func ValidateShopDomain(host string) error {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return errors.New("empty shop domain")
	}
	if !shopDomainRe.MatchString(h) {
		return fmt.Errorf("invalid shop domain")
	}
	return nil
}

// NormalizeShopDomain lower-cases and trims a shop domain after validation.
func NormalizeShopDomain(host string) string {
	return strings.ToLower(strings.TrimSpace(host))
}

// ConstantTimeEqual compares two strings without leaking length-independent
// timing of the matching prefix (subtle.ConstantTimeCompare is itself
// length-leaking, which is acceptable for these non-secret nonces but we still
// avoid the early-return `==`).
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// VerifyQueryHMAC verifies the Shopify request HMAC on an OAuth callback (or any
// signed Admin redirect). Per Shopify's spec: remove `hmac` and `signature`,
// sort the remaining parameters lexicographically, join them as `k=v` with `&`,
// HMAC-SHA256 with the app secret, and hex-compare (constant-time) to the
// supplied `hmac`. Returns nil on a match.
func VerifyQueryHMAC(q url.Values, secret string) error {
	// Fail closed: an empty secret keys the HMAC on "", making any query
	// forgeable. Reject regardless of caller (F3-SEC-1 defense in depth).
	if secret == "" {
		return errors.New("refusing to verify: empty app secret")
	}
	provided := q.Get("hmac")
	if provided == "" {
		return errors.New("missing hmac")
	}

	keys := make([]string, 0, len(q))
	for k := range q {
		if k == "hmac" || k == "signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		// Use the first value per key (Shopify sends single-valued params).
		parts = append(parts, k+"="+q.Get(k))
	}
	message := strings.Join(parts, "&")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	expected := mac.Sum(nil)

	providedBytes, err := hexDecode(provided)
	if err != nil {
		return fmt.Errorf("malformed hmac: %w", err)
	}
	if !hmac.Equal(expected, providedBytes) {
		return errors.New("hmac mismatch")
	}
	return nil
}

// VerifyWebhookHMAC verifies a Shopify webhook signature. Unlike the OAuth query
// HMAC, Shopify signs the RAW request BODY bytes and sends the result
// base64-encoded in the X-Shopify-Hmac-Sha256 header. The handler MUST read the
// raw body before any JSON decode and pass the exact bytes here. Comparison is
// constant-time (hmac.Equal). Fails closed on an empty secret (a "" key makes the
// MAC forgeable — F3-SEC-1 defense in depth).
func VerifyWebhookHMAC(rawBody []byte, headerHMAC, secret string) error {
	if secret == "" {
		return errors.New("refusing to verify: empty app secret")
	}
	if headerHMAC == "" {
		return errors.New("missing webhook hmac header")
	}
	provided, err := base64.StdEncoding.DecodeString(headerHMAC)
	if err != nil {
		return fmt.Errorf("malformed webhook hmac: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	if !hmac.Equal(mac.Sum(nil), provided) {
		return errors.New("webhook hmac mismatch")
	}
	return nil
}

// hexDecode decodes a lowercase/uppercase hex string into bytes.
func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errors.New("odd-length hex")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi, err := hexVal(s[2*i])
		if err != nil {
			return nil, err
		}
		lo, err := hexVal(s[2*i+1])
		if err != nil {
			return nil, err
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

func hexVal(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	}
	return 0, fmt.Errorf("invalid hex char %q", c)
}

// --- state nonce (CSRF) ---

// SignState produces a tamper-evident token `<nonce>.<sig>` where sig is the
// base64url HMAC-SHA256 of the nonce keyed by secret. Stored in an HttpOnly
// cookie; the raw nonce is sent to Shopify as `state` and echoed back.
func SignState(secret, nonce string) string {
	sig := stateSig(secret, nonce)
	return nonce + "." + sig
}

// VerifyState validates a SignState token and returns the embedded nonce. The
// signature is recomputed and compared constant-time, defeating cookie tampering.
func VerifyState(secret, signed string) (string, error) {
	// Fail closed: an empty secret makes the state signature forgeable
	// (F3-SEC-1 defense in depth).
	if secret == "" {
		return "", errors.New("refusing to verify: empty app secret")
	}
	i := strings.LastIndexByte(signed, '.')
	if i <= 0 || i == len(signed)-1 {
		return "", errors.New("malformed state")
	}
	nonce, sig := signed[:i], signed[i+1:]
	expected := stateSig(secret, nonce)
	if !ConstantTimeEqual(sig, expected) {
		return "", errors.New("state signature mismatch")
	}
	return nonce, nil
}

func stateSig(secret, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(nonce))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// --- App Bridge session token (JWT HS256) ---

// SessionClaims is the subset of App Bridge session-token claims this app uses.
type SessionClaims struct {
	Iss  string `json:"iss"`
	Dest string `json:"dest"`
	Aud  string `json:"aud"`
	Sub  string `json:"sub"`
	Exp  int64  `json:"exp"`
	Nbf  int64  `json:"nbf"`
	Iat  int64  `json:"iat"`
}

// ShopDomain extracts the <shop>.myshopify.com host from the `dest` claim
// (falling back to `iss`). Returns "" if neither yields a valid shop host.
func (c SessionClaims) ShopDomain() string {
	for _, raw := range []string{c.Dest, c.Iss} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		host := u.Host
		if host == "" {
			// `dest` is sometimes just the host without a scheme.
			host = strings.TrimPrefix(raw, "//")
		}
		host = strings.ToLower(host)
		if ValidateShopDomain(host) == nil {
			return host
		}
	}
	return ""
}

// jwtLeeway tolerates small clock skew between Shopify and this server.
const jwtLeeway = 5 * time.Second

// VerifySessionToken verifies an App Bridge session token (HS256, signed with the
// app secret) and returns its claims. It enforces: HS256 only (alg-confusion
// guard), valid signature (constant-time), aud == apiKey, exp/nbf within leeway,
// and a resolvable *.myshopify.com shop from dest/iss. Any failure → error.
func VerifySessionToken(token, secret, apiKey string) (SessionClaims, error) {
	var zero SessionClaims

	// Fail closed: an empty secret means the HMAC is keyed on "", so any
	// attacker can forge a valid-looking HS256 token. This is the exact
	// exploit the F3 security review reproduced — reject it at the primitive,
	// not just at boot (defense in depth, F3-SEC-1).
	if secret == "" {
		return zero, errors.New("refusing to verify: empty app secret")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return zero, errors.New("token must have 3 segments")
	}
	headerB, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return zero, fmt.Errorf("decode header: %w", err)
	}
	var hdr struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerB, &hdr); err != nil {
		return zero, fmt.Errorf("parse header: %w", err)
	}
	if hdr.Alg != "HS256" {
		return zero, fmt.Errorf("unexpected alg %q (only HS256 accepted)", hdr.Alg)
	}

	// Verify signature over the raw `header.payload` before trusting the payload.
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return zero, fmt.Errorf("decode signature: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	if !hmac.Equal(mac.Sum(nil), sig) {
		return zero, errors.New("invalid signature")
	}

	payloadB, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return zero, fmt.Errorf("decode payload: %w", err)
	}
	var claims SessionClaims
	if err := json.Unmarshal(payloadB, &claims); err != nil {
		return zero, fmt.Errorf("parse claims: %w", err)
	}

	if claims.Aud != apiKey {
		return zero, errors.New("aud does not match app api key")
	}
	now := time.Now()
	if claims.Exp != 0 && now.After(time.Unix(claims.Exp, 0).Add(jwtLeeway)) {
		return zero, errors.New("token expired")
	}
	if claims.Nbf != 0 && now.Add(jwtLeeway).Before(time.Unix(claims.Nbf, 0)) {
		return zero, errors.New("token not yet valid (nbf)")
	}
	if claims.ShopDomain() == "" {
		return zero, errors.New("no valid shop in dest/iss")
	}
	return claims, nil
}
