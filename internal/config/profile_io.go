package config

import (
	"io"
	"os"
)

// Load reads a profile, rejecting unknown fields and invalid settings.
func Load(path string) (Profile, error) {
	file, err := os.Open(path)
	if err != nil {
		return Profile{}, err
	}

	// A read-only close cannot change the decoded profile.
	defer file.Close()

	return Decode(file)
}

// Decode reads one strict profile of at most 1 MiB from a caller-owned reader.
func Decode(reader io.Reader) (Profile, error) {
	var profile Profile
	err := decodeDocument(reader, &profile, "profile")
	if err != nil {
		return profile, err
	}

	err = profile.Validate()
	return profile, err
}
