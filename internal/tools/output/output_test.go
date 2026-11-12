package output

import (
	"os"
	"strings"
	"testing"
)

func TestOutputFileLimitIsExplicit(t *testing.T) {
	store := &Store{dir: t.TempDir()}
	file, err := store.Create()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte(strings.Repeat("x", outputFileLimit+100)))
	note := file.Finish()
	if !strings.Contains(note, "later bytes were not saved") {
		t.Fatal(note)
	}
	info, err := os.Stat(file.file.Name())
	if err != nil || info.Size() != outputFileLimit {
		t.Fatal(info, err)
	}
}
