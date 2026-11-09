// Package documents reads document content without executing embedded code or
// following external references. Agent tools and workflow nodes share it.
package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/keshon/gektor/internal/worker"
	"github.com/keshon/gektor/internal/workspace"
)

const MaxFileBytes = 16 << 20
const MaxTextBytes = 32 << 10
const MaxValueBytes = 4096

type Request struct {
	Path       string `json:"path"`
	Sheet      string `json:"sheet,omitempty"`
	Offset     int    `json:"offset,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	TextOffset *int   `json:"text_offset,omitempty"`
}

type Service struct{ WS *workspace.Workspace }

func (s Service) Validate(kind string, in Request) (Request, error) {
	if s.WS == nil {
		return in, fmt.Errorf("workspace unavailable")
	}
	if in.Path == "" {
		return in, fmt.Errorf("path is required")
	}
	rel, err := s.WS.Relative(in.Path)
	if err != nil {
		return in, err
	}
	if rel == "." {
		return in, fmt.Errorf("path must name a file")
	}
	in.Path = filepath.ToSlash(rel)
	if err := validate(kind, &in); err != nil {
		return in, err
	}
	return in, nil
}

func validate(kind string, in *Request) error {
	if kind != "docx_read" && kind != "xlsx_read" && kind != "pdf_read" {
		return fmt.Errorf("unknown document operation %q", kind)
	}
	if in.Sheet != "" && kind != "xlsx_read" {
		return fmt.Errorf("sheet is only supported for XLSX")
	}
	maximum := 100
	if kind == "pdf_read" {
		maximum = 10
	}
	if in.Limit == 0 {
		in.Limit = min(50, maximum)
		if in.TextOffset != nil {
			in.Limit = 1
		}
	}
	if in.TextOffset != nil {
		if *in.TextOffset < 0 || in.Limit != 1 {
			return fmt.Errorf("text_offset must be nonnegative and requires limit=1")
		}
		if kind == "xlsx_read" && in.Sheet == "" {
			return fmt.Errorf("text_offset requires an exact XLSX sheet")
		}
	}
	if in.Offset < 0 || in.Limit < 1 || in.Limit > maximum {
		return fmt.Errorf("offset must be nonnegative and limit must be 1–%d", maximum)
	}
	return nil
}

type workerRequest struct {
	Kind    string  `json:"kind"`
	Request Request `json:"request"`
	Data    []byte  `json:"data"`
}

func (s Service) Read(ctx context.Context, kind string, in Request) (string, error) {
	in, err := s.Validate(kind, in)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := s.WS.Open(in.Path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
		return "", fmt.Errorf("document must be a regular file of at most 16 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxFileBytes {
		return "", fmt.Errorf("document exceeds 16 MiB")
	}
	raw, err := json.Marshal(workerRequest{Kind: kind, Request: in, Data: data})
	if err != nil {
		return "", err
	}
	return worker.Run(ctx, "internal-document-worker", raw, 10*time.Second, 1<<20)
}

// Serve receives bytes, not paths: the worker cannot reopen a different file
// after the caller's workspace and permission checks.
func Serve(in io.Reader, out io.Writer) error {
	if err := worker.Limit(); err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(in, (24<<20)+1))
	if err != nil {
		return err
	}
	if len(raw) > 24<<20 {
		return fmt.Errorf("document request exceeds limit")
	}
	var req workerRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return err
	}
	if len(req.Data) > MaxFileBytes {
		return fmt.Errorf("document exceeds 16 MiB")
	}
	if err := validate(req.Kind, &req.Request); err != nil {
		return err
	}
	result, err := parse(req)
	if err != nil {
		return err
	}
	raw, err = json.Marshal(result)
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return fmt.Errorf("document result exceeds limit")
	}
	_, err = out.Write(raw)
	return err
}

func parse(in workerRequest) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = fmt.Errorf("malformed or unsupported document")
		}
	}()
	if in.Kind == "pdf_read" {
		return readPDF(in.Data, in.Request)
	}
	pkg, err := openOffice(in.Data)
	if err != nil {
		return nil, err
	}
	if in.Kind == "docx_read" {
		return pkg.docx(in.Request)
	}
	return pkg.xlsx(in.Request)
}
