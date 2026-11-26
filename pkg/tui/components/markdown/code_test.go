package markdown

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCodeFencesPreserveLiteralSource(t *testing.T) {
	for _, sample := range []struct{ language, source string }{
		{"json", `{"args":{"code":"return input * 2;"}}`},
		{"go", "func main(){println(1)}"},
		{"yaml", "defaults: &defaults {name: \"yes\", count: 2} # comment\ncopy: *defaults"},
		{"bash", "if true;then cat <<'EOF'\n  $NAME\nEOF\nfi"},
		{"sh", "if [ -n \"$NAME\" ];then echo \"$NAME\";fi"},
		{"js", "throw new Error('only source');const x=(a)=>a+1"},
		{"ts", "const x=(a:number):number=>a+1"},
		{"jsx", "const x=<div id='x'>{4+3}</div>"},
		{"tsx", "const x=(p:{value:number})=><div>{p.value}</div>"},
		{"html", "<div   class='card'>Hello</div>"},
		{"css", ".card{color:red}"},
		{"scss", "$accent:red; .card{color:$accent}"},
		{"graphql", "query{viewer{id}}"},
		{"rust", "fn main(){let x=1 println!(\"{}\",x)}"},
		{"rs", "fn main(){let x=1}"},
		{"c", "int main(){return 0;}"},
		{"cpp", "#include <z.h>\n#include <a.h>\n#define TWICE(x) ((x)+(x))"},
		{"unknown", "literal ** *.go ` <tag>  "},
	} {
		t.Run(sample.language, func(t *testing.T) {
			for _, ending := range []string{"", "\n~~~"} {
				source := "~~~" + sample.language + "\n" + sample.source + ending
				rows := Render(source, 200, testStyles())
				var code []string
				for _, row := range rows[1:] {
					plain := ansi.Strip(row)
					code = append(code, strings.TrimPrefix(plain, "│ "))
				}
				got := strings.Join(code, "\n")
				if got != sample.source {
					t.Fatalf("ending %q changed source: got %q, want %q", ending, got, sample.source)
				}
			}
		})
	}
}

func TestNestedFencePreservesSource(t *testing.T) {
	rows := Render("> ~~~json\n> {\"count\":42}\n> ~~~", 80, testStyles())
	got := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(got, `│ │ {"count":42}`) {
		t.Fatalf("nested fence changed source: %q", got)
	}
}

func TestLargeCodeSkipsHighlightingWithoutChangingSource(t *testing.T) {
	source := strings.Repeat("[", maxCodeBytes) + "invalid"
	r := renderer{styles: testStyles()}
	got := r.highlight(source, "json")
	if got != source {
		t.Fatal("large code did not retain its literal plain representation")
	}
}
