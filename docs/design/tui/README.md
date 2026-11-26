# TUI gallery

Open [the gallery](gallery.html) in a browser for terminal captures generated
from GEKTOR's test fixtures. Keep `gallery.css` and `gallery-ansi.css` beside the
HTML for offline viewing.

Regenerate the gallery and its ANSI stylesheet from the repository root:

```sh
python docs/design/tui/export-gallery.py
```

The [exporter](export-gallery.py) runs the Go fixture tests and requires Python 3
and Go. Its `FRAMES` list selects the captures. Review generated changes before
committing them and use the [interaction contracts](../../development/tui.md)
when changing layouts or controls.

Captures show rendered states. Check keyboard, mouse and focus behavior in the
running TUI using the [terminal reference](../../reference/terminal.md).
