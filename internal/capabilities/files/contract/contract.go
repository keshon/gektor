// Package contract supplies shared JSON contracts for file operations. The
// underlying service has no dependency on agent or workflow execution.
package contract

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/capabilities/files"
	"github.com/keshon/gektor/internal/workflow"
)

type Operation struct {
	Service         files.Service
	Kind            string
	AuthorizeOutput OutputAuthorizer
}

type OutputAuthorizer func(context.Context, string, string, json.RawMessage) error

type arguments struct {
	download files.DownloadRequest
	list     files.ArchiveListRequest
	create   files.ArchiveCreateRequest
	extract  files.ArchiveExtractRequest
}

type operationSpec struct {
	kind        string
	description string
	readOnly    bool
	retrySafe   bool
	input       string
	output      string
}

func operations() []operationSpec {
	return []operationSpec{
		{
			kind:        "download_file",
			description: "Download one public HTTP(S) URL to a new workspace file (maximum 16 MiB). Parent folder must exist. Never overwrites, follows redirects, retries or executes downloaded content. Returns path, size, media_type and SHA-256; binary bytes stay out of context.",
			readOnly:    false,
			retrySafe:   false,
			input: `{
  "type": "object",
  "properties": {
    "url": {"type":"string","minLength":1},
    "path": {"type":"string","minLength":1},
    "max_bytes": {"type":"integer","minimum":1,"maximum":16777216},
    "timeout_seconds": {"type":"integer","minimum":1,"maximum":120}
  },
  "required": ["url","path"],
  "additionalProperties": false
}`,
			output: `{
  "type": "object",
  "properties": {
    "path": {"type":"string"},
    "url": {"type":"string"},
    "size": {"type":"integer"},
    "sha256": {"type":"string"},
    "media_type": {"type":"string"}
  },
  "required": ["path","url","size","sha256","media_type"],
  "additionalProperties": false
}`,
		},
		{
			kind:        "archive_list",
			description: "List a workspace archive (format: zip by default, tar, tar.gz or gzip; maximum 16 MiB, 10000 entries and 2 MiB metadata reads), with offset/limit paging. Returns entry names, declared sizes and path/link flags without writing files. TAR/GZIP listing scans and decompresses up to 16 MiB of payload plus 2 MiB of headers; compressed_size is 0 when unavailable. Gzip holds one file. ZIP listing reads metadata only. safe_path is a path check, not approval to extract.",
			readOnly:    true,
			retrySafe:   true,
			input: `{
  "type": "object",
  "properties": {
    "format": {"type":"string","enum":["zip","tar","tar.gz","gzip"]},
    "path": {"type":"string","minLength":1},
    "offset": {"type":"integer","minimum":0},
    "limit": {"type":"integer","minimum":1,"maximum":100}
  },
  "required": ["path"],
  "additionalProperties": false
}`,
			output: `{
  "type": "object",
  "properties": {
    "path": {"type":"string"},
    "total": {"type":"integer"},
    "offset": {"type":"integer"},
    "more": {"type":"boolean"},
    "entries": {"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"size":{"type":"integer"},"compressed_size":{"type":"integer"},"directory":{"type":"boolean"},"symlink":{"type":"boolean"},"safe_path":{"type":"boolean"}},"required":["name","size","compressed_size","directory","symlink","safe_path"],"additionalProperties":false}}
  },
  "required": ["path","total","offset","more","entries"],
  "additionalProperties": false
}`,
		},
		{
			kind:        "archive_create",
			description: "Create a new archive (format: zip by default, tar, tar.gz or gzip) from 1–128 explicit regular workspace files, retaining workspace-relative entry names (gzip takes one file and keeps its Latin-1 basename). No recursive discovery, source links or overwrites. Sources and archive output are limited to 16 MiB each. Returns path, files, size and SHA-256.",
			readOnly:    false,
			retrySafe:   false,
			input: `{
  "type": "object",
  "properties": {
    "format": {"type":"string","enum":["zip","tar","tar.gz","gzip"]},
    "paths": {"type":"array","minItems":1,"maxItems":128,"uniqueItems":true,"items":{"type":"string","minLength":1}},
    "path": {"type":"string","minLength":1}
  },
  "required": ["paths","path"],
  "additionalProperties": false
}`,
			output: `{
  "type": "object",
  "properties": {
    "path": {"type":"string"},
    "files": {"type":"integer"},
    "size": {"type":"integer"},
    "sha256": {"type":"string"}
  },
  "required": ["path","files","size","sha256"],
  "additionalProperties": false
}`,
		},
		{
			kind:        "archive_extract",
			description: "Extract a workspace archive (format: zip by default, tar, tar.gz or gzip) into a new folder whose parent exists. Authorizes every output path before writing. Refuses links, special/encrypted entries, unsafe paths, duplicates and file/directory collisions. Maximum 10000 output paths and 16 MiB expanded data; max_bytes can lower it. Removes incomplete output on failure. Never restores executable permissions.",
			readOnly:    false,
			retrySafe:   false,
			input: `{
  "type": "object",
  "properties": {
    "format": {"type":"string","enum":["zip","tar","tar.gz","gzip"]},
    "path": {"type":"string","minLength":1},
    "out_dir": {"type":"string","minLength":1},
    "max_bytes": {"type":"integer","minimum":1,"maximum":16777216}
  },
  "required": ["path","out_dir"],
  "additionalProperties": false
}`,
			output: `{
  "type": "object",
  "properties": {
    "path": {"type":"string"},
    "directory": {"type":"string"},
    "files": {"type":"integer"},
    "bytes": {"type":"integer"}
  },
  "required": ["path","directory","files","bytes"],
  "additionalProperties": false
}`,
		},
	}
}

