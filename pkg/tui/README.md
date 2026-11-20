# Terminal controls

`pkg/tui` provides GEKTOR's reusable Bubble Tea controls in the root Go module,
`github.com/keshon/gektor`. It uses the root `go.mod` and is versioned with
GEKTOR. The API may change between releases.

Import the packages you need:

```go
import (
    "github.com/keshon/gektor/pkg/tui/screen"
    "github.com/keshon/gektor/pkg/tui/components/form"
    "github.com/keshon/gektor/pkg/tui/components/table"
)
```

| Directory | Responsibility |
| --- | --- |
| [screen](screen) | Shared menu and overlay presentation, forms, tables, themes and input routing |
| [components](components) | Local controls, layout, terminal-cell operations and rendering |
| [examples/workbench](examples/workbench/README.md) | Offline reading-list application using the public packages |

Agent, Workflow and Settings stay in `internal/tui`. Terminal startup and the
services for persistence, permissions and execution stay in application packages.
The public controls have no imports from GEKTOR's `internal` code.

## Ownership and lifecycle

Create one `screen.Controls` for the application's Bubble Tea update loop. Do not
copy it after first use: `DialogID` and `TableID` identify state owned by that
instance. Pass input to the active surface and drain its events in the update
loop. Use the identity captured when a request started to handle its result.
Guarded operations reject IDs from closed or replaced dialogs.

Controls hold form drafts and manage focus, selection and scrolling. The
application supplies presentation data and reads copied snapshots or intent
events. Validate submissions before saving or starting work. The application
owns permissions, persistence and asynchronous workers; cancel and join those
workers on shutdown. Components can also be used directly.

The workbench is an in-memory reading list with editable notes and a background
refresh that can be cancelled. It preserves table navigation when opening a book
or an edit form and supports palette changes. Run it from the repository root:

```sh
go run ./pkg/tui/examples/workbench
go test ./pkg/tui/...
go run scripts/check-tui-package.go
```

The last command builds a separate consumer against a local copy of the root
module using unchanged public import paths. It checks local source without
publishing a release. The first build may download dependencies. The workbench
runs offline.

The packages use the repository's [MIT license](../../LICENSE).
