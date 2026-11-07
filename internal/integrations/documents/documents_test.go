package documents

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/workspace"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "workflow" && os.Args[2] == "internal-document-worker" {
		if err := Serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

func officeBytes(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, content := range parts {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const wordHeader = `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`
const wordFooter = `</w:body></w:document>`
const sheetHeader = `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`
const sheetFooter = `</sheetData></worksheet>`

func TestDOCXOrderTablesRevisionsAndPaging(t *testing.T) {
	xml := wordHeader + `<w:p><w:r><w:t>Hello </w:t></w:r><w:r><w:t>world</w:t><w:tab/><w:t>!</w:t></w:r><w:del><w:r><w:t>removed</w:t></w:r></w:del></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>A</w:t></w:r></w:p><w:p><w:r><w:t>B</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>42</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:p><w:r><w:t>Last</w:t></w:r></w:p>` + wordFooter
	data := officeBytes(t, map[string]string{"word/document.xml": xml})
	for offset := 0; offset < 4; offset++ {
		p, err := openOffice(data)
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.docx(Request{Path: "input.docx", Offset: offset, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if got.Total != 3 || got.Offset != min(offset, 3) || got.NextOffset != min(offset+1, 3) || got.More != (offset < 2) {
			t.Fatal(got)
		}
		switch offset {
		case 0:
			if got.Blocks[0].Text != "Hello world\t!" {
				t.Fatal(got)
			}
		case 1:
			if got.Blocks[0].Kind != "table_row" || strings.Join(got.Blocks[0].Cells, "|") != "A\nB|42" || got.Blocks[0].Table != 1 {
				t.Fatal(got)
			}
		case 2:
			if got.Blocks[0].Text != "Last" {
				t.Fatal(got)
			}
		case 3:
			if len(got.Blocks) != 0 {
				t.Fatal(got)
			}
		}
	}
}

func TestDOCXBoundsAndMalformedParts(t *testing.T) {
	data := officeBytes(t, map[string]string{"word/document.xml": wordHeader + strings.Repeat(`<w:p><w:r><w:t>`+strings.Repeat("Ж", 3000)+`</w:t></w:r></w:p>`, 20) + wordFooter})
	p, _ := openOffice(data)
	got, err := p.docx(Request{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !got.More || got.NextOffset == 0 || got.NextOffset >= 20 {
		t.Fatal(got)
	}
	total := 0
	for _, b := range got.Blocks {
		total += len(b.Text)
		if !b.Truncated || !utf8.ValidString(b.Text) {
			t.Fatal("unmarked or broken truncation")
		}
	}
	if total > MaxTextBytes {
		t.Fatal(total)
	}
	for _, xml := range []string{`<!DOCTYPE doc [<!ENTITY x SYSTEM "file:///secret">]>` + wordHeader + `<w:p><w:t>&x;</w:t></w:p>` + wordFooter, wordHeader + `<w:tbl><w:tr><w:tc><w:tbl/></w:tc></w:tr></w:tbl>` + wordFooter, wordHeader + `<w:p>`} {
		p, _ := openOffice(officeBytes(t, map[string]string{"word/document.xml": xml}))
		if _, err := p.docx(Request{Limit: 1}); err == nil {
			t.Fatal("accepted malformed/unsupported document")
		}
	}
	p, _ = openOffice(officeBytes(t, map[string]string{"word/document.xml": strings.Repeat("x", MaxFileBytes+1)}))
	if _, err := p.docx(Request{Limit: 1}); err == nil {
		t.Fatal("accepted expanded XML bomb")
	}
}

func spreadsheetParts(sheet string) map[string]string {
	return map[string]string{
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Data" sheetId="1" r:id="rId1"/><sheet name="Hidden" state="hidden" sheetId="2" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="https://example.com/sheet" TargetMode="External"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><r><t>Hello </t></r><r><t>world</t></r></si></sst>`,
		"xl/worksheets/sheet1.xml":   sheetHeader + sheet + sheetFooter,
	}
}

func TestXLSXSheetsSparseCellsFormulaAndCachedValues(t *testing.T) {
	data := officeBytes(t, spreadsheetParts(`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1"><f>SUM(A2:B2)</f><v>17</v></c><c r="D1" t="inlineStr"><is><t>inline</t></is></c></row><row r="1048576"><c r="XFD1048576" t="b"><v>1</v></c></row>`))
	p, _ := openOffice(data)
	sheets, err := p.xlsx(Request{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets.Sheets) != 2 || sheets.Sheets[1].State != "hidden" || len(sheets.Cells) != 0 {
		t.Fatal(sheets)
	}
	p, _ = openOffice(data)
	got, err := p.xlsx(Request{Sheet: "Data", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 4 || !got.More || got.NextOffset != 2 || got.Cells[0].Value != "Hello world" || got.Cells[1].Formula != "SUM(A2:B2)" || got.Cells[1].Value != "17" {
		t.Fatal(got)
	}
	p, _ = openOffice(data)
	got, err = p.xlsx(Request{Sheet: "Data", Offset: 2, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if got.More || len(got.Cells) != 2 || got.Cells[1].Address != "XFD1048576" || got.Cells[0].Value != "inline" {
		t.Fatal(got)
	}
	for _, sheet := range []string{"Hidden", "missing"} {
		p, _ := openOffice(data)
		if _, err := p.xlsx(Request{Sheet: sheet, Limit: 1}); err == nil {
			t.Fatal("read external/missing sheet")
		}
	}
}

func TestXLSXRejectsBadReferencesAndDuplicateCells(t *testing.T) {
	for _, sheet := range []string{`<row><c r="A1" t="s"><v>99</v></c></row>`, `<row><c r="A1"/><c r="A1"/></row>`, `<row><c r="XFE1"/></row>`, `<row><c r="A1048577"/></row>`, `<row><c r="A+1"/></row>`} {
		p, _ := openOffice(officeBytes(t, spreadsheetParts(sheet)))
		if _, err := p.xlsx(Request{Sheet: "Data", Limit: 1}); err == nil {
			t.Fatal("accepted", sheet)
		}
	}
}

func pdfBytes(content string) []byte {
	objects := []string{`<< /Type /Catalog /Pages 2 0 R >>`, `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`, `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>`, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content), `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`, `<< /Title (Test report) /Author (Gektor) >>`}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, o := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info 6 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return b.Bytes()
}

func TestPDFTextMetadataAndNoText(t *testing.T) {
	for _, content := range []string{"BT /F1 12 Tf 20 100 Td (Hello PDF) Tj ET", ""} {
		got, err := readPDF(pdfBytes(content), Request{Path: "input.pdf", Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if got.Total != 1 || got.NextOffset != 1 || got.More || got.Metadata["Title"] != "Test report" || len(got.Pages) != 1 {
			t.Fatal(got)
		}
		if content == "" {
			if !got.Pages[0].NoText {
				t.Fatal(got)
			}
		} else if !strings.Contains(got.Pages[0].Text, "Hello PDF") {
			t.Fatal(got)
		}
	}
}

func TestPDFTruncationDoesNotMarkTextPageEmpty(t *testing.T) {
	content := "BT /F1 12 Tf 20 100 Td (" + strings.Repeat(" ", MaxTextBytes) + "Hello) Tj ET"
	got, err := readPDF(pdfBytes(content), Request{Limit: 1})
	if err != nil || len(got.Pages) != 1 || !got.Pages[0].Truncated || got.Pages[0].NoText {
		t.Fatal(got, err)
	}
}

func TestDocumentWorkerReadsBytesAndContainsMalformedPDF(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := Service{WS: ws}
	if err := ws.WriteFile("report.pdf", pdfBytes("BT /F1 12 Tf 20 100 Td (Hello PDF) Tj ET"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := s.Read(t.Context(), "pdf_read", Request{Path: "report.pdf"})
	if err != nil || !strings.Contains(out, "Hello PDF") || !json.Valid([]byte(out)) {
		t.Fatal(out, err)
	}
	// Upstream parser issue #80: an unfinished array used to loop and allocate
	// forever. The pinned version may return no text; either outcome must be
	// bounded, and an empty extraction must be reported explicitly.
	if err := ws.WriteFile("broken.pdf", pdfBytes("BT /F1 12 Tf 20 100 Td [ "), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if out, err := s.Read(ctx, "pdf_read", Request{Path: "broken.pdf"}); err == nil && !strings.Contains(out, `"no_text":true`) {
		t.Fatal("malformed PDF produced unmarked empty extraction", out)
	}
	out, err = s.Read(t.Context(), "pdf_read", Request{Path: "report.pdf"})
	if err != nil || !strings.Contains(out, "Hello PDF") {
		t.Fatal("parser failure damaged subsequent calls", out, err)
	}
	for _, req := range []Request{{Path: "../escape.pdf"}, {Path: "report.pdf", Offset: -1}, {Path: "report.pdf", Limit: 11}} {
		if _, err := s.Read(t.Context(), "pdf_read", req); err == nil {
			t.Fatal("accepted", req)
		}
	}
}
