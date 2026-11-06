// Package chatoauth implements the public Sign in with ChatGPT flow. Credentials
// live outside ARTEX's browsable data directory and never enter LLM profiles.
package chatoauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	Issuer    = "https://auth.openai.com"
	APIBase   = "https://api.openai.com/v1"
	KeyPrefix = "oauth-chatgpt:"
	scope     = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
)

type Credential struct {
	ClientID     string    `json:"client_id"`
	Subject      string    `json:"subject"`
	Email        string    `json:"email"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token"`
	Scope        string    `json:"scope"`
	ExpiresAt    time.Time `json:"expires_at"`
}
type diskState struct {
	HostID   string                `json:"host_id"`
	Active   string                `json:"active"`
	Accounts map[string]Credential `json:"accounts"`
}
type attempt struct {
	State, Nonce, Verifier, Redirect, ClientID, Subject string
	Expires                                             time.Time
}
type Status struct {
	Connected bool   `json:"connected"`
	Pending   bool   `json:"pending"`
	AccountID string `json:"account_id,omitempty"`
	Email     string `json:"email,omitempty"`
	Error     string `json:"error,omitempty"`
}
type Manager struct {
	mu              sync.Mutex
	path            string
	client          *http.Client
	issuer, apiBase string
	disk            diskState
	pending         *attempt
	lastError       string
}

func New(path string) *Manager {
	return &Manager{path: path, issuer: Issuer, apiBase: APIBase, client: &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

var defaultOnce sync.Once
var defaultManager *Manager

func Default() *Manager {
	defaultOnce.Do(func() {
		dir := os.Getenv("ARTEX_OAUTH_DIR")
		if dir == "" {
			exe, _ := os.Executable()
			dir = filepath.Join(filepath.Dir(exe), ".oauth")
		}
		defaultManager = New(filepath.Join(dir, "chatgpt.json"))
	})
	return defaultManager
}
func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func (m *Manager) load() error {
	if m.disk.Accounts != nil {
		return nil
	}
	b, err := os.ReadFile(m.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot read OAuth credentials")
	}
	if err == nil {
		if json.Unmarshal(b, &m.disk) != nil {
			return errors.New("invalid OAuth credential file")
		}
	}
	if m.disk.Accounts == nil {
		m.disk.Accounts = map[string]Credential{}
	}
	if m.disk.HostID == "" || (strings.HasPrefix(m.disk.HostID, "artex-") && len(m.disk.Accounts) == 0) {
		m.disk.HostID = "urn:uuid:" + uuid.NewString()
		return m.save()
	}
	return nil
}
func (m *Manager) save() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(m.disk)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.path), ".oauth-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, m.path)
}
func (m *Manager) Status() (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return Status{}, err
	}
	c, ok := m.disk.Accounts[m.disk.Active]
	return Status{Connected: ok, Pending: m.pending != nil && time.Now().Before(m.pending.Expires), AccountID: m.disk.Active, Email: c.Email, Error: m.lastError}, nil
}

