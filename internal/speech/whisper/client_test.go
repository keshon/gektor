package whisper

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLongRecordingIsUploadedWithoutTruncation(t *testing.T) {
	recording := make([]byte, 44+16000*2*40)
	copy(recording, "opening")
	copy(recording[len(recording)-6:], "ending")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(5_000_000); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		got, err := io.ReadAll(file)
		if err != nil || !bytes.Equal(got, recording) {
			t.Error("recording truncated")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"language":"english","text":"Opening words and ending words."}`)
	}))
	defer server.Close()
	got, err := NewEnglish(server.URL).Recognize(context.Background(), recording)
	if err != nil || got.Text != "Opening words and ending words." {
		t.Fatal(got, err)
	}
}
