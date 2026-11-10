package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Reject duplicates before schema validation: encoding/json otherwise keeps the
// last value, allowing a contradictory decision to pass based on key order.
func validateAnswerJSON(answer string) error {
	d := json.NewDecoder(strings.NewReader(answer))
	d.UseNumber()
	var value func() error
	value = func() error {
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		keys := map[string]bool{}
		for d.More() {
			if delim == '{' {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || keys[name] {
					return fmt.Errorf("invalid or duplicate answer key %q", key)
				}
				keys[name] = true
			}
			if err := value(); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("answer must contain one complete JSON value")
	}
	return nil
}
