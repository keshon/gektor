package openai

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
}

func randomID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func exchange(ctx context.Context, fields map[string]string) (tokenResponse, error) {
	var result tokenResponse
	values := url.Values{}
	for k, v := range fields {
		values.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", issuer+"/api/accounts/oauth/token", strings.NewReader(values.Encode()))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return result, fmt.Errorf("ChatGPT token exchange could not connect")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return result, fmt.Errorf("ChatGPT token exchange HTTP %d; sign in again if access expired", resp.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return result, fmt.Errorf("invalid ChatGPT token response")
	}
	if result.AccessToken == "" || result.ExpiresIn <= 0 || !strings.EqualFold(result.TokenType, "Bearer") {
		return result, fmt.Errorf("incomplete ChatGPT token response")
	}
	return result, nil
}

// Login calls open with an authorization URL after binding a loopback listener.
// Credentials are accepted only after signature, identity, nonce and scope checks.
func (m *Manager) Login(ctx context.Context, open func(string) error, newAccount bool) error {
	var host, clientID, subject string
	err := m.withStore(ctx, func(s *credentials) (bool, error) {
		changed := false
		if s.HostID == "" {
			b := make([]byte, 16)
			if _, err := rand.Read(b); err != nil {
				return false, err
			}
			b[6] = (b[6] & 15) | 64
			b[8] = (b[8] & 63) | 128
			h := hex.EncodeToString(b)
			s.HostID = "urn:uuid:" + h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
			changed = true
		}
		host = s.HostID
		if a, ok := s.Accounts[s.Active]; ok && !newAccount {
			clientID, subject = a.ClientID, a.Subject
		}
		return changed, nil
	})
	if err != nil {
		return err
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return fmt.Errorf("cannot load OpenAI sign-in metadata")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	callback := "http://" + listener.Addr().String() + "/auth/callback"
	state, nonce, verifier := randomID(), randomID(), randomID()
	digest := sha256.Sum256([]byte(verifier))
	newRegistration := clientID == ""
	if newRegistration {
		clientID = "dynamic_agent_client"
	}
	query := url.Values{"client_id": {clientID}, "ext_agent_host_id": {host}, "response_type": {"code"}, "redirect_uri": {callback}, "scope": {scopes}, "resource": {API}, "state": {state}, "nonce": {nonce}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}}
	if newRegistration {
		query.Set("agent_name_hint", "Gektor")
	}
	returned := make(chan url.Values, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.Method != "GET" || r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid sign-in callback.", 400)
			return
		}
		select {
		case returned <- r.URL.Query():
			fmt.Fprint(w, "Return to Gektor to check the sign-in result.")
		default:
			http.Error(w, "Sign-in callback already received.", http.StatusConflict)
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	if err = open(issuer + "/api/accounts/authorize?" + query.Encode()); err != nil {
		return err
	}
	var params url.Values
	select {
	case <-ctx.Done():
		return ctx.Err()
	case params = <-returned:
	}
	if params.Get("error") != "" {
		return fmt.Errorf("ChatGPT sign-in was declined or failed")
	}
	issued := params.Get("client_id")
	if newRegistration {
		if issued == "" || issued == "dynamic_agent_client" {
			return fmt.Errorf("ChatGPT registration returned no issued client ID")
		}
		clientID = issued
	} else if issued != "" && issued != clientID {
		return fmt.Errorf("ChatGPT callback changed the account registration")
	}
	if params.Get("code") == "" {
		return fmt.Errorf("ChatGPT callback returned no authorization code")
	}
	t, err := exchange(ctx, map[string]string{"grant_type": "authorization_code", "client_id": clientID, "code": params.Get("code"), "code_verifier": verifier, "redirect_uri": callback, "resource": API})
	if err != nil {
		return err
	}
	id, err := provider.Verifier(&oidc.Config{ClientID: clientID}).Verify(ctx, t.IDToken)
	if err != nil {
		return fmt.Errorf("ChatGPT ID token validation failed")
	}
	if id.Nonce != nonce || id.Subject == "" || (subject != "" && id.Subject != subject) {
		return fmt.Errorf("ChatGPT sign-in identity or nonce mismatch")
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err = id.Claims(&claims); err != nil {
		return fmt.Errorf("invalid ChatGPT identity claims")
	}
	a := Account{ClientID: clientID, Subject: id.Subject, Email: claims.Email, AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, IDToken: t.IDToken, Scope: t.Scope, Expires: time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)}
	err = m.withStore(ctx, func(s *credentials) (bool, error) { s.Accounts[clientID] = a; s.Active = clientID; return true, nil })
	if err != nil {
		return err
	}
	if !permitted(t.Scope) {
		return fmt.Errorf("signed in, but ChatGPT plan usage was not authorized; inference is disabled")
	}
	return nil
}

func (m *Manager) Logout(ctx context.Context) error {
	return m.withStore(ctx, func(s *credentials) (bool, error) {
		a, ok := s.Accounts[s.Active]
		if !ok {
			return false, nil
		}
		if a.RefreshToken != "" {
			req, err := http.NewRequestWithContext(ctx, "GET", issuer+"/.well-known/openid-configuration", nil)
			if err != nil {
				return false, err
			}
			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return false, fmt.Errorf("logout could not load OpenAI metadata; credentials retained for retry")
			}
			var metadata struct {
				Revocation string `json:"revocation_endpoint"`
			}
			err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&metadata)
			resp.Body.Close()
			u, parseErr := url.Parse(metadata.Revocation)
			if err != nil || parseErr != nil || u.Scheme != "https" || u.Host != "auth.openai.com" {
				return false, fmt.Errorf("invalid OpenAI revocation endpoint")
			}
			values := url.Values{"token": {a.RefreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {a.ClientID}}
			req, err = http.NewRequestWithContext(ctx, "POST", metadata.Revocation, strings.NewReader(values.Encode()))
			if err != nil {
				return false, err
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err = client.Do(req)
			if err != nil {
				return false, fmt.Errorf("ChatGPT logout could not connect; retry logout")
			}
			resp.Body.Close()
			if resp.StatusCode != 200 {
				return false, fmt.Errorf("ChatGPT logout HTTP %d; credentials retained for retry", resp.StatusCode)
			}
		}
		a.AccessToken, a.RefreshToken, a.IDToken = "", "", ""
		s.Accounts[s.Active] = a
		return true, nil
	})
}
