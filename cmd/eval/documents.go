package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// Markers lie past the first 32 KiB of a single structured item. A page-only
// reader cannot recover them, even though its top-level more flag is false.
func documentContinuationFixture() (map[string][]byte, error) {
	padding := strings.Repeat("Reference material. ", 2200)
	pack := func(parts map[string]string) ([]byte, error) {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		var names []string
		for name := range parts {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			f, err := w.Create(name)
			if err != nil {
				return nil, err
			}
			if _, err := f.Write([]byte(parts[name])); err != nil {
				return nil, err
			}
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}
	docx, err := pack(map[string]string{"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:tbl><w:tr><w:tc><w:p><w:r><w:t>` + padding + `</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Release authorization: EMBER-7391</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body></w:document>`})
	if err != nil {
		return nil, err
	}
	xlsx, err := pack(map[string]string{
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Audit" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row><c r="A1" t="str"><f>"QUARTZ-2846"</f><v>` + padding + `</v></c></row></sheetData></worksheet>`,
	})
	if err != nil {
		return nil, err
	}
	content := "BT /F1 12 Tf 20 100 Td (" + padding + "Final approval: HARBOR-6152) Tj ET"
	objects := []string{`<< /Type /Catalog /Pages 2 0 R >>`, `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`, `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>`, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content), `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, pdf.Len())
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return map[string][]byte{"release.docx": docx, "audit.xlsx": xlsx, "approval.pdf": pdf.Bytes()}, nil
}
