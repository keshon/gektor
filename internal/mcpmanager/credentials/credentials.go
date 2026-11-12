// Package credentials stores origin/header-bound MCP secrets separately from
// application configuration. MCP management and capability configuration share
// this store; workflow environment references and provider accounts are separate.
package credentials

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	secrets "github.com/keshon/gektor/internal/credentials"
)

type Store struct{ Directory string }
type entry struct{ Origin, Header, Value string }

func Origin(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("authenticated MCP requires HTTPS without userinfo")
	}

	host := strings.ToLower(u.Host)
	if u.Port() == "443" {
		host = strings.ToLower(u.Hostname())
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	}

	return "https://" + host, nil
}
func (s Store) Put(endpoint, header, value string) (string, error) {
	origin, e := Origin(endpoint)
	if e != nil {
		return "", e
	}

	if value == "" || len(value) > 16384 || strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("invalid credential value")
	}

	raw, e := json.Marshal(entry{origin, header, value})
	if e != nil {
		return "", e
	}

	store := secrets.Store{Directory: s.Directory}
	return store.Put(raw)
}
func (s Store) Resolve(id, endpoint, header string) (string, error) {
	if !secrets.ValidReference(id) {
		return "", fmt.Errorf("invalid credential reference")
	}

	origin, e := Origin(endpoint)
	if e != nil {
		return "", e
	}

	store := secrets.Store{Directory: s.Directory}
	raw, e := store.Resolve(id)
	if e != nil {
		return "", e
	}

	var v entry
	if json.Unmarshal(raw, &v) != nil || v.Origin != origin || !strings.EqualFold(v.Header, header) {
		return "", fmt.Errorf("credential endpoint or header mismatch")
	}

	return v.Value, nil
}
func (s Store) Delete(id string) error {
	store := secrets.Store{Directory: s.Directory}
	return store.Delete(id)
}
