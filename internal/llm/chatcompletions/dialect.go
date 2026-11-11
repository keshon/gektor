package chatcompletions

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
