// Package contract shares document JSON contracts between execution engines.
package contract

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/capabilities/documents"
	"github.com/keshon/gektor/internal/workflow"
)

type Operation struct {
	Service documents.Service
	Kind    string
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
			kind:        "docx_read",
			description: "Read DOCX body paragraphs and table rows in order, with offset/limit paging (default 50, maximum 100 blocks). Text continuation traverses paragraph text or row cells in order, retaining cell positions. No rendering, headers, footnotes, nested tables or legacy .doc. Item offset is zero-based. For an item's next_text_offset, repeat its item offset with text_offset (Unicode code points); limit defaults to 1 and must be 1. Finish item continuations even when more=false. Empty or omitted fields in a truncated item may still be unread; finish it before claiming content is absent. 32 KiB content budget. Keep the file unchanged.",
			readOnly:    true,
			retrySafe:   true,
			input: `{
  "additionalProperties": false,
  "properties": {
    "limit": {"maximum":100,"minimum":1,"type":"integer"},
    "offset": {"minimum":0,"type":"integer"},
    "path": {"minLength":1,"type":"string"},
    "text_offset": {"description":"Continue one item at this Unicode code-point offset; copy next_text_offset. Requires limit=1 (default when present).","minimum":0,"type":"integer"}
  },
  "required": ["path"],
  "type": "object"
}`,
			output: `{
  "additionalProperties": false,
  "properties": {
    "blocks": {"items":{"additionalProperties":false,"properties":{"cells":{"items":{"type":"string"},"type":"array"},"kind":{"type":"string"},"next_text_offset":{"minimum":0,"type":"integer"},"table":{"type":"integer"},"text":{"type":"string"},"text_offset":{"minimum":0,"type":"integer"},"truncated":{"type":"boolean"}},"required":["kind","truncated"],"type":"object"},"type":"array"},
    "more": {"type":"boolean"},
    "next_offset": {"type":"integer"},
    "offset": {"type":"integer"},
    "path": {"type":"string"},
    "total": {"type":"integer"}
  },
  "required": ["path","total","offset","next_offset","more","blocks"],
  "type": "object"
}`,
		},
		{
			kind:        "xlsx_read",
			description: "Read XLSX sheet names; set sheet to an exact name to read stored cells with offset/limit paging (default 50, maximum 100). Text continuation requires sheet and traverses value then formula. Append chunks to their same fields. Cell type describes the stored value, not formula presence: even a string cell can have a formula, returned only after its value is fully read. No recalculation; dates remain raw numbers. No macros, external links, formatting or legacy .xls. Item offset is zero-based. For an item's next_text_offset, repeat its item offset with text_offset (Unicode code points); limit defaults to 1 and must be 1. Finish item continuations even when more=false. Empty or omitted fields in a truncated item may still be unread; finish it before claiming content is absent. 32 KiB content budget. Keep the file unchanged.",
			readOnly:    true,
			retrySafe:   true,
			input: `{
  "additionalProperties": false,
  "properties": {
    "limit": {"maximum":100,"minimum":1,"type":"integer"},
    "offset": {"minimum":0,"type":"integer"},
    "path": {"minLength":1,"type":"string"},
    "sheet": {"maxLength":255,"minLength":1,"type":"string"},
    "text_offset": {"description":"Continue one item at this Unicode code-point offset; copy next_text_offset. Requires limit=1 (default when present).","minimum":0,"type":"integer"}
  },
  "required": ["path"],
  "type": "object"
}`,
			output: `{
  "additionalProperties": false,
  "properties": {
    "cells": {"items":{"additionalProperties":false,"properties":{"address":{"type":"string"},"formula":{"type":"string"},"formula_range":{"type":"string"},"formula_shared_index":{"type":"string"},"formula_type":{"type":"string"},"next_text_offset":{"minimum":0,"type":"integer"},"text_offset":{"minimum":0,"type":"integer"},"truncated":{"type":"boolean"},"type":{"type":"string"},"value":{"type":"string"}},"required":["address","type","value","truncated"],"type":"object"},"type":"array"},
    "more": {"type":"boolean"},
    "next_offset": {"type":"integer"},
    "offset": {"type":"integer"},
    "path": {"type":"string"},
    "sheet": {"type":"string"},
    "sheets": {"items":{"additionalProperties":false,"properties":{"name":{"type":"string"},"state":{"type":"string"}},"required":["name","state"],"type":"object"},"type":"array"},
    "total": {"type":"integer"}
  },
  "required": ["path","total","offset","next_offset","more","sheet","sheets","cells"],
  "type": "object"
}`,
		},
		{
			kind:        "pdf_read",
			description: "Read PDF metadata and text pages, with offset/limit paging (default/maximum 10 pages) and one-based page numbers. no_text describes the full extracted page, not its current slice. No OCR or encrypted PDFs; text order may differ from visual layout. Item offset is zero-based. For an item's next_text_offset, repeat its item offset with text_offset (Unicode code points); limit defaults to 1 and must be 1. Finish item continuations even when more=false. Empty or omitted fields in a truncated item may still be unread; finish it before claiming content is absent. 32 KiB content budget. Keep the file unchanged.",
			readOnly:    true,
			retrySafe:   true,
			input: `{
  "additionalProperties": false,
  "properties": {
    "limit": {"maximum":10,"minimum":1,"type":"integer"},
    "offset": {"minimum":0,"type":"integer"},
    "path": {"minLength":1,"type":"string"},
    "text_offset": {"description":"Continue one item at this Unicode code-point offset; copy next_text_offset. Requires limit=1 (default when present).","minimum":0,"type":"integer"}
  },
  "required": ["path"],
  "type": "object"
}`,
			output: `{
  "additionalProperties": false,
  "properties": {
    "metadata": {"additionalProperties":{"type":"string"},"type":"object"},
    "more": {"type":"boolean"},
    "next_offset": {"type":"integer"},
    "offset": {"type":"integer"},
    "pages": {"items":{"additionalProperties":false,"properties":{"next_text_offset":{"minimum":0,"type":"integer"},"no_text":{"type":"boolean"},"number":{"type":"integer"},"text":{"type":"string"},"text_offset":{"minimum":0,"type":"integer"},"truncated":{"type":"boolean"}},"required":["number","text","truncated","no_text"],"type":"object"},"type":"array"},
    "path": {"type":"string"},
    "total": {"type":"integer"}
  },
  "required": ["path","total","offset","next_offset","more","metadata","pages"],
  "type": "object"
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

func (o Operation) parse(args json.RawMessage) (documents.Request, error) {
	var in documents.Request
	err := workflow.ValidateJSON(args, o.Schema())
	if err != nil {
		return in, err
	}

	err = json.Unmarshal(args, &in)
	if err != nil {
		return in, err
	}

	return o.Service.Validate(o.Kind, in)
}
func (o Operation) Validate(args json.RawMessage) error {
	_, err := o.parse(args)
	return err
}
func (o Operation) Resources(args json.RawMessage) ([]string, error) {
	in, err := o.parse(args)
	if err != nil {
		return nil, err
	}

	return []string{in.Path}, nil
}
func (o Operation) Run(ctx context.Context, args json.RawMessage) (string, error) {
	in, err := o.parse(args)
	if err != nil {
		return "", err
	}

	return o.Service.Read(ctx, o.Kind, in)
}

func (o Operation) ValidateTemplate(raw json.RawMessage) error {
	return workflow.ValidateLiteralPaths(raw, []string{"path"}, func(path string) error {
		if o.Service.WS == nil {
			return fmt.Errorf("workspace unavailable")
		}

		_, err := o.Service.WS.Relative(path)
		return err
	})
}
