// Package adapters supplies shared JSON contracts for file operations. The
// underlying service has no dependency on agent or workflow execution.
package adapters

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/integrations/fileops"
)

type Operation struct {
	Service fileops.Service
	Kind    string
}

func (o Operation) Name() string   { return o.Kind }
func (o Operation) ReadOnly() bool { return o.Kind == "archive_list" }
func (o Operation) Description() string {
	if o.Kind == "download_file" {
		return "Download one public HTTP(S) URL to a new workspace file (maximum 16 MiB). Parent folder must exist. Never overwrites, follows redirects, retries or executes downloaded content. Returns path, size, media_type and SHA-256; binary bytes stay out of context."
	}
	return "List a workspace ZIP archive (maximum 16 MiB, 10000 entries and 2 MiB metadata reads), with offset/limit paging. Returns entry names, declared sizes and path/link flags without extracting or decompressing. safe_path is a path check, not approval to extract."
}

func (o Operation) Schema() json.RawMessage {
	if o.Kind == "download_file" {
		return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","minLength":1},"path":{"type":"string","minLength":1},"max_bytes":{"type":"integer","minimum":1,"maximum":16777216},"timeout_seconds":{"type":"integer","minimum":1,"maximum":120}},"required":["url","path"],"additionalProperties":false}`)
	}
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":100}},"required":["path"],"additionalProperties":false}`)
}

func (o Operation) OutputSchema() json.RawMessage {
	if o.Kind == "download_file" {
		return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"url":{"type":"string"},"size":{"type":"integer"},"sha256":{"type":"string"},"media_type":{"type":"string"}},"required":["path","url","size","sha256","media_type"],"additionalProperties":false}`)
	}
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"total":{"type":"integer"},"offset":{"type":"integer"},"more":{"type":"boolean"},"entries":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"size":{"type":"integer"},"compressed_size":{"type":"integer"},"directory":{"type":"boolean"},"symlink":{"type":"boolean"},"safe_path":{"type":"boolean"}},"required":["name","size","compressed_size","directory","symlink","safe_path"],"additionalProperties":false}}},"required":["path","total","offset","more","entries"],"additionalProperties":false}`)
}

func (o Operation) parse(args json.RawMessage) (fileops.DownloadRequest, fileops.ZIPRequest, error) {
	var download fileops.DownloadRequest
	var archive fileops.ZIPRequest
	if o.Kind != "download_file" && o.Kind != "archive_list" {
		return download, archive, fmt.Errorf("unknown file operation %q", o.Kind)
	}
	if err := automation.ValidateJSON(args, o.Schema()); err != nil {
		return download, archive, err
	}
	var err error
	if o.Kind == "download_file" {
		if err = json.Unmarshal(args, &download); err == nil {
			download, err = o.Service.ValidateDownload(download)
		}
	} else {
		if err = json.Unmarshal(args, &archive); err == nil {
			archive, err = o.Service.ValidateZIP(archive)
		}
	}
	return download, archive, err
}

func (o Operation) Validate(args json.RawMessage) error {
	_, _, err := o.parse(args)
	return err
}

// Both adapters authorize the canonical URL and the exact destination, or the
// ZIP source path. No directory-wide authorization or inferred filename is used.
func (o Operation) Resources(args json.RawMessage) ([]string, error) {
	download, archive, err := o.parse(args)
	if err != nil {
		return nil, err
	}
	if o.Kind == "download_file" {
		return []string{download.URL, download.Path}, nil
	}
	return []string{archive.Path}, nil
}

func (o Operation) Run(ctx context.Context, args json.RawMessage) (string, error) {
	download, archive, err := o.parse(args)
	if err != nil {
		return "", err
	}
	var result any
	if o.Kind == "download_file" {
		result, err = o.Service.Download(ctx, download)
	} else {
		result, err = o.Service.ListZIP(ctx, archive)
	}
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	return string(data), err
}
