package documents

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

type PDFPage struct {
	Number    int    `json:"number"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	NoText    bool   `json:"no_text"`
}
type PDFResult struct {
	Path       string            `json:"path"`
	Total      int               `json:"total"`
	Offset     int               `json:"offset"`
	NextOffset int               `json:"next_offset"`
	More       bool              `json:"more"`
	Metadata   map[string]string `json:"metadata"`
	Pages      []PDFPage         `json:"pages"`
}

// This parser runs only in a memory-limited, time-limited Gektor worker.
// It reads the existing text layer; it does not render pages or run PDF actions.
func readPDF(data []byte, in Request) (PDFResult, error) {
	result := PDFResult{Path: in.Path, Metadata: map[string]string{}, Pages: []PDFPage{}}
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return result, fmt.Errorf("read PDF (encrypted files are unsupported): %w", err)
	}
	if !r.Trailer().Key("Encrypt").IsNull() {
		return result, fmt.Errorf("encrypted PDF files are unsupported")
	}
	result.Total = r.NumPage()
	if result.Total < 0 || result.Total > 10000 {
		return result, fmt.Errorf("PDF page count exceeds limit")
	}
	info := r.Trailer().Key("Info")
	for _, key := range []string{"Title", "Author", "Subject", "Keywords", "Creator", "Producer", "CreationDate", "ModDate"} {
		value, cut := clip(info.Key(key).Text(), MaxValueBytes)
		if cut {
			return result, fmt.Errorf("PDF metadata field exceeds limit")
		}
		if value != "" {
			result.Metadata[key] = value
		}
	}
	result.Offset = min(in.Offset, result.Total)
	budget := MaxTextBytes
	for i := result.Offset; i < result.Total && len(result.Pages) < in.Limit && budget > 0; i++ {
		page := r.Page(i + 1)
		text, err := page.GetPlainText(nil)
		if err != nil {
			return result, fmt.Errorf("PDF page %d: %w", i+1, err)
		}
		noText := strings.TrimSpace(text) == ""
		text, cut := clip(text, budget)
		budget -= len(text)
		result.Pages = append(result.Pages, PDFPage{Number: i + 1, Text: text, Truncated: cut, NoText: noText})
	}
	result.NextOffset = result.Offset + len(result.Pages)
	result.More = result.NextOffset < result.Total
	return result, nil
}
