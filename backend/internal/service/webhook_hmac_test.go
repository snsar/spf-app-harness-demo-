package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func signWebhook(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// TestVerifyWebhookHMAC_Valid accepts a body signed with the secret.
func TestVerifyWebhookHMAC_Valid(t *testing.T) {
	secret := "shpss_topsecret"
	body := []byte(`{"id":123,"title":"Toy"}`)
	header := signWebhook(secret, body)
	if err := VerifyWebhookHMAC(body, header, secret); err != nil {
		t.Fatalf("valid HMAC rejected: %v", err)
	}
}

// TestVerifyWebhookHMAC_BadSignature rejects a wrong/tampered signature.
func TestVerifyWebhookHMAC_BadSignature(t *testing.T) {
	secret := "shpss_topsecret"
	body := []byte(`{"id":123}`)
	if err := VerifyWebhookHMAC(body, signWebhook("wrong-secret", body), secret); err == nil {
		t.Fatal("wrong-secret HMAC accepted (must reject)")
	}
	if err := VerifyWebhookHMAC(body, "not-base64-!@#", secret); err == nil {
		t.Fatal("malformed header accepted (must reject)")
	}
	if err := VerifyWebhookHMAC(body, "", secret); err == nil {
		t.Fatal("empty header accepted (must reject)")
	}
}

// TestVerifyWebhookHMAC_RawBodyExact proves verification is byte-exact: any
// mutation of the body (even whitespace) invalidates the signature.
func TestVerifyWebhookHMAC_RawBodyExact(t *testing.T) {
	secret := "shpss_topsecret"
	body := []byte(`{"id":123,"title":"Toy"}`)
	header := signWebhook(secret, body)
	mutated := []byte(`{"id":123, "title":"Toy"}`) // extra space
	if err := VerifyWebhookHMAC(mutated, header, secret); err == nil {
		t.Fatal("HMAC accepted a mutated body (must be byte-exact)")
	}
}

// TestVerifyWebhookHMAC_EmptySecret_FailClosed rejects when the secret is empty
// (defense in depth — an empty key makes the MAC forgeable).
func TestVerifyWebhookHMAC_EmptySecret_FailClosed(t *testing.T) {
	body := []byte(`{"id":1}`)
	header := signWebhook("", body) // an attacker could compute this with key ""
	if err := VerifyWebhookHMAC(body, header, ""); err == nil {
		t.Fatal("empty secret accepted (must fail closed)")
	}
}
