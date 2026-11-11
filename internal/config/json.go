package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const maxDocumentBytes = 1 << 20

// decodeDocument checks the entire bounded input, not just a valid JSON prefix.
// The reader stays caller-owned; destination may contain partially decoded values on error.
func decodeDocument(reader io.Reader, destination any, label string) error {
	limited := io.LimitReader(reader, maxDocumentBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(data) > maxDocumentBytes {
		return fmt.Errorf("%s: exceeds 1 MiB", label)
	}

	source := bytes.NewReader(data)
	decoder := json.NewDecoder(source)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(destination)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}

	var extra any
	err = decoder.Decode(&extra)
	if err != io.EOF {
		return fmt.Errorf("%s: expected one JSON document", label)
	}

	return nil
}
