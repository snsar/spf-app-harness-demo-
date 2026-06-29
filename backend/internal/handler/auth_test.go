package handler_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gpsr/backend/internal/handler"
	"github.com/gpsr/backend/internal/model"
	"github.com/gpsr/backend/internal/service"
)

const (
	testSecret = "test_app_secret"
	testAPIKey = "test_api_key"
)

// --- fakes ---

// fakeShopStore implements handler.ShopStore + handler.ShopSaver in memory.
type fakeShopStore struct {
	shops map[string]*model.Shop
}

func newFakeShopStore() *fakeShopStore {
	return &fakeShopStore{shops: map[string]*model.Shop{}}
}

func (f *fakeShopStore) GetByDomain(_ context.Context, domain string) (*model.Shop, error) {
	return f.shops[domain], nil
}

func (f *fakeShopStore) Upsert(_ context.Context, domain, token, scope string) error {
	f.shops[domain] = &model.Shop{ID: int64(len(f.shops) + 1), ShopDomain: domain, AccessToken: token, Scope: scope}
	return nil
}

// fakeExchanger implements service.TokenExchanger without network.
type fakeExchanger struct {
	tok service.AccessToken
	err error
}

func (f *fakeExchanger) ExchangeCode(_ context.Context, shop, code string) (service.AccessToken, error) {
	return f.tok, f.err
}

func newAuthRouter(store *fakeShopStore, ex service.TokenExchanger) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	deps := handler.AuthDeps{
		APIKey:    testAPIKey,
		APISecret: testSecret,
		AppURL:    "https://gpsr.example.com",
		Scopes:    "read_products,write_products",
		Shops:     store,
		Exchanger: ex,
	}
	handler.RegisterAuthRoutes(r, deps)
	return r
}

// --- /auth ---

func TestAuthBegin_RedirectsToShopify(t *testing.T) {
	r := newAuthRouter(newFakeShopStore(), &fakeExchanger{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth?shop=acme.myshopify.com", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("bad Location: %v", err)
	}
	if u.Host != "acme.myshopify.com" || u.Path != "/admin/oauth/authorize" {
		t.Fatalf("redirect target = %s", loc)
	}
	q := u.Query()
	if q.Get("client_id") != testAPIKey {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("scope") != "read_products,write_products" {
		t.Errorf("scope = %q", q.Get("scope"))
	}
	if q.Get("redirect_uri") != "https://gpsr.example.com/auth/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("state") == "" {
		t.Error("state must be present")
	}
	// State cookie must be set, HttpOnly.
	var stateCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "gpsr_oauth_state" {
			stateCookie = c
		}
	}
	if stateCookie == nil {
		t.Fatal("expected gpsr_oauth_state cookie")
	}
	if !stateCookie.HttpOnly {
		t.Error("state cookie must be HttpOnly")
	}
}

func TestAuthBegin_RejectsBadShop_NoOpenRedirect(t *testing.T) {
	r := newAuthRouter(newFakeShopStore(), &fakeExchanger{})
	for _, bad := range []string{"evil.com", "acme.myshopify.com.evil.com", "", "https://acme.myshopify.com"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/auth?shop="+url.QueryEscape(bad), nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("shop=%q: status = %d, want 400 (no redirect)", bad, w.Code)
		}
		if loc := w.Header().Get("Location"); loc != "" {
			t.Errorf("shop=%q: must not emit a redirect, got Location %q", bad, loc)
		}
	}
}

// --- /auth/callback ---

func signedCallback(t *testing.T, shop, code, nonce string) (*url.URL, *http.Cookie) {
	t.Helper()
	params := map[string]string{
		"code":      code,
		"shop":      shop,
		"state":     nonce,
		"timestamp": fmt.Sprintf("%d", time.Now().Unix()),
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte(strings.Join(parts, "&")))
	digest := fmt.Sprintf("%x", mac.Sum(nil))

	v := url.Values{}
	for k, val := range params {
		v.Set(k, val)
	}
	v.Set("hmac", digest)
	u := &url.URL{Path: "/auth/callback", RawQuery: v.Encode()}

	cookie := &http.Cookie{Name: "gpsr_oauth_state", Value: service.SignState(testSecret, nonce)}
	return u, cookie
}

func TestAuthCallback_Success_StoresTokenAndRedirects(t *testing.T) {
	store := newFakeShopStore()
	ex := &fakeExchanger{tok: service.AccessToken{AccessToken: "shpat_xyz", Scope: "read_products"}}
	r := newAuthRouter(store, ex)

	u, cookie := signedCallback(t, "acme.myshopify.com", "the-code", "nonce-1")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, u.String(), nil)
	req.AddCookie(cookie)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "acme.myshopify.com/admin/apps/") {
		t.Errorf("expected embedded-app redirect, got %q", loc)
	}
	got := store.shops["acme.myshopify.com"]
	if got == nil || got.AccessToken != "shpat_xyz" {
		t.Fatalf("token not stored: %+v", got)
	}
	// The stored token must never appear in the response.
	if strings.Contains(w.Body.String()+loc, "shpat_xyz") {
		t.Error("access token leaked into response")
	}
}

