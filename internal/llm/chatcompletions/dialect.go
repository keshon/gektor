package chatcompletions

import (
	"fmt"
	"strings"
)

// dialect changes only Chat Completions request formatting.
type dialect interface {
	name() string
	applySampling(*wireRequest, bool)
	contentGrammar() string
	applyStructured(*wireRequest, string, string)
}

// kinds lists supported request dialects, independently of provider integrations.
var kinds = []struct {
	kind    string
	dialect dialect
}{
	{"kobold", koboldDialect{}},
	{"llama", llamaDialect{}},
	{"openai", openaiDialect{}},
}

// Kinds lists supported request dialects.
func Kinds() []string {
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, k.kind)
	}
	return out
}

// ClientFor builds a wire client using the selected request dialect.
func ClientFor(kind, baseURL, model string) (*Server, error) {
	for _, k := range kinds {
		if k.kind == kind {
			return newServer(baseURL, model, k.dialect), nil
		}
	}
	return nil, fmt.Errorf("unknown backend kind %q, want one of %s",
		kind, strings.Join(Kinds(), " or "))
}
