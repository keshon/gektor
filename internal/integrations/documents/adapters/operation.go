// Package adapters shares document JSON contracts between execution engines.
package adapters

import (
	"context"
	"encoding/json"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/integrations/documents"
)

type Operation struct {
	Service documents.Service
	Kind    string
}

func (o Operation) Name() string { return o.Kind }
func (o Operation) Description() string {
	switch o.Kind {
	case "docx_read":
		return "Read DOCX body paragraphs and table rows in order, with offset/limit paging (default 50, maximum 100 blocks). Returns next_offset, more and explicit truncation flags. Text only: no rendering, headers, footnotes, nested tables or legacy .doc."
	case "xlsx_read":
		return "Read XLSX sheet names; set sheet to an exact name to read stored cells with offset/limit paging (default 50, maximum 100). Cells keep addresses and types; formulas and cached values are returned without recalculation. Numeric dates remain raw numbers. No macros, external links, formatting or legacy .xls."
	default:
		return "Read PDF metadata and text pages, with zero-based offset and limit (default/maximum 10 pages). Returns one-based page numbers, next_offset, more and truncation flags. no_text means no text was extracted; scanned pages may need OCR, which is unavailable. Text order may differ from visual layout. No encrypted PDFs."
	}
}
func (o Operation) Schema() json.RawMessage {
	maximum := 100
	props := map[string]any{"path": map[string]any{"type": "string", "minLength": 1}, "offset": map[string]any{"type": "integer", "minimum": 0}}
	if o.Kind == "pdf_read" {
		maximum = 10
	}
	props["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": maximum}
	if o.Kind == "xlsx_read" {
		props["sheet"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 255}
	}
	raw, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "required": []string{"path"}, "additionalProperties": false})
	return raw
}
func (o Operation) OutputSchema() json.RawMessage {
	var schema string
	switch o.Kind {
	case "docx_read":
		schema = `{"blocks":{"type":"array","items":{"type":"object","properties":{"kind":{"type":"string"},"text":{"type":"string"},"cells":{"type":"array","items":{"type":"string"}},"table":{"type":"integer"},"truncated":{"type":"boolean"}},"required":["kind","truncated"],"additionalProperties":false}}}`
	case "xlsx_read":
		schema = `{"sheet":{"type":"string"},"sheets":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"state":{"type":"string"}},"required":["name","state"],"additionalProperties":false}},"cells":{"type":"array","items":{"type":"object","properties":{"address":{"type":"string"},"type":{"type":"string"},"value":{"type":"string"},"formula":{"type":"string"},"formula_type":{"type":"string"},"formula_range":{"type":"string"},"formula_shared_index":{"type":"string"},"truncated":{"type":"boolean"}},"required":["address","type","value","truncated"],"additionalProperties":false}}}`
	default:
		schema = `{"metadata":{"type":"object","additionalProperties":{"type":"string"}},"pages":{"type":"array","items":{"type":"object","properties":{"number":{"type":"integer"},"text":{"type":"string"},"truncated":{"type":"boolean"},"no_text":{"type":"boolean"}},"required":["number","text","truncated","no_text"],"additionalProperties":false}}}`
	}
	var props map[string]any
	_ = json.Unmarshal([]byte(schema), &props)
	props["path"] = map[string]any{"type": "string"}
	for _, name := range []string{"total", "offset", "next_offset"} {
		props[name] = map[string]any{"type": "integer"}
	}
	props["more"] = map[string]any{"type": "boolean"}
	required := []string{"path", "total", "offset", "next_offset", "more"}
	switch o.Kind {
	case "docx_read":
		required = append(required, "blocks")
	case "xlsx_read":
		required = append(required, "sheet", "sheets", "cells")
	default:
		required = append(required, "metadata", "pages")
	}
	raw, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false})
	return raw
}
func (o Operation) parse(args json.RawMessage) (documents.Request, error) {
	var in documents.Request
	if err := automation.ValidateJSON(args, o.Schema()); err != nil {
		return in, err
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return in, err
	}
	return o.Service.Validate(o.Kind, in)
}
func (o Operation) Validate(args json.RawMessage) error { _, err := o.parse(args); return err }
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
