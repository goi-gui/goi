# AGENTS.md

`goi` is a declarative GUI framework for Go on WebGPU. A `.goi` file describes the
window; Go finds elements by id and attaches behaviour.

## Names that are easy to get wrong

- Module is `github.com/ksckaan1/goi`, but the imported package is **`ui`**:
  `import "github.com/ksckaan1/goi/ui"`. Do not "fix" it to `goi`.
- Design files are **`.goi`** (not `.ui`). The project was renamed from `goui`;
  do not reintroduce either old name. The checker is `cmd/goicheck`.
- Everything user-facing is `ui.X`: `ui.MustByID[*ui.Button](w, "id")`.

## Commands

```sh
go build -ldflags=-linkmode=internal ./...   # required on Linux
go vet ./...                                 # SDL picks Wayland/X11 at runtime
gofmt -l .                                   # CI fails on any output
go test -race ./ui ./internal/syntax ./internal/schema ./internal/layout ./internal/paint ./internal/text ./internal/hotreload ./internal/lsp
go test ./...                                # also fine locally
go run ./cmd/goicheck -layout -ops examples/*/*.goi   # headless render check
```

- `-ldflags=-linkmode=internal` is not optional on Linux: goffi's dynamic
  imports (the renderer's FFI) fail under the external linker.
- `internal/platform` is the only cgo in the tree: a small `sdl_shim.c` over
  SDL3, because SDL3 has no Go binding we own and its dark, `cgo_import_dynamic`
  build clashes with goffi's. Everything above `platform` stays cgo-free.
- Run the examples **from the repo root** (`make gallery`, `make counter`, …).
  They load `examples/<name>/<name>.goi` by relative path; running the binary
  from elsewhere fails with "file not found".
- Golden AST snapshots: `go test ./internal/syntax -run TestGoldenAST -update`.
- `staticcheck ./...` runs in CI.

## Layers (`docs/ARCHITECTURE.md` has the table)

`syntax → schema → tree → layout/paint → render`, with `platform` for the window
and `ui` on top.

- Nothing below `ui` may import `ui`.
- Only `render` and `platform` touch the GPU or the window. `syntax`, `schema`,
  `tree`, `layout`, `paint`, `text` must stay pure so they run headless.
- `internal/componentdef` is the single list of component kinds and their
  properties; the schema and the node tree both read it.
- `internal/m3` is internal: user-facing theme work goes through the aliases in
  `ui` (`ui.Theme`, `ui.Colors`, `ui.LightTheme()`, `ui.DarkTheme()`).

## Adding a property (the step people miss)

A **component** property needs all of:
1. `internal/componentdef/componentdef.go` — the `props` map, plus `TypeOf` for a
   non-string type and `EnumValues` for an enum. The schema derives from this.
2. `internal/tree/components.go` `one()` — a distinct cell. A name missing here
   silently binds to `Label`, so the file accepts it and reads the wrong value.
3. `internal/paint/m3.go` — actually draw it.
4. `docs/components/<name>.md` — the table matches componentdef, and the page's
   `.goi` example is validated by `TestComponentDocsExamplesValidate`.

A **common** property (every element) needs `internal/schema/schema.go`
(`common`), `internal/tree/widgets.go` (`commonProps`) and
`internal/tree/build.go` (`b()` defaults), plus the table in
`docs/components.md`.

A **new element** additionally needs `Kinds`, `newM3Component` in
`internal/tree/build.go`, the dispatch in `paintComponent`, the alias in
`ui/types.go`, and a docs page.

Tests that enforce the above: `TestEveryComponentValidates`,
`TestEveryComponentPropHasItsOwnCell`, `TestComponentDocsExamplesValidate`.

## Design-file language

- `.goi` is literals only: no loops, no scripting, no conditionals beyond
  expressions (`app.dark ? "light_mode" : "dark_mode"`). Behaviour belongs in Go.
- Lists come from a hidden template plus `ui.Instantiate`; there is no `for`.
- Colours as M3 role names (`primary`, `surfaceContainer`, `onSurfaceVariant`)
  follow the scheme; hex literals are deliberately fixed. Prefer roles.
- `spacing` and `gap` are the same cell; the only intentional alias.
- `Positioned` reserves `-1` for "auto", so animation slides use
  `translateX`/`translateY`, not `left`.

## Go-side conventions

- `Bind` runs again after every hot reload: state declared inside it resets.
  Keep app state outside `Bind` (see `examples/counter/main.go`).
- `Prop[T].Set` is a no-op when the value is unchanged; `Set` marks the tree
  dirty and schedules a frame, so no manual repaint call is needed.
- `DirtyPaint` vs `DirtyLayout`: anything that changes size, text, padding,
  visibility or flex is `DirtyLayout`; layout dirt repaints everything.
- In `.goi`-driven apps do not build nodes by hand; look them up with
  `ByID[T]`/`MustByID[T]`.
- A render frame is two passes: `textRenderer.prepare`/`imageRenderer.prepare`
  reserve atlas space for the whole display list, then the emit loop appends
  instances. Keep that order — an atlas compaction mid-emit would invalidate
  instances already written.

## Docs

- `docs/components.md` is the grouped index; `docs/components/*.md` are one page
  per element. They were generated by a script that is **not** in the repo, so
  edit them by hand and keep the tables in sync with `componentdef`.
- `docs/SYNTAX.md` is the language reference, `docs/DECISIONS.md` the running
  rationale log (append a decision when you make a non-obvious choice).

## Verifying visuals

There is no screenshot test. `examples/gallery` is the manual test bed for every
component and page; `goicheck -layout` catches nodes that collapse to zero size,
which is the failure a syntax check cannot see.
