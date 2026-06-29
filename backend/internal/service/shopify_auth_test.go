package service_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gpsr/backend/internal/service"
)

// ---- shop-domain validation (open-redirect / forged-callback guard) ----

func TestValidateShopDomain(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"plain valid", "acme.myshopify.com", true},
		{"hyphen + digits", "acme-store-123.myshopify.com", true},
		{"uppercase normalised", "Acme.MyShopify.com", true},
		{"empty", "", false},
		{"missing suffix", "acme.example.com", false},
		{"open redirect attacker host", "evil.com", false},
		{"suffix as substring not suffix", "acme.myshopify.com.evil.com", false},
		{"embedded path traversal", "acme.myshopify.com/../evil", false},
		{"scheme prefix", "https://acme.myshopify.com", false},
		{"at-sign userinfo trick", "acme.myshopify.com@evil.com", false},
		{"sub-subdomain not allowed", "a.b.myshopify.com", false},
		{"leading dot", ".myshopify.com", false},
		{"just suffix", "myshopify.com", false},
		{"underscore invalid char", "acme_store.myshopify.com", false},
		{"space", "acme .myshopify.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := service.ValidateShopDomain(tc.in)
			if (got == nil) != tc.ok {
				t.Fatalf("ValidateShopDomain(%q): err=%v, wantOK=%v", tc.in, got, tc.ok)
			}
		})
	}
}

// ---- OAuth callback HMAC verification (Shopify spec) ----

// signQuery computes the Shopify HMAC the same way the server must verify it:
// drop hmac (and signature), sort the remaining params, join k=v with '&',
// HMAC-SHA256 with the secret, hex-encode. Used by the test to build a
// correctly-signed query.
func signQuery(secret string, params map[string]string) url.Values {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "hmac" || k == "signature" {
			continue
		}
		keys = append(keys, k)
	}
	// simple insertion sort to avoid importing sort in the test for clarity
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join(parts, "&")))
	digest := fmt.Sprintf("%x", mac.Sum(nil))

	v := url.Values{}
	for k, val := range params {
		v.Set(k, val)
	}
	v.Set("hmac", digest)
	return v
}

func TestVerifyQueryHMAC(t *testing.T) {
	const secret = "shpss_test_secret_value"
	base := map[string]string{
		"code":      "abc123",
		"shop":      "acme.myshopify.com",
		"state":     "nonce-xyz",
		"timestamp": "1700000000",
	}

	t.Run("correctly signed passes", func(t *testing.T) {
		v := signQuery(secret, base)
		if err := service.VerifyQueryHMAC(v, secret); err != nil {
			t.Fatalf("expected valid HMAC to pass, got %v", err)
		}
	})

	t.Run("tampered param fails", func(t *testing.T) {
		v := signQuery(secret, base)
		v.Set("shop", "evil.myshopify.com") // tamper after signing
		if err := service.VerifyQueryHMAC(v, secret); err == nil {
			t.Fatal("expected tampered query to FAIL HMAC verification")
		}
	})

	t.Run("wrong secret fails", func(t *testing.T) {
		v := signQuery(secret, base)
		if err := service.VerifyQueryHMAC(v, "wrong_secret"); err == nil {
			t.Fatal("expected wrong secret to FAIL HMAC verification")
		}
	})

	t.Run("missing hmac fails", func(t *testing.T) {
		v := url.Values{}
		for k, val := range base {
			v.Set(k, val)
		}
		if err := service.VerifyQueryHMAC(v, secret); err == nil {
			t.Fatal("expected missing hmac to FAIL")
		}
	})
}

// ---- state nonce sign + verify (CSRF) ----

func TestStateNonce(t *testing.T) {
	const secret = "state_signing_secret"

	t.Run("signed value verifies and returns nonce", func(t *testing.T) {
		nonce := "the-random-nonce"
		signed := service.SignState(secret, nonce)
		if signed == nonce {
			t.Fatal("signed state must differ from raw nonce")
		}
		got, err := service.VerifyState(secret, signed)
		if err != nil {
			t.Fatalf("verify signed state: %v", err)
		}
		if got != nonce {
			t.Fatalf("recovered nonce = %q, want %q", got, nonce)
		}
	})

	t.Run("tampered signature rejected", func(t *testing.T) {
		signed := service.SignState(secret, "n1")
		tampered := signed + "x"
		if _, err := service.VerifyState(secret, tampered); err == nil {
			t.Fatal("expected tampered signed state to be rejected")
		}
	})

	t.Run("wrong secret rejected", func(t *testing.T) {
		signed := service.SignState(secret, "n1")
		if _, err := service.VerifyState("other_secret", signed); err == nil {
			t.Fatal("expected wrong-secret state to be rejected")
		}
	})

	t.Run("query state must equal cookie nonce", func(t *testing.T) {
		// The handler compares the decoded cookie nonce to the raw `state` query
		// param. Mismatch must be rejected — exercised here at the unit level.
		cookieNonce, _ := service.VerifyState(secret, service.SignState(secret, "real"))
		if service.ConstantTimeEqual(cookieNonce, "forged") {
			t.Fatal("mismatched state must not compare equal")
		}
		if !service.ConstantTimeEqual(cookieNonce, "real") {
			t.Fatal("matching state must compare equal")
		}
	})
}

