package credentials

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreProtectsIndependentValuesAndRejectsInvalidReferences(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	first, err := store.Put([]byte("first-private-credential-value"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put([]byte("second-private-credential-value"))
	if err != nil || first == second || !ValidReference(first) {
		t.Fatal("independent references missing", err)
	}
	raw, err := os.ReadFile(filepath.Join(store.Directory, first))
	if err != nil {
		t.Fatal(err)
	}
	if Encrypted() && bytes.Contains(raw, []byte("first-private-credential-value")) {
		t.Fatal("credential persisted in plaintext")
	}
	value, err := store.Resolve(first)
	if err != nil || string(value) != "first-private-credential-value" {
		t.Fatal("round trip failed", err)
	}
	_, err = store.Resolve("../outside")
	if err == nil {
		t.Fatal("invalid reference accepted")
	}
	err = store.Delete(first)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Resolve(first)
	if err == nil {
		t.Fatal("deleted reference resolved")
	}
	value, err = store.Resolve(second)
	if err != nil || string(value) != "second-private-credential-value" {
		t.Fatal("deleting one value damaged another", err)
	}
}
