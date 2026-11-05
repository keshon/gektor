package openai

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestLoginValidatesSignedIdentityAndNonce(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, badNonce := range []bool{false, true} {
		t.Run(fmt.Sprint(badNonce), func(t *testing.T) {
			m := &Manager{Dir: t.TempDir()}
			var nonce string
			transport := http.DefaultTransport
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "127.0.0.1" || r.URL.Hostname() == "127.0.0.1" {
					return transport.RoundTrip(r)
				}
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					return fakeResponse(`{"issuer":"https://auth.openai.com","authorization_endpoint":"https://auth.openai.com/api/accounts/authorize","token_endpoint":"https://auth.openai.com/api/accounts/oauth/token","jwks_uri":"https://auth.openai.com/jwks","id_token_signing_alg_values_supported":["RS256"]}`), nil
				case "/jwks":
					return fakeResponse(fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"test","alg":"RS256","use":"sig","n":"%s","e":"AQAB"}]}`, base64.RawURLEncoding.EncodeToString(key.N.Bytes()))), nil
				case "/api/accounts/oauth/token":
					_ = r.ParseForm()
					if r.Form.Get("client_id") != "issued-client" || r.Form.Get("code_verifier") == "" {
						t.Error("invalid token exchange")
					}
					if badNonce {
						nonce = "wrong"
					}
					claims, _ := json.Marshal(map[string]any{"iss": issuer, "aud": "issued-client", "sub": "subject", "email": "test@example.com", "nonce": nonce, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()})
					header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`))
					signed := header + "." + base64.RawURLEncoding.EncodeToString(claims)
					hash := sha256.Sum256([]byte(signed))
					sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
					raw, _ := json.Marshal(map[string]any{"access_token": "access-secret", "refresh_token": "refresh-secret", "id_token": signed + "." + base64.RawURLEncoding.EncodeToString(sig), "token_type": "Bearer", "expires_in": 3600, "scope": scopes})
					return fakeResponse(string(raw)), nil
				default:
					t.Error("unexpected request", r.URL)
					return nil, fmt.Errorf("unexpected request")
				}
			})
			defer func() { http.DefaultTransport = transport }()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := m.Login(ctx, func(raw string) error {
				u, _ := url.Parse(raw)
				q := u.Query()
				nonce = q.Get("nonce")
				if q.Get("client_id") != "dynamic_agent_client" || !strings.HasPrefix(q.Get("ext_agent_host_id"), "urn:uuid:") {
					t.Error("bad registration")
				}
				callback, _ := url.Parse(q.Get("redirect_uri"))
				callback.RawQuery = url.Values{"state": {q.Get("state")}, "code": {"code"}, "client_id": {"issued-client"}}.Encode()
				resp, err := http.Get(callback.String())
				if err != nil {
					return err
				}
				resp.Body.Close()
				return nil
			}, false)
			if badNonce {
				if err == nil {
					t.Fatal("invalid nonce accepted")
				}
				label, _ := m.Status(ctx)
				if strings.Contains(label, "test@example.com") {
					t.Fatal("invalid identity saved")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			status, err := m.Status(ctx)
			if err != nil || status != "test@example.com" {
				t.Fatal(status, err)
			}
			token, err := m.Token(ctx)
			if err != nil || token != "access-secret" {
				t.Fatal("credential roundtrip failed", err)
			}
		})
	}
}

func TestRefreshIsSerializedAndReplacesTokenAcrossManagers(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{Dir: dir}
	err := m.withStore(t.Context(), func(s *credentials) (bool, error) {
		s.Active = "client"
		s.Accounts["client"] = Account{ClientID: "client", AccessToken: "old-access", RefreshToken: "old-refresh", Scope: scopes, Expires: time.Now().Add(-time.Hour)}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport
	defer func() { http.DefaultTransport = transport }()
	var refreshes atomic.Int32
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		refreshes.Add(1)
		_ = r.ParseForm()
		if r.Form.Get("refresh_token") != "old-refresh" {
			t.Error("wrong refresh token")
		}
		return fakeResponse(`{"access_token":"new-access","refresh_token":"new-refresh","token_type":"Bearer","expires_in":3600}`), nil
	})
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			token, err := (&Manager{Dir: dir}).Token(t.Context())
			if err != nil || token != "new-access" {
				t.Error("refresh failed", err)
			}
		})
	}
	wg.Wait()
	if refreshes.Load() != 1 {
		t.Fatal("refresh raced", refreshes.Load())
	}
	raw, err := os.ReadFile(filepath.Join(dir, "credentials"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("empty store")
	}
}
