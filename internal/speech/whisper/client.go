package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/keshon/tars/internal/speech"
)

var ErrNoSpeech = errors.New("ASR returned no speech")

type Client struct{ address, language, beam string }

func New(address string) *Client { return &Client{address: address, language: "auto", beam: "5"} }

// NewEnglish uses the measured greedy policy of the English Kokoro composition.
func NewEnglish(address string) *Client { return &Client{address: address, language: "en", beam: "1"} }
func (c *Client) Recognize(ctx context.Context, wav []byte) (speech.Transcript, error) {
	baseURL := c.address
	var tx speech.Transcript
	if len(wav) > speech.MaxInputBytes {
		return tx, fmt.Errorf("audio input exceeds limit")
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"language": c.language, "response_format": "verbose_json", "no_language_probabilities": "true",
		"no_timestamps": "true", "no_context": "true", "translate": "false", "beam_size": c.beam, "best_of": c.beam,
	} {
		if err := form.WriteField(name, value); err != nil {
			return tx, err
		}
	}
	part, err := form.CreateFormFile("file", "input.wav")
	if err != nil {
		return tx, err
	}
	if _, err := io.Copy(part, bytes.NewReader(wav)); err != nil {
		return tx, err
	}
	if body.Len() > speech.MaxInputBytes+10_000 {
		return tx, fmt.Errorf("audio input exceeds limit")
	}
	if err := form.Close(); err != nil {
		return tx, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(baseURL, "/")+"/inference", &body)
	if err != nil {
		return tx, err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	client := http.Client{Timeout: 2 * time.Minute}
	response, err := client.Do(req)
	if err != nil {
		return tx, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	if err != nil {
		return tx, err
	}
	if response.StatusCode != http.StatusOK {
		return tx, fmt.Errorf("ASR server: %s: %s", response.Status, raw)
	}
	var native struct {
		Language string `json:"language"`
		Text     string `json:"text"`
	}
	if err := json.Unmarshal(raw, &native); err != nil {
		return tx, fmt.Errorf("invalid ASR response: %w", err)
	}
	if strings.TrimSpace(native.Text) == "" {
		return tx, ErrNoSpeech
	}
	tx.Language = map[string]string{"english": "en", "russian": "ru", "en": "en", "ru": "ru"}[strings.ToLower(native.Language)]
	if tx.Language == "" {
		return tx, fmt.Errorf("unsupported ASR language %q", native.Language)
	}
	tx.Text = strings.TrimSpace(native.Text)
	return tx, nil
}
