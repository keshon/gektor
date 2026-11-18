// Package credentials owns named HTTP bindings; shared credentials own secret storage.
package credentials

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	secrets "github.com/keshon/gektor/internal/credentials"
)

var names = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type Config struct {
	Origin    string `json:"origin"`
	Env       string `json:"env,omitempty"`
	Reference string `json:"reference,omitempty"`
	Header    string `json:"header"`
	Prefix    string `json:"prefix,omitempty"`
}
type Store struct{ Directory string }

type storedSecret struct {
	Kind   string `json:"kind"`
	Origin string `json:"origin"`
	Header string `json:"header"`
	Value  string `json:"value"`
}

func Origin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("credential origin must be HTTPS without userinfo")
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
func validate(c Config) (Config, error) {
	origin, err := Origin(c.Origin)
	if err != nil {
		return c, err
	}

	if strings.TrimSuffix(c.Origin, "/") != origin {
		return c, fmt.Errorf("use a canonical HTTPS origin without path or query")
	}

	c.Origin = origin
	source := secrets.Source{Env: c.Env, Reference: c.Reference}
	err = source.Validate()
	if err != nil {
		return c, err
	}

	if c.Header != "Authorization" && c.Header != "X-API-Key" {
		return c, fmt.Errorf("credential header must be Authorization or X-API-Key")
	}

	if len(c.Prefix) > 64 || strings.ContainsAny(c.Prefix, "\r\n") {
		return c, fmt.Errorf("invalid credential prefix")
	}

	return c, nil
}

// Binding exposes non-secret metadata. Revision fingerprints the binding, not its secret.
type Binding struct {
	Name       string `json:"name"`
	Revision   string `json:"revision"`
	Origin     string `json:"origin"`
	Header     string `json:"header"`
	Prefix     string `json:"prefix,omitempty"`
	Source     string `json:"source"`
	Revoked    bool   `json:"revoked"`
	Protection string `json:"protection"`
}

func (s Store) Register(name string, c Config) error {
	if !names.MatchString(name) {
		return fmt.Errorf("invalid credential name")
	}
	c, err := validate(c)
	if err != nil {
		return err
	}

	err = os.MkdirAll(s.Directory, 0700)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(s.Directory)
	if err != nil {
		return err
	}
	defer root.Close()

	revocations := secrets.Revocations{Directory: filepath.Join(s.Directory, "http-revoked")}
	revoked, err := revocations.Revoked(name)
	if err != nil {
		return err
	}
	if revoked {
		return fmt.Errorf("credential name is revoked")
	}

	file, err := root.OpenFile(name+".json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(file).Encode(c)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	err = errors.Join(err, closeErr)
	if err != nil {
		removeErr := root.Remove(name + ".json")
		return errors.Join(err, removeErr)
	}
	return nil
}

// RegisterStored keeps the secret out of the binding file and removes it on registration failure.
func (s Store) RegisterStored(name string, c Config, value []byte) error {
	if c.Env != "" || c.Reference != "" {
		return fmt.Errorf("stored input cannot combine with a secret reference")
	}
	if len(value) == 0 || len(value) > 8192 || strings.ContainsAny(string(value), "\r\n") {
		return fmt.Errorf("invalid HTTP credential value")
	}

	origin, err := Origin(c.Origin)
	if err != nil {
		return err
	}
	entry := storedSecret{Kind: "http-header", Origin: origin, Header: c.Header, Value: string(value)}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	protected := secrets.Store{Directory: s.Directory}
	reference, err := protected.Put(raw)
	if err != nil {
		return err
	}
	c.Reference = reference
	err = s.Register(name, c)
	if err != nil {
		deleteErr := protected.Delete(reference)
		return errors.Join(err, deleteErr)
	}
	return nil
}

func (s Store) read(name string) (Config, bool, error) {
	var config Config
	if !names.MatchString(name) {
		return config, false, fmt.Errorf("invalid credential name")
	}
	root, err := os.OpenRoot(s.Directory)
	if err != nil {
		return config, false, fmt.Errorf("credential store unavailable")
	}
	defer root.Close()
	file, err := root.Open(name + ".json")
	if err != nil {
		return config, false, fmt.Errorf("credential %s unavailable", name)
	}
	defer file.Close() // The file is read-only; closing cannot change the binding.

	raw, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil || len(raw) > 8192 {
		return config, false, fmt.Errorf("invalid credential configuration")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&config)
	if err != nil {
		return config, false, fmt.Errorf("invalid credential configuration")
	}
	err = decoder.Decode(new(any))
	if err != io.EOF {
		return config, false, fmt.Errorf("invalid credential configuration")
	}
	config, err = validate(config)
	if err != nil {
		return config, false, err
	}

	revocations := secrets.Revocations{Directory: filepath.Join(s.Directory, "http-revoked")}
	revoked, err := revocations.Revoked(name)
	if err != nil {
		return config, false, fmt.Errorf("credential revocation unavailable")
	}
	return config, revoked, nil
}

func describe(name string, config Config, revoked bool) (Binding, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return Binding{}, err
	}

	binding := Binding{
		Name: name, Revision: fmt.Sprintf("%x", sha256.Sum256(data)),
		Origin: config.Origin, Header: config.Header, Prefix: config.Prefix,
		Source: "environment", Revoked: revoked, Protection: "Environment of the executing process",
	}
	if config.Reference != "" {
		binding.Source = "stored"
		binding.Protection = secrets.Protection()
	}
	return binding, nil
}

