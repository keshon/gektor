# Terminal Markdown

`Render(source, width, styles)` returns ANSI-styled rows. The caller supplies all
styles; the renderer retains no state and performs no I/O. GEKTOR's agent screen
uses it for messages and reasoning, while tool arguments and outputs keep their
literal rendering path.

Goldmark parses Markdown, and Chroma colors code by language. Code fences keep
source spelling, spacing, punctuation and syntax errors in both open and closed
blocks. The renderer does not format or execute code. Tabs display as four spaces;
terminal control sequences are removed before rendering. The session retains the
original Markdown.

Unknown languages, lexer errors, and blocks larger than 64 KiB use plain code
styling. Diff blocks use added/deleted styles while preserving prefixes.
Highlighting falls back to plain code if a lexer changes the text.

Tables shrink and wrap their columns to the available width. At widths too small
for useful columns they become labelled records. Links show the destination in
text; the renderer emits SGR styling but no OSC hyperlinks or cursor commands. Input escape
sequences and decoded control characters are removed before display.
Code wrapping retains spaces. At extreme widths, list and quote gutters yield
to content; a wide grapheme that cannot fit in one remaining cell is shown as
an ellipsis rather than overwriting the next pane.

Import it as `github.com/keshon/gektor/pkg/tui/components/markdown`.
It shares the root module version and [controls API status](../../README.md).
It depends on Lip Gloss and ANSI cell measurement as well as its parser/highlighter.

```sh
go test ./pkg/tui/components/markdown
```
