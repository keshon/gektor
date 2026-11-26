package agent

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMarkdownSpans(t *testing.T) {
	st := defaultStyles()
	out := renderBlock(answerBlock("Say **hi** and `code`."), st, tagged(), 80)
	if strings.Contains(out, "**") || strings.Contains(out, "`") {
		t.Fatalf("markers survived: %q", out)
	}
	for _, want := range []string{"hi", "code"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %q", want, out)
		}
	}
}

func TestMarkdownHeaders(t *testing.T) {
	st := defaultStyles()
	out := renderBlock(answerBlock("# Title\n#tag stays"), st, tagged(), 80)
	if !strings.Contains(out, "Title") || !strings.Contains(out, "#tag stays") {
		t.Fatalf("header handling wrong: %q", out)
	}
	// The "# Title" marker must go; "#tag" must survive.
	lines := strings.Split(out, "\n")
	for _, ln := range lines {
		if strings.Contains(ln, "Title") && strings.Contains(ln, "#") {
			t.Fatalf("header marker survived: %q", ln)
		}
	}
}

func TestMarkdownFence(t *testing.T) {
	st := defaultStyles()
	out := renderBlock(answerBlock("```go\n# not header\n```"), st, tagged(), 80)
	if !strings.Contains(out, "# not header") {
		t.Fatalf("fence content altered: %q", out)
	}
}

func TestCodeSyntaxErrorsRemainVisibleInConversation(t *testing.T) {
	const broken = "fn main(){let x=1 println!(\"{}\",x)}"
	const source = "~~~rust\n" + broken + "\n~~~"
	for _, modern := range []bool{false, true} {
		for _, r := range []role{roleUser, roleAnswer, roleThink} {
			b := block{role: r, text: source}
			out := renderBlock(b, defaultStyles(), renderOpts{modern: modern, expand: true}, 100)
			if !strings.Contains(ansi.Strip(out), broken) {
				t.Fatalf("modern=%t role=%v repaired the source: %q", modern, r, out)
			}
		}
		for _, suffix := range []string{"", "\n\nExplain this error."} {
			props := transcriptProps{live: source + suffix, styles: defaultStyles(), modern: modern}
			out := strings.Join(renderLive(props, 100, renderOpts{modern: modern, expand: true}), "\n")
			if !strings.Contains(ansi.Strip(out), broken) {
				t.Fatalf("modern=%t live suffix=%q repaired the source: %q", modern, suffix, out)
			}
		}
	}
}

func TestMarkdownBullets(t *testing.T) {
	st := defaultStyles()
	out := ansi.Strip(renderBlock(answerBlock("- item\n  * sub"), st, tagged(), 80))
	if !strings.Contains(out, "• item") || !strings.Contains(out, "• sub") {
		t.Fatalf("bullets not prettified: %q", out)
	}
}

func TestMarkdownWrapSplitSpan(t *testing.T) {
	st := defaultStyles()
	// Unstamped to isolate wrapping from the timestamp reserve.
	out := renderBlock(block{role: roleAnswer, text: "say **bold words here** today"}, st, tagged(), 14)
	if strings.Contains(out, "**") {
		t.Fatalf("wrap split the span: %q", out)
	}
	for _, want := range []string{"bold", "today"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %q", want, out)
		}
	}
}

func TestMarkdownGlobSafe(t *testing.T) {
	st := defaultStyles()
	out := renderBlock(toolCardBlock("", "run rm **/*.go"), st, tagged(), 80)
	if !strings.Contains(out, "**/*.go") {
		t.Fatalf("tool args must stay raw: %q", out)
	}
}

func TestMarkdownLiteralsSurviveConversationRoles(t *testing.T) {
	for _, r := range []role{roleUser, roleAnswer, roleThink} {
		for _, source := range []string{"inspect *.go", "2 * 3", "unclosed `code", "find **/*.go", "partial **bold"} {
			b := block{role: r, text: source}
			out := ansi.Strip(renderBlock(b, defaultStyles(), renderOpts{expand: true}, 80))
			if out != source {
				t.Errorf("role %v: %q became %q", r, source, out)
			}
		}
	}
}

func TestMarkdownTableEndToEnd(t *testing.T) {
	st := defaultStyles()
	in := "4. Risks\n| Risk | Level |\n| --- | --- |\n| Sandbox | High |"
	out := renderBlock(answerBlock(in), st, tagged(), 80)
	if strings.Contains(out, ":---") || strings.Contains(out, "| ---") {
		t.Fatalf("separator row leaked: %q", out)
	}
	for _, want := range []string{"Risk", "Sandbox", "High"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %q", want, out)
		}
	}
}

func TestMarkdownPipesLiteral(t *testing.T) {
	st := defaultStyles()
	out := renderBlock(answerBlock("echo a | grep b"), st, tagged(), 80)
	if !strings.Contains(out, "echo a | grep b") {
		t.Fatalf("pipe line altered: %q", out)
	}
}

func TestLiveRendersMarkdownBeforeTheStep(t *testing.T) {
	st := defaultStyles()
	streamed := ansi.Strip(strings.Join(renderLive(transcriptProps{live: "<think>private</think>Say **hello**.", styles: st, speaker: "Assistant", labels: true}, 80, tagged()), "\n"))
	if strings.Contains(streamed, "<think>") {
		t.Fatalf("live response leaked reasoning: %q", streamed)
	}
	if strings.Contains(streamed, "**hello**") || !strings.Contains(streamed, "hello") {
		t.Fatalf("live stream must render markup: %q", streamed)
	}
	final := ansi.Strip(renderBlock(answerBlock("Say **hello**."), st, tagged(), 80))
	if strings.Contains(final, "**hello**") || !strings.Contains(final, "hello") {
		t.Fatalf("completed block must render the markup: %q", final)
	}
}
