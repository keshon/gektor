# Terminal Markdown

`Render(source, width, styles)` returns ANSI-styled rows. The caller supplies all
styles; the renderer retains no state and performs no I/O. GEKTOR's agent screen
uses it for messages and reasoning, while tool arguments and outputs keep their
literal rendering path.

Goldmark parses Markdown, and Chroma colors code by language. Closed fences
labelled `json`, `go` or `golang` use Go's standard library formatters.
Single-document `yaml` and `yml` expand flow collections using the YAML parse
tree. `bash` and `shell` use the Bash formatter. `sh` and `posix-shell` use
the POSIX shell formatter. Open fences, snippets a formatter rejects, multi-document YAML,
and YAML with directives keep their original layout. Formatting affects only the
rendered view. The session retains the original Markdown. Code blocks with an
unknown language, a lexer error, or more than 64 KiB of source use plain code styling. Diff blocks
use added/deleted styles while preserving prefixes. Highlighting never changes
the source text; tabs render as four spaces.

JavaScript, TypeScript, JSX, TSX, HTML, CSS, SCSS and GraphQL (`graphql` or
`gql`) use embedded Prettier 3.10.1 bundles. Goja runs the formatter with a
200 ms deadline in standard builds and a 3 second deadline in race builds.
Goja receives snippets as strings. Scripts and styles inside HTML fences keep
their original formatting. Prettier runs inside the GEKTOR binary.
The [upstream license](prettier/LICENSE) ships with releases.

Rust fences (`rust` and `rs`) use the same embedded Prettier runtime with the
`@live-codes/prettier-plugin-rust@0.2.0` browser bundle. Its formatting rules
differ from rustfmt and it can correct some syntax errors in the rendered view.
The bundle's SHA-256 is
`88c04375a4d65b83f0d69beade8074b53b9827a93e1c69173ee75d66151d16bd`.
Its [MIT license](prettier/RUST-LICENSE) ships with releases.

C and C++ fences (`c`, `cpp`, `c++`, `cc`, `cxx`) use the embedded
dprint ClangFormat WASM build. It runs inside wazero with a per-snippet time
and memory limit. It uses a two-space indent and keeps include order.
GEKTOR needs neither a system Clang installation nor a separate WASM file.
The Agent screen calls `WarmClangFormat` in a background command and refreshes
the displayed fences when compilation completes. Until then, C and C++ fences
display their original source with syntax coloring.

The embedded `clang-format/plugin.wasm` is from the npm package
`@dprint/clang-format@0.3.2`. Its SHA-256 is
`6651f34d66866f6904473f5907b312906a575935d8df1d4c9e8aea2afd56c9d5`.
The [upstream license](clang-format/LICENSE) ships with releases.

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
