package ssh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/keshon/gektor/internal/credentials"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var profileNames = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Profile contains references only. Secrets are resolved when a call connects.
type Profile struct {
	Host          string `json:"host"`
	Port          int    `json:"port,omitempty"`
	User          string `json:"user"`
	KnownHosts    string `json:"known_hosts"`
	PrivateKey    string `json:"private_key,omitempty"`
	PasswordEnv   string `json:"password_env,omitempty"`
	PassphraseEnv string `json:"passphrase_env,omitempty"`
}

func loadProfiles(filename string) (map[string]Profile, error) {
	if !filepath.IsAbs(filename) {
		return nil, fmt.Errorf("SSH profiles require an absolute config path")
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("SSH profiles unavailable: %w", err)
	}
	defer f.Close() // Read-only config; closing cannot affect decoded profiles.

	content, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return nil, err
	}
	if len(content) > 64<<10 {
		return nil, fmt.Errorf("SSH profiles exceed 64 KiB")
	}
	var profiles map[string]Profile
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&profiles)
	if err != nil {
		return nil, fmt.Errorf("invalid SSH profiles: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("invalid SSH profiles trailing content")
	}
	for name, profile := range profiles {
		if !profileNames.MatchString(name) {
			return nil, fmt.Errorf("invalid SSH profile name")
		}
		if profile.Port == 0 {
			profile.Port = 22
		}
		invalidHost := profile.Host == "" || strings.ContainsAny(profile.Host, " /\\\t\r\n\x00@[]")
		if strings.Contains(profile.Host, ":") && net.ParseIP(profile.Host) == nil {
			invalidHost = true
		}
		if invalidHost || profile.Port < 1 || profile.Port > 65535 || profile.User == "" || strings.ContainsAny(profile.User, " /\\\t\r\n\x00@:") {
			return nil, fmt.Errorf("invalid SSH endpoint for %s", name)
		}
		if !filepath.IsAbs(profile.KnownHosts) {
			return nil, fmt.Errorf("SSH profile %s requires an absolute known_hosts path", name)
		}
		if (profile.PrivateKey == "") == (profile.PasswordEnv == "") {
			return nil, fmt.Errorf("SSH profile %s requires either private_key or password_env", name)
		}
		if profile.PrivateKey != "" && !filepath.IsAbs(profile.PrivateKey) {
			return nil, fmt.Errorf("SSH profile %s requires an absolute private_key path", name)
		}
		invalidPassword := profile.PasswordEnv != "" && !credentials.ValidEnvironment(profile.PasswordEnv)
		invalidPassphrase := profile.PassphraseEnv != "" && (!credentials.ValidEnvironment(profile.PassphraseEnv) || profile.PrivateKey == "")
		if invalidPassword || invalidPassphrase {
			return nil, fmt.Errorf("invalid SSH credential reference for %s", name)
		}
		profiles[name] = profile
	}
	return profiles, nil
}

func (p Profile) address() string { return net.JoinHostPort(p.Host, strconv.Itoa(p.Port)) }

func (p Profile) clientConfig() (*ssh.ClientConfig, []string, error) {
	verify, err := knownhosts.New(p.KnownHosts)
	if err != nil {
		return nil, nil, fmt.Errorf("load SSH known_hosts: %w", err)
	}
	var auth ssh.AuthMethod
	var secrets []string
	if p.PasswordEnv != "" {
		password, resolveErr := credentials.Environment(p.PasswordEnv)
		if resolveErr != nil {
			return nil, nil, resolveErr
		}
		secrets = append(secrets, password)
		auth = ssh.Password(password)
	} else {
		key, readErr := os.ReadFile(p.PrivateKey)
		if readErr != nil {
			return nil, nil, fmt.Errorf("read SSH private key: %w", readErr)
		}
		var signer ssh.Signer
		if p.PassphraseEnv != "" {
			passphrase, resolveErr := credentials.Environment(p.PassphraseEnv)
			if resolveErr != nil {
				return nil, nil, resolveErr
			}
			secrets = append(secrets, passphrase)
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("parse SSH private key")
		}
		auth = ssh.PublicKeys(signer)
	}
	return &ssh.ClientConfig{User: p.User, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: verify}, secrets, nil
}
