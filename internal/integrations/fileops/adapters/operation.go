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
	Service         fileops.Service
	Kind            string
	AuthorizeOutput OutputAuthorizer
}

type OutputAuthorizer func(context.Context, string, string, json.RawMessage) error

func (o Operation) Name() string   { return o.Kind }
func (o Operation) ReadOnly() bool { return o.Kind == "archive_list" }
func (o Operation) Description() string {
	switch o.Kind {
	case "archive_create":
		return "Create a new archive (format: zip by default, tar, tar.gz or gzip) from 1–128 explicit regular workspace files, retaining workspace-relative entry names (gzip takes one file and keeps its Latin-1 basename). No recursive discovery, source links or overwrites. Sources and archive output are limited to 16 MiB each. Returns path, files, size and SHA-256."
	case "archive_extract":
		return "Extract a workspace archive (format: zip by default, tar, tar.gz or gzip) into a new folder whose parent exists. Authorizes every output path before writing. Refuses links, special/encrypted entries, unsafe paths, duplicates and file/directory collisions. Maximum 10000 output paths and 16 MiB expanded data; max_bytes can lower it. Removes incomplete output on failure. Never restores executable permissions."
	}
	if o.Kind == "download_file" {
		return "Download one public HTTP(S) URL to a new workspace file (maximum 16 MiB). Parent folder must exist. Never overwrites, follows redirects, retries or executes downloaded content. Returns path, size, media_type and SHA-256; binary bytes stay out of context."
	}
	return "List a workspace archive (format: zip by default, tar, tar.gz or gzip; maximum 16 MiB, 10000 entries and 2 MiB metadata reads), with offset/limit paging. Returns entry names, declared sizes and path/link flags without writing files. TAR/GZIP listing scans and decompresses up to 16 MiB of payload plus 2 MiB of headers; compressed_size is 0 when unavailable. Gzip holds one file. ZIP listing reads metadata only. safe_path is a path check, not approval to extract."
}

func (o Operation) Schema() json.RawMessage {
	switch o.Kind {
	case "archive_create":
		return json.RawMessage(`{"type":"object","properties":{"format":{"type":"string","enum":["zip","tar","tar.gz","gzip"]},"paths":{"type":"array","minItems":1,"maxItems":128,"uniqueItems":true,"items":{"type":"string","minLength":1}},"path":{"type":"string","minLength":1}},"required":["paths","path"],"additionalProperties":false}`)
	case "archive_extract":
		return json.RawMessage(`{"type":"object","properties":{"format":{"type":"string","enum":["zip","tar","tar.gz","gzip"]},"path":{"type":"string","minLength":1},"out_dir":{"type":"string","minLength":1},"max_bytes":{"type":"integer","minimum":1,"maximum":16777216}},"required":["path","out_dir"],"additionalProperties":false}`)
	}
	if o.Kind == "download_file" {
		return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","minLength":1},"path":{"type":"string","minLength":1},"max_bytes":{"type":"integer","minimum":1,"maximum":16777216},"timeout_seconds":{"type":"integer","minimum":1,"maximum":120}},"required":["url","path"],"additionalProperties":false}`)
	}
	return json.RawMessage(`{"type":"object","properties":{"format":{"type":"string","enum":["zip","tar","tar.gz","gzip"]},"path":{"type":"string","minLength":1},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":100}},"required":["path"],"additionalProperties":false}`)
}

func (o Operation) OutputSchema() json.RawMessage {
	switch o.Kind {
	case "archive_create":
		return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"files":{"type":"integer"},"size":{"type":"integer"},"sha256":{"type":"string"}},"required":["path","files","size","sha256"],"additionalProperties":false}`)
	case "archive_extract":
		return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"directory":{"type":"string"},"files":{"type":"integer"},"bytes":{"type":"integer"}},"required":["path","directory","files","bytes"],"additionalProperties":false}`)
	}
	if o.Kind == "download_file" {
		return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"url":{"type":"string"},"size":{"type":"integer"},"sha256":{"type":"string"},"media_type":{"type":"string"}},"required":["path","url","size","sha256","media_type"],"additionalProperties":false}`)
	}
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"total":{"type":"integer"},"offset":{"type":"integer"},"more":{"type":"boolean"},"entries":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"size":{"type":"integer"},"compressed_size":{"type":"integer"},"directory":{"type":"boolean"},"symlink":{"type":"boolean"},"safe_path":{"type":"boolean"}},"required":["name","size","compressed_size","directory","symlink","safe_path"],"additionalProperties":false}}},"required":["path","total","offset","more","entries"],"additionalProperties":false}`)
}

type arguments struct {
	download fileops.DownloadRequest
	list     fileops.ArchiveListRequest
	create   fileops.ArchiveCreateRequest
	extract  fileops.ArchiveExtractRequest
}

func (o Operation) parse(args json.RawMessage) (arguments, error) {
	var in arguments
	if o.Kind != "download_file" && o.Kind != "archive_list" && o.Kind != "archive_create" && o.Kind != "archive_extract" {
		return in, fmt.Errorf("unknown file operation %q", o.Kind)
	}
	if err := automation.ValidateJSON(args, o.Schema()); err != nil {
		return in, err
	}
	var err error
	switch o.Kind {
	case "download_file":
		if err = json.Unmarshal(args, &in.download); err == nil {
			in.download, err = o.Service.ValidateDownload(in.download)
		}
	case "archive_list":
		if err = json.Unmarshal(args, &in.list); err == nil {
			in.list, err = o.Service.ValidateArchiveList(in.list)
		}
	case "archive_create":
		if err = json.Unmarshal(args, &in.create); err == nil {
			in.create, err = o.Service.ValidateArchiveCreate(in.create)
		}
	case "archive_extract":
		if err = json.Unmarshal(args, &in.extract); err == nil {
			in.extract, err = o.Service.ValidateArchiveExtract(in.extract)
		}
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
		var authorize fileops.WriteAuthorizer
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