func (s Store) Inspect(name string) (Binding, error) {
	config, revoked, err := s.read(name)
	if err != nil {
		return Binding{}, err
	}
	return describe(name, config, revoked)
}

// Revoke is permanent for this name. Already-resolved requests can still finish.
func (s Store) Revoke(name string) error {
	_, _, err := s.read(name)
	if err != nil {
		return err
	}
	revocations := secrets.Revocations{Directory: filepath.Join(s.Directory, "http-revoked")}
	return revocations.Revoke(name)
}

func (s Store) Resolve(name, origin, revision string) (string, string, string, error) {
	config, revoked, err := s.read(name)
	if err != nil {
		return "", "", "", err
	}
	binding, err := describe(name, config, revoked)
	if err != nil {
		return "", "", "", err
	}
	if revoked {
		return "", "", "", fmt.Errorf("credential %s is revoked", name)
	}
	if revision == "" || revision != binding.Revision {
		return "", "", "", fmt.Errorf("credential binding revision mismatch")
	}

	actual, err := Origin(origin)
	if err != nil || actual != config.Origin {
		return "", "", "", fmt.Errorf("credential origin mismatch")
	}

	source := secrets.Source{Env: config.Env, Reference: config.Reference}
	protected := secrets.Store{Directory: s.Directory}
	raw, err := source.Resolve(protected)
	if err != nil {
		return "", "", "", fmt.Errorf("credential value missing or invalid")
	}
	secret := string(raw)
	if config.Reference != "" {
		var entry storedSecret
		err = json.Unmarshal(raw, &entry)
		if err != nil || entry.Kind != "http-header" || entry.Origin != config.Origin || entry.Header != config.Header {
			return "", "", "", fmt.Errorf("stored HTTP credential binding mismatch")
		}
		secret = entry.Value
	}
	if secret == "" || len(secret) > 8192 || strings.ContainsAny(secret, "\r\n") {
		return "", "", "", fmt.Errorf("credential value missing or invalid")
	}

	current, err := s.Inspect(name)
	if err != nil {
		return "", "", "", err
	}
	if current.Revoked || current.Revision != revision {
		return "", "", "", fmt.Errorf("credential binding changed or revoked")
	}

	return config.Header, config.Prefix + secret, secret, nil
}
