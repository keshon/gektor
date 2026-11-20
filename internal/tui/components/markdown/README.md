# Terminal Markdown

`Render(source, width, styles)` returns ANSI-styled rows. The caller supplies all
styles; the renderer retains no state and performs no I/O. GEKTOR's agent screen
uses it for messages and reasoning, while tool arguments and outputs keep their
literal rendering path.

Goldmark owns Markdown parsing, including matching inline delimiters and table
cells. Chroma provides code tokens. Code blocks with an unknown language, a
lexer error, or more than 64 KiB of source use plain code styling. Diff blocks
use added/deleted styles while preserving prefixes. Highlighting never changes
the source text; tabs render as four spaces.

Tables shrink and wrap their columns to the available width. At widths too small
for useful columns they become labelled records. Links show the destination in
text; the renderer emits SGR styling but no OSC hyperlinks or cursor commands. Input escape
sequences and decoded control characters are removed before display.
Code wrapping retains spaces. At extreme widths, list and quote gutters yield
to content; a wide grapheme that cannot fit in one remaining cell is shown as
an ellipsis rather than overwriting the next pane.

This is an internal component, not a versioned public Go API. It depends on
Lip Gloss and ANSI cell measurement as well as its parser/highlighter.

```sh
go test ./internal/tui/components/markdown
```
