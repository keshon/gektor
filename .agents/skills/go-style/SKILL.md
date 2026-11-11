---
name: go-style
description: Rules for readable, explicit, well-structured Go. Use whenever writing, reviewing, or refactoring any Go (.go) code, designing packages, directory layout, or APIs, or when code feels like a wall of text, callback soup, or has unclear ownership. Apply even to small edits.
---

# Go style

Write plain, explicit, linear Go that a maintainer can follow after a month away.
Match the existing codebase. Stay within the requested scope and keep existing behavior.

## 1. Group code with blank lines

Never emit a solid wall of statements. Separate code into paragraphs by meaning:
setup, validation, the main action, result handling. One blank line between groups,
none inside a group. A function longer than ~15 lines with no blank lines is wrong.

```go
// BAD
func (s *Store) Save(ctx context.Context, doc Doc) error {
	if doc.ID == "" {
		return errors.New("empty id")
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encoding doc: %w", err)
	}
	path := filepath.Join(s.dir, doc.ID+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	s.index[doc.ID] = path
	return nil
}

// GOOD
func (s *Store) Save(ctx context.Context, doc Doc) error {
	if doc.ID == "" {
		return errors.New("empty id")
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encoding doc: %w", err)
	}

	path := filepath.Join(s.dir, doc.ID+".json")
	err = os.WriteFile(path, data, 0o600)
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	s.index[doc.ID] = path

	return nil
}
```

## 2. No callbacks

Do not pass functions to wire components together (`OnEvent func(...)`, `WithHandler(...)`,
hook fields, closure bundles). Callbacks hide who calls what and in which order.

Use instead, in this order:
1. Return values and plain direct calls.
2. A concrete type with methods.
3. A small consumer-side interface when two implementations really exist.
4. A channel of results when work is truly asynchronous, with a clear owner.

Callbacks are acceptable only where the standard library or a framework requires
them (`http.HandlerFunc`, `sort.Slice`, `slices.SortFunc`) or for a genuinely
caller-supplied operation (a predicate, a visitor).

```go
// BAD
worker := NewWorker(WithOnDone(func(r Result) { ui.Show(r) }))

// GOOD
worker := NewWorker(cfg)
result, err := worker.Run(ctx)
if err != nil {
	return fmt.Errorf("running worker: %w", err)
}

ui.Show(result)
```

## 3. Make ownership explicit

For every resource, goroutine, and piece of mutable state, a reader must be able
to say who creates it, who mutates it, and who closes or stops it.

- The creator owns cleanup. A constructor that opens something documents the
  matching `Close`/`Stop`, and the caller defers it right away.
- One owner mutates a value. Do not hand the same mutable pointer, map, or slice
  to several components. Pass values, copy at the boundary, or give one type sole control.
- Keep a mutex in the same struct as the data it guards, with a comment saying so.
- Every goroutine has an owner that waits for it (`errgroup`, `WaitGroup`) and a
  `ctx` that stops it. No fire-and-forget goroutines.
- Doc comments on exported types state ownership: what is retained, what is
  copied, what the caller must close, what is safe for concurrent use.
- No global mutable state. No package-level `init()` that sets up resources.

```go
// Client talks to the API. It owns its HTTP transport; call Close when done.
// Safe for concurrent use.
type Client struct {
	mu    sync.Mutex // guards cache
	cache map[string]Item

	http *http.Client
}

client := NewClient(cfg)
defer client.Close()
```

## 4. Structure: not flat, not layered

Do not dump everything into one package with dozens of files. Do not create
technical layers either (`models/`, `services/`, `utils/`, `helpers/`).

- Group by feature or responsibility into directories. Each package has a clear
  purpose and a small public surface; internals stay unexported. Use an entry type
  when it owns state, dependencies, or a lifecycle. Stateless operations can be
  plain functions; do not invent a type just to provide a package entry point.
