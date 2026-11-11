// Package credentials stores origin/header-bound MCP secrets separately from
// application configuration. MCP management and capability configuration share
// this store; workflow environment references and provider accounts are separate.
package credentials

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Store struct{ Directory string }
type entry struct{ Origin, Header, Value string }

var validID = regexp.MustCompile(`^[a-f0-9]{32}$`)

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
	raw, e = protect(raw)
	if e != nil {
		return "", e
	}
	if e = os.MkdirAll(s.Directory, 0700); e != nil {
		return "", e
	}
	var id [16]byte
	if _, e = rand.Read(id[:]); e != nil {
		return "", e
	}
	name := hex.EncodeToString(id[:])
	f, e := os.OpenFile(filepath.Join(s.Directory, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return "", e
	}
	_, e = f.Write(raw)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		os.Remove(filepath.Join(s.Directory, name))
		return "", e
	}
	return name, nil
}
func (s Store) Resolve(id, endpoint, header string) (string, error) {
	if !validID.MatchString(id) {
		return "", fmt.Errorf("invalid credential reference")
	}
	origin, e := Origin(endpoint)
	if e != nil {
		return "", e
	}
	raw, e := os.ReadFile(filepath.Join(s.Directory, id))
	if e != nil {
		return "", fmt.Errorf("credential unavailable")
	}
	raw, e = unprotect(raw)
	if e != nil {
		return "", fmt.Errorf("cannot unlock credential")
	}
	var v entry
	if json.Unmarshal(raw, &v) != nil || v.Origin != origin || !strings.EqualFold(v.Header, header) {
		return "", fmt.Errorf("credential endpoint or header mismatch")
	}
	return v.Value, nil
}
func (s Store) Delete(id string) error {
	if !validID.MatchString(id) {
		return fmt.Errorf("invalid credential reference")
	}
	e := os.Remove(filepath.Join(s.Directory, id))
	if os.IsNotExist(e) {
		return nil
	}
	return e
}
