package chatoauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type oauthRig struct {
	m                                                    *Manager
	key                                                  *rsa.PrivateKey
	server                                               *httptest.Server
	query                                                url.Values
	refreshes                                            atomic.Int32
	wrongNonce, wrongAudience, wrongIssuer, missingScope bool
}

func newRig(t *testing.T) *oauthRig {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rig := &oauthRig{key: key}
	rig.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/jwks.json":
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "test", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		case "/api/accounts/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("client_id") != "oaiapp_test" || r.Form.Get("resource") != APIBase {
				t.Error("incorrect token exchange parameters")
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				rig.refreshes.Add(1)
				json.NewEncoder(w).Encode(map[string]any{"access_token": "renewed-access", "refresh_token": "rotated-refresh", "expires_in": 3600, "token_type": "Bearer"})
				return
			}
			if r.Form.Get("redirect_uri") != "http://127.0.0.1:8787/auth/callback" || len(r.Form.Get("code_verifier")) < 43 {
				t.Error("missing callback or PKCE")
			}
			nonce := rig.query.Get("nonce")
			if rig.wrongNonce {
				nonce = "wrong"
			}
			aud := "oaiapp_test"
			if rig.wrongAudience {
				aud = "other"
			}
			issuer := rig.server.URL
			if rig.wrongIssuer {
				issuer = "https://attacker.invalid"
			}
			tok := jwt.NewWithClaims(jwt.SigningMethodRS256, idClaims{RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, Audience: jwt.ClaimStrings{aud}, Subject: "account-1", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, Nonce: nonce, Email: "test@example.invalid"})
			tok.Header["kid"] = "test"
			signed, err := tok.SignedString(key)
			if err != nil {
				t.Error(err)
			}
			granted := scope
			if rig.missingScope {
				granted = "openid profile"
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "test-access", "refresh_token": "test-refresh", "id_token": signed, "expires_in": 3600, "scope": granted, "token_type": "Bearer"})
		case "/models":
			json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]string{"slug": "visible-model", "display_name": "Visible", "visibility": "list"}, map[string]string{"slug": "hidden", "visibility": "hidden"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(rig.server.Close)
	rig.m = New(filepath.Join(t.TempDir(), "private", "chatgpt.json"))
	rig.m.issuer = rig.server.URL
	rig.m.apiBase = rig.server.URL
	return rig
}
func (r *oauthRig) start(t *testing.T) url.Values {
	t.Helper()
	address, err := r.m.Start("http://127.0.0.1:8787/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(address)
	r.query = u.Query()
	return url.Values{"state": {r.query.Get("state")}, "client_id": {"oaiapp_test"}, "code": {"auth-code"}}
}

func TestOAuthValidatedConnectionPersistenceAndRefresh(t *testing.T) {
	r := newRig(t)
	callback := r.start(t)
	if r.query.Get("client_id") != "dynamic_agent_client" || r.query.Get("code_challenge_method") != "S256" {
		t.Fatal("wrong authorization flow")
	}
	if !strings.HasPrefix(r.query.Get("ext_agent_host_id"), "urn:uuid:") {
		t.Fatal("host ID must use the documented UUID URI format")
	}
	if err := r.m.Callback(context.Background(), callback); err != nil {
		t.Fatal(err)
	}
	st, err := r.m.Status()
	if err != nil || !st.Connected || st.Email != "test@example.invalid" {
		t.Fatalf("status %+v %v", st, err)
	}
	if err = r.m.Callback(context.Background(), callback); err == nil {
		t.Fatal("replayed code accepted")
	}
	// A restart reads the same protected identity and credentials.
	restarted := New(r.m.path)
	restarted.issuer = r.server.URL
	restarted.apiBase = r.server.URL
	st2, err := restarted.Status()
	if err != nil || st2.AccountID != st.AccountID {
		t.Fatal("connection did not survive restart")
	}
	models, err := restarted.Models(context.Background(), st.AccountID)
	if err != nil || len(models) != 1 || models[0].ID != "visible-model" {
		t.Fatalf("models: %+v %v", models, err)
	}
	c := restarted.disk.Accounts[st.AccountID]
	c.ExpiresAt = time.Now().Add(-time.Hour)
	restarted.disk.Accounts[st.AccountID] = c
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := restarted.AccessToken(context.Background(), st.AccountID)
			if err != nil || token != "renewed-access" {
				t.Errorf("refresh: %q %v", token, err)
			}
		}()
	}
	wg.Wait()
	if r.refreshes.Load() != 1 {
		t.Fatal("concurrent refresh was not serialized")
	}
	if restarted.disk.Accounts[st.AccountID].RefreshToken != "rotated-refresh" {
		t.Fatal("rotating refresh token not saved")
	}
	if err = restarted.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.AccessToken(context.Background(), st.AccountID); err == nil {
		t.Fatal("disconnected credential still works")
	}
	raw, err := os.ReadFile(r.m.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "test-access") || strings.Contains(string(raw), "rotated-refresh") {
		t.Fatal("tokens persisted after disconnect")
	}
}

func TestOAuthRejectsStateAndInvalidIdentity(t *testing.T) {
	for _, kind := range []string{"state", "expired", "nonce", "audience", "issuer", "scope", "client", "denied"} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t)
			q := r.start(t)
			switch kind {
			case "state":
				q.Set("state", "forged")
			case "expired":
				r.m.pending.Expires = time.Now().Add(-time.Second)
			case "nonce":
				r.wrongNonce = true
			case "audience":
				r.wrongAudience = true
			case "issuer":
				r.wrongIssuer = true
			case "scope":
				r.missingScope = true
			case "client":
				q.Del("client_id")
			case "denied":
				q.Set("error", "access_denied")
			}
			if err := r.m.Callback(context.Background(), q); err == nil {
				t.Fatal("invalid authorization accepted")
			}
			status, err := r.m.Status()
			if err != nil || status.Connected {
				t.Fatal("invalid account stored")
			}
		})
	}
}

func TestOAuthReturningAccountAndCallbackBoundary(t *testing.T) {
	r := newRig(t)
	for _, redirect := range []string{"https://example.com/auth/callback", "http://localhost:8787/auth/callback", "http://127.0.0.1:8787/callback", "http://127.0.0.1:8787/auth/callback?x=1"} {
		if _, err := r.m.Start(redirect); err == nil {
			t.Fatal("invalid redirect accepted", redirect)
		}
	}
	if err := r.m.Callback(context.Background(), r.start(t)); err != nil {
		t.Fatal(err)
	}
	q := r.start(t)
	if r.query.Get("client_id") != "oaiapp_test" || r.query.Get("id_token_hint") == "" || r.query.Get("agent_name_hint") != "" {
		t.Fatal("returning registration was not reused")
	}
	q.Set("client_id", "another-client")
	if err := r.m.Callback(context.Background(), q); err == nil {
		t.Fatal("changed client accepted")
	}
	status, _ := r.m.Status()
	if !status.Connected {
		t.Fatal("failed reauthorization discarded the active account")
	}
}
