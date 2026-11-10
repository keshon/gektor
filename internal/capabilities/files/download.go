package files

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/keshon/gektor/internal/httptransport"
)

type DownloadRequest struct {
	URL            string `json:"url"`
	Path           string `json:"path"`
	MaxBytes       int64  `json:"max_bytes,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type DownloadResult struct {
	Path      string `json:"path"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type"`
}

func (s Service) ValidateDownload(in DownloadRequest) (DownloadRequest, error) {
	var err error
	in.Path, err = s.path(in.Path)
	if err != nil {
		return in, err
	}
	in.URL, err = httptransport.Normalize(in.URL, false)
	if err != nil {
		return in, err
	}
	// Fragments are never sent over HTTP; they must not change the resource
	// spelling used by permission rules for the same network request.
	u, err := url.Parse(in.URL)
	if err != nil {
		return in, err
	}
	u.Fragment, u.RawFragment = "", ""
	in.URL = u.String()
	if in.MaxBytes == 0 {
		in.MaxBytes = MaxFileBytes
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 30
	}
	if in.MaxBytes < 1 || in.MaxBytes > MaxFileBytes {
		return in, fmt.Errorf("max_bytes must be 1–%d", MaxFileBytes)
	}
	if in.TimeoutSeconds < 1 || in.TimeoutSeconds > 120 {
		return in, fmt.Errorf("timeout_seconds must be 1–120")
	}
	return in, nil
}

func (s Service) Download(ctx context.Context, in DownloadRequest) (result DownloadResult, err error) {
	in, err = s.ValidateDownload(in)
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if _, e := s.WS.Stat(in.Path); e == nil {
		return result, fmt.Errorf("destination already exists")
	} else if !errors.Is(e, os.ErrNotExist) {
		return result, e
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutSeconds)*time.Second)
	defer cancel()
	client := httptransport.Client(false, s.transport)
	client.Timeout = time.Duration(in.TimeoutSeconds) * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("download: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, fmt.Errorf("HTTP status %d; redirects and retries are disabled", response.StatusCode)
	}
	if response.ContentLength > in.MaxBytes {
		return result, fmt.Errorf("download exceeds %d bytes", in.MaxBytes)
	}
	f, err := s.WS.CreateExclusive(in.Path, 0600)
	if err != nil {
		return result, err
	}
	complete := false
	defer func() {
		_ = f.Close()
		if !complete {
			_ = s.WS.Remove(in.Path)
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(contextReader{ctx, response.Body}, in.MaxBytes+1))
	if err != nil {
		return result, fmt.Errorf("download body: %w", err)
	}
	if n > in.MaxBytes {
		return result, fmt.Errorf("download exceeds %d bytes", in.MaxBytes)
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = f.Sync(); err != nil {
		return result, err
	}
	if err = f.Close(); err != nil {
		return result, err
	}
	complete = true
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if len(mediaType) > 128 || mediaType == "" {
		mediaType = "application/octet-stream"
	}
	return DownloadResult{Path: in.Path, URL: in.URL, Size: n, SHA256: fmt.Sprintf("%x", hash.Sum(nil)), MediaType: mediaType}, nil
}