- Use `internal/` for code that is not a public API.
- Prefer a package-named file (`config.go`, `library.go`) as the reader's starting
  point: package documentation, main API types, and construction or orchestration
  when relevant. This is a reading convention, not a special Go entrypoint.
- Name the remaining files by responsibility (`store.go`, `encode.go`, `client.go`).
  Keep related data and behavior together; do not collect every method in the
  package-named file or make all other files data-only. Tiny packages may fit in
  one file. Do not invent scaffolding, an entry type, or wrappers to fill it.
- Keep a feature's types, config, errors, and tests together in its directory.
- Check a split by tracing one ordinary call across the new boundary. If the
  caller still needs most of the old internals, the boundary is wrong.

```
cmd/app/main.go
internal/
  app/            wiring and lifecycle only
  library/        feature: documents
    library.go    entry type, constructor
    store.go
    search.go
  sync/           feature: remote sync
    sync.go
    client.go
```

## 5. Linear control flow

- Early returns. No `else` after `return`, `break`, `continue`, or `panic`.
- At most 2-3 indentation levels. Flatten first; extract a helper only if its
  name describes a real operation.
- Prefer one meaningful operation per line. Give intermediate results descriptive
  variables so the reader can remember the steps without mentally evaluating
  nested calls. Prefer naming a value when its role deserves a name, even if used once.
  Trivial expressions such as `len(items)` may stay inline when a variable would
  only repeat the expression. Do not expand multi-step transformations into chains.
  Preserve expressions required by language or API semantics, such as an unsafe
  pointer conversion that must remain inside a syscall argument; comment why.
- Never call a method on an inline struct literal: `(T{X: x}).Do()`.
  Assign it to a variable, use a constructor, or make it a plain function.
- Name complex conditions: `canRetry := attempts < maxRetries && isTransient(err)`.
- No naked returns. No shadowing of `err` or `ctx`.
- Plain functions over stateless structs. No `Manager`, `Helper`, `Service`,
  or universal `Env` types.
- Config goes in a named struct with keyed fields, one per line. No functional options.

## 6. Errors, context, stdlib

- Check `err` on its own line right after the call. Never use the inline form
  `if val, err := f(); err != nil`. Declare the variables first, then check.
  For a call that returns only an error, assign it with `err =` (or `err :=` if
  it is the first use in the scope), then check on the next line.

```go
// BAD
if err := os.MkdirAll(dir, 0o750); err != nil {
	return fmt.Errorf("creating %s: %w", dir, err)
}

if data, err := os.ReadFile(path); err != nil {
	return fmt.Errorf("reading %s: %w", path, err)
}

// GOOD
err := os.MkdirAll(dir, 0o750)
if err != nil {
	return fmt.Errorf("creating %s: %w", dir, err)
}

data, err := os.ReadFile(path)
if err != nil {
	return fmt.Errorf("reading %s: %w", path, err)
}
```

- Wrap with context: `fmt.Errorf("doing X: %w", err)`. Inspect with `errors.Is`/`errors.As`.
- Never ignore an error silently. If it is intentional, say why in a comment.
- `ctx context.Context` is the first parameter for I/O and cancellable work.
- Use `any`, `slices`, `maps`, `cmp` where they replace boilerplate.

## 7. Comments

- Plain `//` above the declaration. No banners, no step-by-step narration, no change history.
- Exported names get a doc comment starting with the name: purpose and contract.
- Explain what code cannot say: ownership, concurrency, cancellation, nil/zero
  meaning, units, and why a non-obvious choice was made.
- If a comment explains tangled flow, simplify the flow instead.

## 8. Do not break hidden requirements

Before removing a branch, lock, cache, or error path, understand why it exists
(validation, ordering, cancellation, persistence format, resource limits).
Keep wire formats and user-visible behavior. Keep the diff focused.
Run `gofmt`, `go vet`, and affected tests; add `-race` when concurrency changed.
