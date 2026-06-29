package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"testing"
)

// signHS256 forges an HS256 JWT signed with the given secret. With an empty
// secret this is exactly the exploit the security reviewer reproduced: anyone
// can mint a token the verifier would otherwise trust.
func signHS256(t *testing.T, secret string, headerJSON, payloadJSON string) string {
	t.Helper()
	h := base64.RawURLEncoding.EncodeToString([]byte(headerJSON))
	p := base64.RawURLEncoding.EncodeToString([]byte(payloadJSON))
	signingInput := h + "." + p
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + sig
}

// TestVerifySessionToken_EmptySecret_Rejected is the belt-and-suspenders guard
// for F3-SEC-1: even if a caller forgets the boot-time check, the verify
// primitive itself must fail closed when the secret is empty. Without this, a
// forged token signed with "" is accepted (the reproduced exploit).
func TestVerifySessionToken_EmptySecret_Rejected(t *testing.T) {
	const apiKey = "key123"
	// A token forged with the empty secret, otherwise well-formed.
	forged := signHS256(t, "",
		`{"alg":"HS256","typ":"JWT"}`,
		`{"aud":"key123","dest":"https://acme.myshopify.com","exp":9999999999}`)

	_, err := VerifySessionToken(forged, "", apiKey)
	if err == nil {
		t.Fatalf("VerifySessionToken accepted a token forged with an EMPTY secret; auth is forgeable")
	}
}

// TestVerifyQueryHMAC_EmptySecret_Rejected ensures the request-HMAC primitive
// fails closed on an empty secret too (the OAuth callback trust path).
func TestVerifyQueryHMAC_EmptySecret_Rejected(t *testing.T) {
	q := url.Values{}
	q.Set("shop", "acme.myshopify.com")
	q.Set("code", "abc")
	// Forge the hmac with the empty secret.
	keys := []string{"code", "shop"}
	msg := "code=" + q.Get("code") + "&shop=" + q.Get("shop")
	_ = keys
	mac := hmac.New(sha256.New, []byte(""))
	mac.Write([]byte(msg))
	forgedHMAC := ""
	for _, b := range mac.Sum(nil) {
		const hexd = "0123456789abcdef"
		forgedHMAC += string(hexd[b>>4]) + string(hexd[b&0x0f])
	}
	q.Set("hmac", forgedHMAC)

	if err := VerifyQueryHMAC(q, ""); err == nil {
		t.Fatalf("VerifyQueryHMAC accepted a query HMAC forged with an EMPTY secret")
	}
}

// TestVerifyState_EmptySecret_Rejected ensures signed-state verification fails
// closed on an empty secret.
func TestVerifyState_EmptySecret_Rejected(t *testing.T) {
	signed := SignState("", "nonce-value")
	if _, err := VerifyState("", signed); err == nil {
		t.Fatalf("VerifyState accepted a state token signed with an EMPTY secret")
	}
}