// ---- App Bridge session token (JWT HS256) ----

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// makeJWT builds an HS256 JWT signed with secret from the given claims.
func makeJWT(t *testing.T, secret string, claims map[string]any) string {
	t.Helper()
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(claims)
	signingInput := b64url(hb) + "." + b64url(cb)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + b64url(mac.Sum(nil))
}

func validClaims() map[string]any {
	now := time.Now().Unix()
	return map[string]any{
		"iss":  "https://acme.myshopify.com/admin",
		"dest": "https://acme.myshopify.com",
		"aud":  "test-api-key",
		"sub":  "42",
		"exp":  now + 600,
		"nbf":  now - 10,
		"iat":  now - 10,
	}
}

func TestVerifySessionToken(t *testing.T) {
	const secret = "jwt_secret"
	const apiKey = "test-api-key"

	t.Run("valid token resolves shop", func(t *testing.T) {
		tok := makeJWT(t, secret, validClaims())
		claims, err := service.VerifySessionToken(tok, secret, apiKey)
		if err != nil {
			t.Fatalf("valid token rejected: %v", err)
		}
		if claims.ShopDomain() != "acme.myshopify.com" {
			t.Fatalf("shop = %q, want acme.myshopify.com", claims.ShopDomain())
		}
	})

	t.Run("bad signature rejected", func(t *testing.T) {
		tok := makeJWT(t, "WRONG_secret", validClaims())
		if _, err := service.VerifySessionToken(tok, secret, apiKey); err == nil {
			t.Fatal("expected bad-signature token to be rejected")
		}
	})

	t.Run("wrong aud rejected", func(t *testing.T) {
		c := validClaims()
		c["aud"] = "some-other-app"
		tok := makeJWT(t, secret, c)
		if _, err := service.VerifySessionToken(tok, secret, apiKey); err == nil {
			t.Fatal("expected wrong-aud token to be rejected")
		}
	})

	t.Run("expired rejected", func(t *testing.T) {
		c := validClaims()
		c["exp"] = time.Now().Unix() - 60
		tok := makeJWT(t, secret, c)
		if _, err := service.VerifySessionToken(tok, secret, apiKey); err == nil {
			t.Fatal("expected expired token to be rejected")
		}
	})

	t.Run("not-yet-valid (nbf future) rejected", func(t *testing.T) {
		c := validClaims()
		c["nbf"] = time.Now().Unix() + 600
		tok := makeJWT(t, secret, c)
		if _, err := service.VerifySessionToken(tok, secret, apiKey); err == nil {
			t.Fatal("expected nbf-in-future token to be rejected")
		}
	})

	t.Run("dest with non-myshopify host rejected", func(t *testing.T) {
		c := validClaims()
		c["dest"] = "https://evil.com"
		c["iss"] = "https://evil.com/admin"
		tok := makeJWT(t, secret, c)
		if _, err := service.VerifySessionToken(tok, secret, apiKey); err == nil {
			t.Fatal("expected non-myshopify dest to be rejected")
		}
	})

	t.Run("alg none rejected", func(t *testing.T) {
		header := map[string]string{"alg": "none", "typ": "JWT"}
		hb, _ := json.Marshal(header)
		cb, _ := json.Marshal(validClaims())
		tok := b64url(hb) + "." + b64url(cb) + "."
		if _, err := service.VerifySessionToken(tok, secret, apiKey); err == nil {
			t.Fatal("expected alg=none token to be rejected")
		}
	})

	t.Run("malformed token rejected", func(t *testing.T) {
		if _, err := service.VerifySessionToken("not.a.jwt.at.all", secret, apiKey); err == nil {
			t.Fatal("expected malformed token to be rejected")
		}
		if _, err := service.VerifySessionToken("", secret, apiKey); err == nil {
			t.Fatal("expected empty token to be rejected")
		}
	})
}
