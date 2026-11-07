package llm

import (
	"crypto/sha256"
	"encoding/base64"
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

// ImageEstimateTokens is what one attached picture costs when the backend
// does not report usage. A flat figure, because the real cost depends on the
// model's vision encoder and the resize the server applies, neither of which
// is knowable here. Undershooting would let an image-heavy turn overrun the
// window the meter says it has; overshooting only compacts a little early.
const ImageEstimateTokens = 1024

// Accepted image extensions. The wire MIME is detected from the actual bytes.
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
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("image %q: %w", path, err)
	}
	defer f.Close()
	return ValidateImageFile(f, path)
}

// ValidateImageFile validates an already-opened source without reopening its path.
func ValidateImageFile(f *os.File, path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := imageMIME[ext]; !ok {
		return fmt.Errorf("image %q: format %q not supported (png, jpg, webp, gif, bmp)", path, ext)
	}
	fi, err := f.Stat()
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
	header := make([]byte, 512)
	n, err := f.ReadAt(header, 0)
	if err != nil && err != io.EOF {
		return err
	}
	mime := http.DetectContentType(header[:n])
	if ImageExtension(mime) == "" {
		return fmt.Errorf("image %q: unsupported content type %s", path, mime)
	}
	return nil
}

// ImageMIME reads the actual content type, regardless of a misleading extension.
func ImageMIME(f *os.File) (string, error) {
	header := make([]byte, 512)
	n, err := f.ReadAt(header, 0)
	if err != nil && err != io.EOF {
		return "", err
	}
	mime := http.DetectContentType(header[:n])
	if ImageExtension(mime) == "" {
		return "", fmt.Errorf("unsupported image content type %s", mime)
	}
	return mime, nil
}

func ImageExtension(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	}
	return ""
}

// SnapshotImage freezes a tool image before it enters history. A subsequent
// screenshot at the same source path must not change earlier model inputs.
func SnapshotImage(path, dir string) (string, error) {
	source, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer source.Close()
	return SnapshotImageFile(source, path, dir)
}

// SnapshotImageFile stores bytes from the confined file handle into the trusted
// application cache. The detected format determines the cache extension.
func SnapshotImageFile(source *os.File, path, dir string) (string, error) {
	if err := ValidateImageFile(source, path); err != nil {
		return "", err
	}
	mime, err := ImageMIME(source)
	if err != nil {
		return "", err
	}
	path = strings.TrimSuffix(path, filepath.Ext(path)) + ImageExtension(mime)
	if _, err := source.Seek(0, 0); err != nil {
		return "", err
	}
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
	targetDir := filepath.Join(dir, fmt.Sprintf("%x", hash.Sum(nil)))
	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return "", err
	}
	target := filepath.Join(targetDir, filepath.Base(path))
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

// EncodeImageFile reads an absolute image path into a data URL. Paths
// arrive resolved (workspace escape already checked at attach time);
// this checks existence, size, and format only.
func EncodeImageFile(path string) (string, error) {
	if err := ValidateImage(path); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	mime, err := ImageMIME(f)
	if err != nil {
		return "", err
	}
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
