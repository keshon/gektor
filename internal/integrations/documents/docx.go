package documents

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

type Block struct {
	Kind      string   `json:"kind"`
	Text      string   `json:"text,omitempty"`
	Cells     []string `json:"cells,omitempty"`
	Table     int      `json:"table,omitempty"`
	Truncated bool     `json:"truncated"`
}

type DOCXResult struct {
	Path       string  `json:"path"`
	Total      int     `json:"total"`
	Offset     int     `json:"offset"`
	NextOffset int     `json:"next_offset"`
	More       bool    `json:"more"`
	Blocks     []Block `json:"blocks"`
}

func wordNS(ns string) bool {
	return ns == "http://schemas.openxmlformats.org/wordprocessingml/2006/main" || ns == "http://purl.oclc.org/ooxml/wordprocessingml/main"
}

// Paragraphs and table rows retain document order. Formatting, drawings,
// headers, footnotes and deleted revisions are not part of this text reader.
func (p *office) docx(in Request) (DOCXResult, error) {
	result := DOCXResult{Path: in.Path, Blocks: []Block{}}
	data, err := p.part("word/document.xml")
	if err != nil {
		return result, err
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	var paragraph, cell strings.Builder
	var cells []string
	table, depth, body := 0, 0, false
	validBody, rootSeen := false, false
	total, budget := 0, MaxTextBytes
	stopped := false
	add := func(block Block) {
		index := total
		total++
		if index < in.Offset || stopped {
			return
		}
		if len(result.Blocks) >= in.Limit || budget == 0 {
			stopped = true
			return
		}
		text, cut := clip(block.Text, min(MaxValueBytes, budget))
		block.Text = text
		budget -= len(text)
		block.Truncated = cut
		for i, s := range block.Cells {
			v, c := clip(s, min(MaxValueBytes, budget))
			block.Cells[i] = v
			budget -= len(v)
			block.Truncated = block.Truncated || c
		}
		result.Blocks = append(result.Blocks, block)
	}
	for {
		t, e := token(d)
		if e == io.EOF {
			break
		}
		if e != nil {
			return result, e
		}
		switch t := t.(type) {
		case xml.StartElement:
			if !rootSeen {
				rootSeen = true
				if !wordNS(t.Name.Space) || t.Name.Local != "document" {
					return result, fmt.Errorf("invalid DOCX document root")
				}
			}
			if !wordNS(t.Name.Space) {
				continue
			}
			switch t.Name.Local {
			case "body":
				body = true
				validBody = true
			case "del", "drawing", "pict", "txbxContent":
				if err := d.Skip(); err != nil {
					return result, err
				}
			case "tbl":
				if body {
					depth++
					if depth > 1 {
						return result, fmt.Errorf("nested DOCX tables are unsupported")
					}
					table++
				}
			case "tr":
				cells = []string{}
			case "tc":
				cell.Reset()
			case "p":
				paragraph.Reset()
			case "t":
				var text string
				if err := d.DecodeElement(&text, &t); err != nil {
					return result, err
				}
				if body {
					paragraph.WriteString(text)
				}
			case "tab":
				if body {
					paragraph.WriteByte('\t')
				}
			case "br", "cr":
				if body {
					paragraph.WriteByte('\n')
				}
			}
		case xml.EndElement:
			if !wordNS(t.Name.Space) {
				continue
			}
			switch t.Name.Local {
			case "p":
				if body {
					if depth > 0 {
						if cell.Len() > 0 {
							cell.WriteByte('\n')
						}
						cell.WriteString(paragraph.String())
					} else {
						add(Block{Kind: "paragraph", Text: paragraph.String()})
					}
				}
			case "tc":
				cells = append(cells, cell.String())
				if len(cells) > 1000 {
					return result, fmt.Errorf("DOCX row exceeds 1000 cells")
				}
			case "tr":
				if body {
					add(Block{Kind: "table_row", Cells: append([]string{}, cells...), Table: table})
				}
			case "tbl":
				if body {
					depth--
				}
			case "body":
				body = false
			}
		}
	}
	if !validBody {
		return result, fmt.Errorf("DOCX body is missing")
	}
	result.Total = total
	result.Offset = min(in.Offset, total)
	result.NextOffset = result.Offset + len(result.Blocks)
	result.More = result.NextOffset < total
	return result, nil
}
