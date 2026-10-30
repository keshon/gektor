package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
)

// Uses a local fake login, never a real account or the operator's profile.
func TestBrowserPersistentProfileLive(t *testing.T) {
	if os.Getenv("GEKTOR_BROWSER_SMOKE") != "1" {
		t.Skip("set GEKTOR_BROWSER_SMOKE=1 to launch local Chrome")
	}
	t.Setenv("USERPROFILE", browserHostHome)
	t.Setenv("HOME", browserUnixHome)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	root, profile := t.TempDir(), t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "login", Value: "fixture", Path: "/", MaxAge: 3600, HttpOnly: true})
		}
		if cookie, err := r.Cookie("login"); err == nil && cookie.Value == "fixture" {
			w.Write([]byte("<h1>Signed in</h1>"))
		} else {
			w.Write([]byte("<h1>Signed out</h1>"))
		}
	}))
	defer server.Close()
	channel := os.Getenv("GEKTOR_BROWSER_CHANNEL")
	if channel == "" {
		channel = "chrome"
	}
	if err := PrepareBrowser(ctx, channel, root, func(s string) { t.Log(s) }); err != nil {
		t.Fatal(err)
	}
	for pass := range 2 {
		func() {
			release, err := lockBrowserProfile(profile)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			list, client, err := startBrowser(ctx, channel, root, profile, true)
			if err != nil {
				t.Fatal(err)
			}
			b := &Browser{client: client, tools: list}
			defer b.Close()
			call := func(name string, args string) string {
				t.Helper()
				out, err := agent.NewRegistry(list...).Run(ctx, "mcp__browser__"+name, json.RawMessage(args))
				if err != nil {
					t.Fatal(name, out, err)
				}
				return out
			}
			if pass == 0 {
				call("browser_navigate", `{"url":"`+server.URL+`/login"}`)
				call("browser_evaluate", `{"function":"() => { localStorage.setItem('account', 'fixture-account'); return 'saved'; }"}`)
			} else {
				call("browser_navigate", `{"url":"`+server.URL+`/"}`)
				out := call("browser_evaluate", `{"function":"() => document.body.innerText"}`)
				if !strings.Contains(out, "Signed in") {
					t.Fatal("login cookie lost after restart", out)
				}
				out = call("browser_evaluate", `{"function":"() => localStorage.getItem('account')"}`)
				if !strings.Contains(out, "fixture-account") {
					t.Fatal("local storage lost after restart", out)
				}
			}
		}()
	}
}