func Names() []string {
	specs := operations()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.kind)
	}
	return names
}

func (o Operation) spec() operationSpec {
	for _, spec := range operations() {
		if spec.kind == o.Kind {
			return spec
		}
	}
	return operationSpec{}
}

func (o Operation) Name() string                  { return o.Kind }
func (o Operation) Description() string           { return o.spec().description }
func (o Operation) ReadOnly() bool                { return o.spec().readOnly }
func (o Operation) RetrySafe() bool               { return o.spec().retrySafe }
func (o Operation) Schema() json.RawMessage       { return json.RawMessage(o.spec().input) }
func (o Operation) OutputSchema() json.RawMessage { return json.RawMessage(o.spec().output) }

func (o Operation) parse(args json.RawMessage) (arguments, error) {
	var in arguments
	if o.spec().kind == "" {
		return in, fmt.Errorf("unknown file operation %q", o.Kind)
	}

	err := workflow.ValidateJSON(args, o.Schema())
	if err != nil {
		return in, err
	}

	switch o.Kind {
	case "download_file":
		err = json.Unmarshal(args, &in.download)
		if err != nil {
			return in, err
		}
		in.download, err = o.Service.ValidateDownload(in.download)
	case "archive_list":
		err = json.Unmarshal(args, &in.list)
		if err != nil {
			return in, err
		}
		in.list, err = o.Service.ValidateArchiveList(in.list)
	case "archive_create":
		err = json.Unmarshal(args, &in.create)
		if err != nil {
			return in, err
		}
		in.create, err = o.Service.ValidateArchiveCreate(in.create)
	case "archive_extract":
		err = json.Unmarshal(args, &in.extract)
		if err != nil {
			return in, err
		}
		in.extract, err = o.Service.ValidateArchiveExtract(in.extract)
	}

	return in, err
}

func (o Operation) Validate(args json.RawMessage) error {
	_, err := o.parse(args)
	return err
}

// Resources covers explicit inputs and destinations. Extraction additionally
// authorizes each derived output after reading archive entries.
func (o Operation) Resources(args json.RawMessage) ([]string, error) {
	in, err := o.parse(args)
	if err != nil {
		return nil, err
	}

	switch o.Kind {
	case "download_file":
		return []string{in.download.URL, in.download.Path}, nil
	case "archive_create":
		return append(in.create.Paths, in.create.Path), nil
	case "archive_extract":
		return []string{in.extract.Path, in.extract.OutDir}, nil
	}

	return []string{in.list.Path}, nil
}

func (o Operation) Run(ctx context.Context, args json.RawMessage) (string, error) {
	in, err := o.parse(args)
	if err != nil {
		return "", err
	}

	var result any

	switch o.Kind {
	case "download_file":
		result, err = o.Service.Download(ctx, in.download)
	case "archive_list":
		result, err = o.Service.ListArchive(ctx, in.list)
	case "archive_create":
		result, err = o.Service.CreateArchive(ctx, in.create)
	case "archive_extract":
		var authorize files.WriteAuthorizer
		if o.AuthorizeOutput != nil {
			authorize = func(ctx context.Context, path string) error { return o.AuthorizeOutput(ctx, o.Name(), path, args) }
		}

		result, err = o.Service.ExtractArchive(ctx, in.extract, authorize)
	}

	if err != nil {
		return "", err
	}

	data, err := json.Marshal(result)

	return string(data), err
}

func (o Operation) ValidateTemplate(raw json.RawMessage) error {
	return workflow.ValidateLiteralPaths(raw, []string{"path", "out_dir", "paths"}, func(path string) error {
		if o.Service.WS == nil {
			return fmt.Errorf("workspace unavailable")
		}

		_, err := o.Service.WS.Relative(path)
		return err
	})
}
