package openai

import (
	"testing"
	"time"
)

func TestBoundAccountDoesNotFollowSelectionOrReplacement(t *testing.T) {
	m := &Manager{Dir: t.TempDir()}
	err := m.withStore(t.Context(), func(s *credentials) (bool, error) {
		s.Active = "a"
		s.Accounts["a"] = Account{ClientID: "a", Subject: "subject-a", AccessToken: "token-a", Scope: scopes, Expires: time.Now().Add(time.Hour)}
		s.Accounts["b"] = Account{ClientID: "b", Subject: "subject-b", AccessToken: "token-b", Scope: scopes, Expires: time.Now().Add(time.Hour)}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := m.Bind(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Select(t.Context(), "b"); err != nil {
		t.Fatal(err)
	}
	if token, err := bound.Token(t.Context()); err != nil || token != "token-a" {
		t.Fatal("binding followed selection", err)
	}
	err = m.withStore(t.Context(), func(s *credentials) (bool, error) {
		a := s.Accounts["a"]
		a.Subject = "replacement"
		s.Accounts["a"] = a
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bound.Token(t.Context()); err == nil {
		t.Fatal("replacement identity accepted")
	}
	err = m.withStore(t.Context(), func(s *credentials) (bool, error) { delete(s.Accounts, "a"); return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bound.Token(t.Context()); err == nil {
		t.Fatal("missing registration accepted")
	}
}

func TestBoundAccountRejectsLogoutAndWithdrawnPermission(t *testing.T) {
	for _, scenario := range []string{"logout", "withdrawn"} {
		t.Run(scenario, func(t *testing.T) {
			m := &Manager{Dir: t.TempDir()}
			err := m.withStore(t.Context(), func(s *credentials) (bool, error) {
				s.Active = "a"
				s.Accounts["a"] = Account{ClientID: "a", Subject: "subject", AccessToken: "test", Scope: scopes, Expires: time.Now().Add(time.Hour)}
				return true, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			bound, err := m.Bind(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "logout" {
				err = m.Logout(t.Context())
			} else {
				err = m.withStore(t.Context(), func(s *credentials) (bool, error) {
					a := s.Accounts["a"]
					a.Scope = "openid"
					s.Accounts["a"] = a
					return true, nil
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := bound.Token(t.Context()); err == nil {
				t.Fatal("invalidated binding accepted")
			}
		})
	}
}