// Start retains the active account until a new authorization is fully validated.
func (m *Manager) Start(redirect string) (string, error) {
	u, err := url.Parse(redirect)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Path != "/auth/callback" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", errors.New("OAuth requires a 127.0.0.1 loopback callback")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err = m.load(); err != nil {
		return "", err
	}
	a := &attempt{State: random(), Nonce: random(), Verifier: random(), Redirect: redirect, ClientID: "dynamic_agent_client", Expires: time.Now().Add(10 * time.Minute)}
	q := url.Values{"client_id": {a.ClientID}, "agent_name_hint": {"ARTEX Global"}, "ext_agent_host_id": {m.disk.HostID}, "response_type": {"code"}, "redirect_uri": {redirect}, "scope": {scope}, "resource": {APIBase}, "state": {a.State}, "nonce": {a.Nonce}, "code_challenge_method": {"S256"}}
	if c, ok := m.disk.Accounts[m.disk.Active]; ok {
		a.ClientID = c.ClientID
		a.Subject = c.Subject
		q.Set("client_id", c.ClientID)
		q.Del("agent_name_hint")
		q.Set("id_token_hint", c.IDToken)
	}
	hash := sha256.Sum256([]byte(a.Verifier))
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(hash[:]))
	m.pending = a
	m.lastError = ""
	return m.issuer + "/api/accounts/authorize?" + q.Encode(), nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func (m *Manager) token(ctx context.Context, v url.Values) (tokenResponse, error) {
	r, err := http.NewRequestWithContext(ctx, "POST", m.issuer+"/api/accounts/oauth/token", strings.NewReader(v.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.client.Do(r)
	if err != nil {
		return tokenResponse{}, errors.New("OAuth token endpoint unavailable; try signing in again")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return tokenResponse{}, fmt.Errorf("OAuth token exchange failed (HTTP %d); sign in again", resp.StatusCode)
	}
	var t tokenResponse
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t) != nil || t.AccessToken == "" || t.ExpiresIn <= 0 || !strings.EqualFold(t.TokenType, "Bearer") {
		return t, errors.New("invalid OAuth token response")
	}
	return t, nil
}
func hasScope(s string) bool {
	for _, v := range strings.Fields(s) {
		if v == "chatgpt.tokens.use.direct" {
			return true
		}
	}
	return false
}

type idClaims struct {
	jwt.RegisteredClaims
	Nonce string `json:"nonce"`
	Email string `json:"email"`
}

func (m *Manager) validateID(ctx context.Context, raw, clientID, nonce string) (*idClaims, error) {
	claims := &idClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("missing signing key ID")
		}
		r, _ := http.NewRequestWithContext(ctx, "GET", m.issuer+"/.well-known/jwks.json", nil)
		resp, err := m.client.Do(r)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, errors.New("signing keys unavailable")
		}
		var keys struct {
			Keys []struct{ Kty, Kid, N, E, Alg, Use string } `json:"keys"`
		}
		if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&keys); err != nil {
			return nil, err
		}
		for _, k := range keys.Keys {
			if k.Kid == kid && k.Kty == "RSA" && (k.Alg == "" || k.Alg == "RS256") && (k.Use == "" || k.Use == "sig") {
				n, e1 := base64.RawURLEncoding.DecodeString(k.N)
				eb, e2 := base64.RawURLEncoding.DecodeString(k.E)
				if e1 != nil || e2 != nil || len(n) < 256 || len(eb) > 4 {
					return nil, errors.New("invalid signing key")
				}
				e := 0
				for _, v := range eb {
					e = e*256 + int(v)
				}
				if e < 3 {
					return nil, errors.New("invalid exponent")
				}
				return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: e}, nil
			}
		}
		return nil, errors.New("unknown signing key")
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(m.issuer), jwt.WithAudience(clientID), jwt.WithExpirationRequired())
	if err != nil || claims.Subject == "" || claims.Nonce != nonce {
		return nil, errors.New("OAuth ID token validation failed")
	}
	return claims, nil
}

