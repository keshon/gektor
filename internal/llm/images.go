package llm

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// MaxImageBytes caps one attached image at 20MB: vision turns already
// spend thousands of context tokens per picture, and anything bigger
// is a video frame or a mistake, not a screenshot.
const MaxImageBytes = 20 << 20

// imageEstimateTokens is what one attached picture costs when the backend
// does not report usage. A flat figure, because the real cost depends on the
// model's vision encoder and the resize the server applies, neither of which
// is knowable here. Undershooting would let an image-heavy turn overrun the
// window the meter says it has; overshooting only compacts a little early.
const imageEstimateTokens = 1024

// imageMIME maps accepted extensions to wire MIME types. Deliberately
// narrow: servers agree on these, and an exotic codec failing server-
// side looks exactly like model blindness.
var imageMIME = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".bmp":  "image/bmp",
}

// IsImagePath reports whether path names a sendable image by
// extension. The wire encoder re-checks existence and size.
func IsImagePath(path string) bool {
	_, ok := imageMIME[strings.ToLower(filepath.Ext(path))]
	return ok
}

// ValidateImage reports whether path is a picture the wire encoder can
// send: a supported extension, a regular file, and within MaxImageBytes.
// Tools use it before naming an attachment so the model gets the reason
// now instead of the whole turn failing at send time.
func ValidateImage(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := imageMIME[ext]; !ok {
		return fmt.Errorf("image %q: format %q not supported (png, jpg, webp, gif, bmp)", path, ext)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("image %q: %w", path, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("image %q: is a directory", path)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("image %q: is not a regular file", path)
	}
	if fi.Size() > MaxImageBytes {
		return fmt.Errorf("image %q: %d bytes over the %d-byte cap", path, fi.Size(), MaxImageBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	header := make([]byte, 512)
	n, err := f.Read(header)
	if err != nil && err != io.EOF {
		return err
	}
	if mime := http.DetectContentType(header[:n]); mime != imageMIME[ext] {
		return fmt.Errorf("image %q: content type %s does not match %s", path, mime, imageMIME[ext])
	}
	return nil
}

// SnapshotImage freezes a tool image before it enters history. A subsequent
// screenshot at the same source path must not change earlier model inputs.
func SnapshotImage(path, dir string) (string, error) {
	if err := ValidateImage(path); err != nil {
		return "", err
	}
	source, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer source.Close()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".image-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(source, MaxImageBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if n > MaxImageBytes {
		return "", fmt.Errorf("image grew beyond the %d-byte cap", MaxImageBytes)
	}
	target := filepath.Join(dir, fmt.Sprintf("%x%s", hash.Sum(nil), strings.ToLower(filepath.Ext(path))))
	if err := os.Rename(f.Name(), target); err != nil {
		// Another concurrent read may have stored these identical bytes.
		if _, statErr := os.Stat(target); statErr != nil {
			return "", err
		}
	}
	if err := ValidateImage(target); err != nil {
		return "", err
	}
	return target, nil
}

// encodeImageFile reads an absolute image path into a data URL. Paths
// arrive resolved (workspace escape already checked at attach time);
// this checks existence, size, and format only.
func encodeImageFile(path string) (string, error) {
	if err := ValidateImage(path); err != nil {
		return "", err
	}
	mime := imageMIME[strings.ToLower(filepath.Ext(path))]
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxImageBytes+1))
	if err != nil {
		return "", fmt.Errorf("image %q: %w", path, err)
	}
	if len(raw) > MaxImageBytes {
		return "", fmt.Errorf("image %q grew beyond the byte cap", path)
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

// contentPart is one OpenAI content part: text or an image URL.
type contentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

// encodeContent builds a message's wire content: a plain string when
// imageless (byte-identical to the old shape — text models, text
// backends, and golden tests never see a difference), a multipart
// array otherwise. allowImages is false on backends that cannot
// process pictures: attaching there is a loud error, never a silent
// drop the model then hallucinates around.
func encodeContent(text string, images []string, allowImages bool) (json.RawMessage, error) {
	if len(images) == 0 {
		raw, err := json.Marshal(text)
		if err != nil {
			return nil, err
		}
		return raw, nil
	}
	if !allowImages {
		return nil, fmt.Errorf("images attached but this backend cannot process images — use a vision-capable llama-server backend with --mmproj")
	}
	parts := []contentPart{{Type: "text", Text: text}}
	for _, path := range images {
		url, err := encodeImageFile(path)
		if err != nil {
			return nil, err
		}
		parts = append(parts, contentPart{
			Type: "image_url",
			ImageURL: &struct {
				URL string `json:"url"`
			}{URL: url},
		})
	}
	raw, err := json.Marshal(parts)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// encodeMessages converts history to the wire shape, shared by Chat
// and Stream so both paths attach (or refuse) identically.
func encodeMessages(messages []Message, allowImages bool) ([]wireMessage, error) {
	out := make([]wireMessage, 0, len(messages))
	for _, m := range messages {
		content, err := encodeContent(m.Content, m.Images, allowImages)
		if err != nil {
			return nil, err
		}
		wm := wireMessage{Role: string(m.Role), Content: content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			// Per the OpenAI tool-call wire format, function.arguments is a
			// JSON-encoded *string*, not a raw object — encodeArguments
			// re-wraps our internal object form before it goes back out.
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: wireFunction{
					Name:      tc.Name,
					Arguments: encodeArguments(tc.Arguments),
				},
			})
		}
		out = append(out, wm)
	}
	return out, nil
}
