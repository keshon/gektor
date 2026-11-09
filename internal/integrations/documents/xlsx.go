package documents

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

type Sheet struct {
	Name  string `json:"name"`
	State string `json:"state"`
}
type Cell struct {
	Address            string `json:"address"`
	Type               string `json:"type"`
	Value              string `json:"value"`
	Formula            string `json:"formula,omitempty"`
	FormulaType        string `json:"formula_type,omitempty"`
	FormulaRange       string `json:"formula_range,omitempty"`
	FormulaSharedIndex string `json:"formula_shared_index,omitempty"`
	Truncated          bool   `json:"truncated"`
	TextOffset         int    `json:"text_offset,omitempty"`
	NextTextOffset     int    `json:"next_text_offset,omitempty"`
}
type XLSXResult struct {
	Path       string  `json:"path"`
	Sheets     []Sheet `json:"sheets"`
	Sheet      string  `json:"sheet"`
	Total      int     `json:"total"`
	Offset     int     `json:"offset"`
	NextOffset int     `json:"next_offset"`
	More       bool    `json:"more"`
	Cells      []Cell  `json:"cells"`
}

func sheetNS(ns string) bool {
	return ns == "http://schemas.openxmlformats.org/spreadsheetml/2006/main" || ns == "http://purl.oclc.org/ooxml/spreadsheetml/main"
}
func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func (p *office) xlsx(in Request) (XLSXResult, error) {
	result := XLSXResult{Path: in.Path, Sheets: []Sheet{}, Cells: []Cell{}}
	workbook, err := p.part("xl/workbook.xml")
	if err != nil {
		return result, err
	}
	relData, err := p.part("xl/_rels/workbook.xml.rels")
	if err != nil {
		return result, err
	}
	rels := map[string]string{}
	d := xml.NewDecoder(bytes.NewReader(relData))
	for {
		t, e := token(d)
		if e == io.EOF {
			break
		}
		if e != nil {
			return result, e
		}
		s, ok := t.(xml.StartElement)
		if !ok || s.Name.Local != "Relationship" {
			continue
		}
		if attr(s, "TargetMode") == "External" {
			continue
		}
		typ := attr(s, "Type")
		if !strings.HasSuffix(typ, "/worksheet") {
			continue
		}
		target := attr(s, "Target")
		if strings.HasPrefix(target, "/") {
			target = strings.TrimPrefix(target, "/")
		} else {
			target = path.Join("xl", target)
		}
		if !strings.HasPrefix(target, "xl/") || strings.ContainsAny(target, "\\:?#") {
			return result, fmt.Errorf("unsafe worksheet relationship")
		}
		id := attr(s, "Id")
		if _, exists := rels[id]; exists {
			return result, fmt.Errorf("duplicate worksheet relationship")
		}
		rels[id] = target
	}
	d = xml.NewDecoder(bytes.NewReader(workbook))
	selected := ""
	names := map[string]bool{}
	for {
		t, e := token(d)
		if e == io.EOF {
			break
		}
		if e != nil {
			return result, e
		}
		s, ok := t.(xml.StartElement)
		if !ok || !sheetNS(s.Name.Space) || s.Name.Local != "sheet" {
			continue
		}
		name := attr(s, "name")
		if name == "" || len(name) > 255 || names[name] || len(result.Sheets) >= 256 {
			return result, fmt.Errorf("invalid, duplicate or too many XLSX sheets")
		}
		names[name] = true
		state := attr(s, "state")
		if state == "" {
			state = "visible"
		}
		if state != "visible" && state != "hidden" && state != "veryHidden" {
			return result, fmt.Errorf("invalid XLSX sheet visibility")
		}
		result.Sheets = append(result.Sheets, Sheet{Name: name, State: state})
		if name == in.Sheet {
			selected = rels[attr(s, "id")]
		}
	}
	if len(result.Sheets) == 0 {
		return result, fmt.Errorf("workbook has no sheets")
	}
	if in.Sheet == "" {
		return result, nil
	}
	if selected == "" {
		return result, fmt.Errorf("worksheet %q is missing, external or unsupported", in.Sheet)
	}
	result.Sheet = in.Sheet
	shared, err := p.sharedStrings()
	if err != nil {
		return result, err
	}
	data, err := p.part(selected)
	if err != nil {
		return result, err
	}
	d = xml.NewDecoder(bytes.NewReader(data))
	budget, stopped := MaxTextBytes, false
	previousRow, previousColumn := 0, 0
	validRoot := false
	for {
		t, e := token(d)
		if e == io.EOF {
			break
		}
		if e != nil {
			return result, e
		}
		s, ok := t.(xml.StartElement)
		if !ok || !sheetNS(s.Name.Space) {
			continue
		}
		if s.Name.Local == "worksheet" {
			validRoot = true
		}
		if s.Name.Local != "c" {
			continue
		}
		address := attr(s, "r")
		column, row, err := cellAddress(address)
		if err != nil {
			return result, err
		}
		if row < previousRow || (row == previousRow && column <= previousColumn) {
			return result, fmt.Errorf("unordered or duplicate XLSX cell %q", address)
		}
		previousRow, previousColumn = row, column
		c := Cell{Address: address, Type: attr(s, "t")}
		var raw struct {
			Value   string `xml:"v"`
			Formula struct {
				Text        string `xml:",chardata"`
				Type        string `xml:"t,attr"`
				Range       string `xml:"ref,attr"`
				SharedIndex string `xml:"si,attr"`
			} `xml:"f"`
			Inline struct {
				Text string `xml:"t"`
				Runs []struct {
					Text string `xml:"t"`
				} `xml:"r"`
			} `xml:"is"`
		}
		if err := d.DecodeElement(&raw, &s); err != nil {
			return result, err
		}
		c.Formula = raw.Formula.Text
		c.FormulaType, c.FormulaRange, c.FormulaSharedIndex = raw.Formula.Type, raw.Formula.Range, raw.Formula.SharedIndex
		if len(c.FormulaType) > 32 || len(c.FormulaRange) > 255 || len(c.FormulaSharedIndex) > 16 {
			return result, fmt.Errorf("invalid formula metadata")
		}
		c.Value = raw.Value
		switch c.Type {
		case "s":
			index, e := strconv.Atoi(raw.Value)
			if e != nil || index < 0 || index >= len(shared) {
				return result, fmt.Errorf("invalid shared string index")
			}
			c.Value = shared[index]
			c.Type = "string"
		case "inlineStr":
			c.Value = raw.Inline.Text
			for _, r := range raw.Inline.Runs {
				c.Value += r.Text
			}
			c.Type = "string"
		case "str":
			c.Type = "string"
		case "", "n":
			c.Type = "number"
		case "b":
			c.Type = "boolean"
		case "e":
			c.Type = "error"
		case "d":
			c.Type = "date"
		default:
			return result, fmt.Errorf("unsupported XLSX cell type %q", c.Type)
		}
		index := result.Total
		result.Total++
		if index < in.Offset || stopped {
			continue
		}
		if len(result.Cells) >= in.Limit || budget == 0 {
			stopped = true
			continue
		}
		parts, next, used, err := sliceText([]string{c.Value, c.Formula}, in.textStart(), budget)
		if err != nil {
			return result, err
		}
		if parts == nil {
			stopped = true
			continue
		}
		c.Value, c.Formula = parts[0], parts[1]
		c.TextOffset, c.NextTextOffset = in.textStart(), next
		c.Truncated = next > 0
		budget -= used
		result.Cells = append(result.Cells, c)
	}
	if !validRoot {
		return result, fmt.Errorf("invalid worksheet XML")
	}
	if in.TextOffset != nil && in.Offset >= result.Total {
		return result, fmt.Errorf("offset does not select an XLSX cell")
	}
	result.Offset = min(in.Offset, result.Total)
	result.NextOffset = result.Offset + len(result.Cells)
	result.More = result.NextOffset < result.Total
	return result, nil
}

