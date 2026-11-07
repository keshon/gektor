package documents

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

type office struct {
	files     map[string]*zip.File
	remaining int64
}

func openOffice(data []byte) (*office, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("read Office ZIP: %w", err)
	}
	if len(z.File) > 10000 {
		return nil, fmt.Errorf("Office ZIP exceeds 10000 entries")
	}
	p := &office{files: map[string]*zip.File{}, remaining: MaxFileBytes}
	var names int
	for _, f := range z.File {
		names += len(f.Name)
		if names > 2<<20 {
			return nil, fmt.Errorf("Office ZIP metadata exceeds limit")
		}
		if _, ok := p.files[f.Name]; ok {
			return nil, fmt.Errorf("duplicate Office part %q", f.Name)
		}
		if f.Flags&1 != 0 {
			return nil, fmt.Errorf("encrypted Office documents are unsupported")
		}
		p.files[f.Name] = f
	}
	return p, nil
}

func (p *office) part(name string) ([]byte, error) {
	f := p.files[name]
	if f == nil {
		return nil, fmt.Errorf("missing Office part %q", name)
	}
	if !f.Mode().IsRegular() || f.UncompressedSize64 > uint64(p.remaining) {
		return nil, fmt.Errorf("Office XML exceeds 16 MiB budget or is not a regular part")
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, p.remaining+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > p.remaining {
		return nil, fmt.Errorf("Office XML exceeds 16 MiB budget")
	}
	p.remaining -= int64(len(data))
	return data, nil
}

func clip(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}

// XML's standard decoder never resolves external entities. DTDs are rejected
// explicitly instead of being accepted as ignored declarations.
func token(d *xml.Decoder) (xml.Token, error) {
	t, err := d.Token()
	if directive, ok := t.(xml.Directive); ok && strings.Contains(strings.ToUpper(string(directive)), "DOCTYPE") {
		return nil, fmt.Errorf("XML document types are unsupported")
	}
	return t, err
}
