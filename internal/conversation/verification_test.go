package conversation

import (
	"path/filepath"
	"testing"
)

func TestLegacyVerificationRemainsUnknownAfterSave(t *testing.T) {
	snapshot, err := Decode([]byte(`{"version":1,"history":[{"role":"system","content":"system"},{"role":"user","content":"task"}],"runs":[{"start":1,"end":2,"outcome":"completed","steps":1,"verification":"PASSED old text"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := snapshot.Save(path); err != nil {
		t.Fatal(err)
	}
	saved, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Runs[0].Verification != "PASSED old text" || saved.Runs[0].VerificationReceipt != nil {
		t.Fatalf("legacy text became a structured verdict: %+v", saved.Runs[0])
	}
}
