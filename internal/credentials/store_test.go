package credentials

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

func TestSourceResolvesLiveValuesAndRejectsAmbiguity(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	reference, err := store.Put([]byte("stored-value"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GEKTOR_SOURCE_TEST", "first-value")
	environment := Source{Env: "GEKTOR_SOURCE_TEST"}
	raw, err := environment.Resolve(store)
	if err != nil || string(raw) != "first-value" {
		t.Fatal("environment source unavailable", err)
	}
	t.Setenv("GEKTOR_SOURCE_TEST", "second-value")
	raw, err = environment.Resolve(store)
	if err != nil || string(raw) != "second-value" {
		t.Fatal("environment source was cached", err)
	}
	stored := Source{Reference: reference}
	raw, err = stored.Resolve(store)
	if err != nil || string(raw) != "stored-value" {
		t.Fatal("stored source unavailable", err)
	}
	for _, source := range []Source{{}, {Env: "GEKTOR_SOURCE_TEST", Reference: reference}, {Env: "bad name"}, {Reference: "../outside"}} {
		_, err = source.Resolve(store)
		if err == nil {
			t.Fatal("invalid source accepted")
		}
	}
}

func TestRevocationPersistsAcrossStoresAndDeletion(t *testing.T) {
	directory := t.TempDir()
	store := Store{Directory: directory}
	id, err := store.Put([]byte("private-revoked-value"))
	if err != nil {
		t.Fatal(err)
	}
	err = store.Revoke(id)
	if err != nil {
		t.Fatal(err)
	}
	reopened := Store{Directory: directory}
	err = reopened.Revoke(id)
	if err != nil {
		t.Fatal("repeated revocation", err)
	}
	_, err = reopened.Resolve(id)
	if err == nil || strings.Contains(err.Error(), "private-revoked-value") {
		t.Fatal("revoked secret resolved or leaked", err)
	}
	err = reopened.Delete(id)
	if err != nil {
		t.Fatal(err)
	}
	revocations := Revocations{Directory: filepath.Join(directory, "revoked")}
	revoked, err := revocations.Revoked(id)
	if err != nil || !revoked {
		t.Fatal("deletion removed revocation", err)
	}
	for _, bad := range []string{"../outside", "", "a.b"} {
		err = revocations.Revoke(bad)
		if err == nil {
			t.Fatal("invalid revocation name accepted")
		}
	}
}