func (m *Manager) Callback(ctx context.Context, q url.Values) (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	defer func() {
		if err != nil {
			m.lastError = err.Error()
		}
	}()
	a := m.pending
	if a == nil || time.Now().After(a.Expires) || q.Get("state") == "" || q.Get("state") != a.State {
		return errors.New("OAuth state is invalid or expired; start sign-in again")
	}
	m.pending = nil // consume before exchange; replay is never accepted
	if q.Get("error") != "" {
		return errors.New("ChatGPT authorization was declined or failed")
	}
	clientID := q.Get("client_id")
	if a.ClientID == "dynamic_agent_client" {
		if clientID == "" || clientID == "dynamic_agent_client" {
			return errors.New("OAuth registration did not return an issued client ID")
		}
	} else {
		if clientID != "" && clientID != a.ClientID {
			return errors.New("OAuth client ID mismatch")
		}
		clientID = a.ClientID
	}
	if q.Get("code") == "" {
		return errors.New("OAuth callback has no authorization code")
	}
	t, err := m.token(ctx, url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {q.Get("code")}, "code_verifier": {a.Verifier}, "redirect_uri": {a.Redirect}, "resource": {APIBase}})
	if err != nil {
		return err
	}
	claims, err := m.validateID(ctx, t.IDToken, clientID, a.Nonce)
	if err != nil {
		return err
	}
	if a.Subject != "" && claims.Subject != a.Subject {
		return errors.New("OAuth account identity changed; disconnect before choosing another account")
	}
	if !hasScope(t.Scope) {
		return errors.New("ChatGPT plan usage was not authorized; enable plan usage when signing in")
	}
	h := sha256.Sum256([]byte(clientID + "\x00" + claims.Subject))
	id := fmt.Sprintf("%x", h[:16])
	oldActive := m.disk.Active
	old, had := m.disk.Accounts[id]
	m.disk.Accounts[id] = Credential{ClientID: clientID, Subject: claims.Subject, Email: claims.Email, AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, IDToken: t.IDToken, Scope: t.Scope, ExpiresAt: time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)}
	m.disk.Active = id
	if err = m.save(); err != nil {
		m.disk.Active = oldActive
		if had {
			m.disk.Accounts[id] = old
		} else {
			delete(m.disk.Accounts, id)
		}
		return errors.New("cannot save OAuth credentials")
	}
	m.lastError = ""
	return nil
}

func (m *Manager) AccessToken(ctx context.Context, id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return "", err
	}
	c, ok := m.disk.Accounts[id]
	if !ok {
		return "", errors.New("ChatGPT account disconnected; sign in in System > LLM")
	}
	if time.Until(c.ExpiresAt) > time.Minute {
		return c.AccessToken, nil
	}
	if c.RefreshToken == "" {
		return "", errors.New("ChatGPT session expired; sign in again")
	}
	t, err := m.token(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {c.ClientID}, "refresh_token": {c.RefreshToken}, "resource": {APIBase}})
	if err != nil {
		return "", err
	}
	if t.Scope != "" {
		c.Scope = t.Scope
	}
	if !hasScope(c.Scope) {
		return "", errors.New("ChatGPT plan permission is no longer available")
	}
	c.AccessToken = t.AccessToken
	c.ExpiresAt = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	if t.RefreshToken != "" {
		c.RefreshToken = t.RefreshToken
	}
	// Keep the already validated ID token as a hint; never trust a refresh token's
	// unvalidated ID token as a new account identity.
	old := m.disk.Accounts[id]
	m.disk.Accounts[id] = c
	if err = m.save(); err != nil {
		m.disk.Accounts[id] = old
		return "", errors.New("cannot save refreshed OAuth credentials")
	}
	return c.AccessToken, nil
}
func (m *Manager) Disconnect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return err
	}
	old := m.disk
	m.disk = diskState{HostID: old.HostID, Accounts: map[string]Credential{}}
	if err := m.save(); err != nil {
		m.disk = old
		return err
	}
	m.pending = nil
	m.lastError = ""
	return nil
}

type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (m *Manager) Models(ctx context.Context, id string) ([]Model, error) {
	t, err := m.AccessToken(ctx, id)
	if err != nil {
		return nil, err
	}
	r, _ := http.NewRequestWithContext(ctx, "GET", m.apiBase+"/models", nil)
	r.Header.Set("Authorization", "Bearer "+t)
	resp, err := m.client.Do(r)
	if err != nil {
		return nil, errors.New("ChatGPT model catalog unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ChatGPT model catalog failed (HTTP %d)", resp.StatusCode)
	}
	var v struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
		} `json:"models"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&v); err != nil {
		return nil, errors.New("invalid ChatGPT model catalog")
	}
	out := []Model{}
	for _, a := range v.Models {
		if a.Visibility == "list" && a.Slug != "" {
			out = append(out, Model{a.Slug, a.DisplayName})
		}
	}
	return out, nil
}
