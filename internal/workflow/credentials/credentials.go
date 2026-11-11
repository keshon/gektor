// Package credentials stores named, origin-bound references to environment secrets.
package credentials

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var names = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
var environments = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Config struct {
	Origin string `json:"origin"`
	Env    string `json:"env"`
	Header string `json:"header"`
	Prefix string `json:"prefix,omitempty"`
}
type Store struct{ Directory string }

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
	if !environments.MatchString(c.Env) {
		return c, fmt.Errorf("invalid credential environment name")
	}

	if c.Header != "Authorization" && c.Header != "X-API-Key" {
		return c, fmt.Errorf("credential header must be Authorization or X-API-Key")
	}

	if len(c.Prefix) > 64 || strings.ContainsAny(c.Prefix, "\r\n") {
		return c, fmt.Errorf("invalid credential prefix")
	}

	return c, nil
}
func (s Store) Register(name string, c Config) error {
	if !names.MatchString(name) {
		return fmt.Errorf("invalid credential name")
	}

	c, err := validate(c)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(s.Directory, 0700); err != nil {
		return err
	}

	root, err := os.OpenRoot(s.Directory)
	if err != nil {
		return err
	}

	defer root.Close()

	f, err := root.OpenFile(name+".json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}

	defer f.Close()

	if err := json.NewEncoder(f).Encode(c); err != nil {
		return err
	}

	return f.Sync()
}
func (s Store) Resolve(name, origin string) (header, value, secret string, err error) {
	if !names.MatchString(name) {
		return "", "", "", fmt.Errorf("invalid credential name")
	}

	root, e := os.OpenRoot(s.Directory)
	if e != nil {
		return "", "", "", fmt.Errorf("credential store unavailable")
	}

	defer root.Close()

	f, e := root.Open(name + ".json")
	if e != nil {
		return "", "", "", fmt.Errorf("credential %s unavailable", name)
	}

	defer f.Close()

	var c Config
	d := json.NewDecoder(io.LimitReader(f, 8193))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return "", "", "", fmt.Errorf("invalid credential configuration")
	}

	if d.Decode(new(any)) != io.EOF {
		return "", "", "", fmt.Errorf("invalid credential configuration")
	}

	c, e = validate(c)
	if e != nil {
		return "", "", "", e
	}

	actual, e := Origin(origin)
	if e != nil || actual != c.Origin {
		return "", "", "", fmt.Errorf("credential origin mismatch")
	}

	secret, ok := os.LookupEnv(c.Env)
	if !ok || secret == "" || len(secret) > 8192 || strings.ContainsAny(secret, "\r\n") {
		return "", "", "", fmt.Errorf("credential environment value missing or invalid")
	}

	return c.Header, c.Prefix + secret, secret, nil
}
