package ssh

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/credentials"
)

func profileRevision(profile Profile) (string, error) {
	raw, err := json.Marshal(profile)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func connectionMetadata(path, name string, profile Profile) (Connection, error) {
	revision, err := profileRevision(profile)
	if err != nil {
		return Connection{}, err
	}
	revocations := credentials.Revocations{Directory: path + ".revoked"}
	revoked, err := revocations.Revoked(name)
	if err != nil {
		return Connection{}, err
	}
	return Connection{
		Name:     name,
		Host:     profile.Host,
		Port:     profile.Port,
		User:     profile.User,
		Revision: revision,
		Revoked:  revoked,
	}, nil
}

func (s Profiles) Inspect(name string) (Connection, error) {
	profiles, err := s.Load()
	if err != nil {
		return Connection{}, err
	}
	profile, exists := profiles[name]
	if !exists {
		return Connection{}, fmt.Errorf("SSH connection is not registered")
	}
	return connectionMetadata(s.Path, name, profile)
}

// Revoke permanently disables this name, including calls from existing services.
func (s Profiles) Revoke(name string) error {
	_, err := s.Inspect(name)
	if err != nil {
		return err
	}
	revocations := credentials.Revocations{Directory: s.Path + ".revoked"}
	return revocations.Revoke(name)
}

func (s *Service) binding(name, revision string) (Profile, error) {
	profile, err := s.profile(name)
	if err != nil {
		return Profile{}, err
	}
	actual, err := profileRevision(profile)
	if err != nil {
		return Profile{}, err
	}
	if revision == "" || revision != actual {
		return Profile{}, fmt.Errorf("SSH connection revision mismatch; inspect the connection before retrying")
	}
	return profile, nil
}