func TestAuthCallback_TamperedHMAC_Rejected(t *testing.T) {
	r := newAuthRouter(newFakeShopStore(), &fakeExchanger{})
	u, cookie := signedCallback(t, "acme.myshopify.com", "the-code", "nonce-1")
	// Tamper: change shop after signing.
	q := u.Query()
	q.Set("shop", "evil.myshopify.com")
	u.RawQuery = q.Encode()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, u.String(), nil)
	req.AddCookie(cookie)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("tampered HMAC: status = %d, want 400", w.Code)
	}
}

func TestAuthCallback_StateMismatch_Rejected(t *testing.T) {
	r := newAuthRouter(newFakeShopStore(), &fakeExchanger{})
	u, _ := signedCallback(t, "acme.myshopify.com", "the-code", "nonce-1")
	// Provide a cookie carrying a DIFFERENT nonce.
	badCookie := &http.Cookie{Name: "gpsr_oauth_state", Value: service.SignState(testSecret, "different-nonce")}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, u.String(), nil)
	req.AddCookie(badCookie)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("state mismatch: status = %d, want 400", w.Code)
	}
}

func TestAuthCallback_MissingStateCookie_Rejected(t *testing.T) {
	r := newAuthRouter(newFakeShopStore(), &fakeExchanger{})
	u, _ := signedCallback(t, "acme.myshopify.com", "the-code", "nonce-1")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, u.String(), nil) // no cookie
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing state cookie: status = %d, want 400", w.Code)
	}
}

func TestAuthCallback_ExchangeFails_502(t *testing.T) {
	ex := &fakeExchanger{err: fmt.Errorf("token endpoint returned status 401")}
	r := newAuthRouter(newFakeShopStore(), ex)
	u, cookie := signedCallback(t, "acme.myshopify.com", "the-code", "nonce-1")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, u.String(), nil)
	req.AddCookie(cookie)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("exchange failure: status = %d, want 502", w.Code)
	}
}

// --- session-token middleware + /api/me ---

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func makeJWT(secret string, claims map[string]any) string {
	hb, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	cb, _ := json.Marshal(claims)
	in := b64url(hb) + "." + b64url(cb)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(in))
	return in + "." + b64url(mac.Sum(nil))
}

func newMeRouter(store *fakeShopStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	deps := handler.AuthDeps{APIKey: testAPIKey, APISecret: testSecret, AppURL: "https://gpsr.example.com", Shops: store}
	api := r.Group("/api")
	api.Use(handler.RequireSessionToken(deps))
	handler.RegisterMeRoute(api)
	return r
}

func validJWTClaims() map[string]any {
	now := time.Now().Unix()
	return map[string]any{
		"iss": "https://acme.myshopify.com/admin", "dest": "https://acme.myshopify.com",
		"aud": testAPIKey, "sub": "1", "exp": now + 600, "nbf": now - 10, "iat": now - 10,
	}
}

func TestApiMe_ValidToken_ReturnsShop(t *testing.T) {
	store := newFakeShopStore()
	_ = store.Upsert(context.Background(), "acme.myshopify.com", "tok", "read_products")
	r := newMeRouter(store)

	tok := makeJWT(testSecret, validJWTClaims())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if body["shop_domain"] != "acme.myshopify.com" {
		t.Fatalf("shop_domain = %q", body["shop_domain"])
	}
	if strings.Contains(w.Body.String(), "tok") {
		t.Error("access token must not appear in /api/me response")
	}
}

func TestApiMe_Rejects(t *testing.T) {
	store := newFakeShopStore()
	_ = store.Upsert(context.Background(), "acme.myshopify.com", "tok", "read_products")

	cases := []struct {
		name   string
		header string
	}{
		{"missing header", ""},
		{"not bearer", "Basic abc"},
		{"bad signature", "Bearer " + makeJWT("WRONG", validJWTClaims())},
		{"wrong aud", "Bearer " + makeJWT(testSecret, func() map[string]any {
			c := validJWTClaims()
			c["aud"] = "other"
			return c
		}())},
		{"expired", "Bearer " + makeJWT(testSecret, func() map[string]any {
			c := validJWTClaims()
			c["exp"] = time.Now().Unix() - 60
			return c
		}())},
		{"garbage", "Bearer not.a.jwt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newMeRouter(store)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			r.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s: status = %d, want 401", tc.name, w.Code)
			}
		})
	}
}

func TestApiMe_UninstalledShop_Rejected(t *testing.T) {
	// Valid JWT but the shop has no row (uninstalled / unknown tenant) → 401.
	store := newFakeShopStore() // empty
	r := newMeRouter(store)
	tok := makeJWT(testSecret, validJWTClaims())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for uninstalled shop", w.Code)
	}
}