// Cells remain sparse. An address such as XFD1048576 never allocates a matrix.
func cellAddress(s string) (column, row int, err error) {
	i := 0
	for i < len(s) && s[i] >= 'A' && s[i] <= 'Z' {
		column = column*26 + int(s[i]-'A'+1)
		i++
		if column > 16384 {
			return 0, 0, fmt.Errorf("invalid XLSX column")
		}
	}
	if i == 0 || i == len(s) || s[i] == '0' {
		return 0, 0, fmt.Errorf("invalid XLSX cell address %q", s)
	}
	for _, digit := range s[i:] {
		if digit < '0' || digit > '9' {
			return 0, 0, fmt.Errorf("invalid XLSX row")
		}
	}
	row, err = strconv.Atoi(s[i:])
	if err != nil || row < 1 || row > 1048576 {
		return 0, 0, fmt.Errorf("invalid XLSX row")
	}
	return column, row, nil
}

func (p *office) sharedStrings() ([]string, error) {
	if p.files["xl/sharedStrings.xml"] == nil {
		return nil, nil
	}
	data, err := p.part("xl/sharedStrings.xml")
	if err != nil {
		return nil, err
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	var values []string
	for {
		t, e := token(d)
		if e == io.EOF {
			return values, nil
		}
		if e != nil {
			return nil, e
		}
		s, ok := t.(xml.StartElement)
		if !ok || !sheetNS(s.Name.Space) || s.Name.Local != "si" {
			continue
		}
		var raw struct {
			Text string `xml:"t"`
			Runs []struct {
				Text string `xml:"t"`
			} `xml:"r"`
		}
		if err := d.DecodeElement(&raw, &s); err != nil {
			return nil, err
		}
		text := raw.Text
		for _, r := range raw.Runs {
			text += r.Text
		}
		if len(values) >= 100000 {
			return nil, fmt.Errorf("XLSX exceeds 100000 shared strings")
		}
		values = append(values, text)
	}
}
