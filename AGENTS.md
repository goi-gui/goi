# AGENTS.md

`goi` is a declarative GUI framework for Go on WebGPU. A `.goi` file describes the
window; Go finds elements by id and attaches behaviour.

## Names that are easy to get wrong

- Module is `github.com/goi-gui/goi`, but the imported package is **`ui`**:
  `import "github.com/goi-gui/goi/ui"`. Do not "fix" it to `goi`.
- Design files are **`.goi`** (not `.ui`). The project was renamed from `goui`;
  do not reintroduce either old name. The checker is `cmd/goicheck`.
- Everything user-facing is `ui.X`: `ui.MustByID[*ui.Button](w, "id")`.

## Commands

```sh
go build -ldflags=-linkmode=internal ./...   # required on Linux
go vet ./...                                 # SDL picks Wayland/X11 at runtime
gofmt -l .                                   # CI fails on any output
go test -race -ldflags=-linkmode=internal ./ui ./internal/syntax ./internal/schema ./internal/layout ./internal/paint ./internal/text ./internal/hotreload ./internal/lsp ./internal/goifmt
go test -ldflags=-linkmode=internal ./...    # test binaries link goffi too
go run ./cmd/goicheck -layout -ops ../examples/*/*.goi  # headless render check
go run ./cmd/goifmt -l .                                # CI fails on any output
```

- `-ldflags=-linkmode=internal` is not optional on Linux: goffi's dynamic
  imports (the renderer's FFI) fail under the external linker.
- `internal/platform` is the only cgo in the tree: a small `sdl_shim.c` over
  SDL3, because SDL3 has no Go binding we own and its dark, `cgo_import_dynamic`
  build clashes with goffi's. Everything above `platform` stays cgo-free.
- The examples are their own module in `../examples`, next to this checkout.
  `make -C ../examples gallery` runs one, because each loads
  `<name>/<name>.goi` by relative path; running the binary from elsewhere fails
  with "file not found". They build against this tree through a `replace` in
  their `go.mod`.
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
4. `../docs/docs/components/<name>.md` — the table matches componentdef, and the page's
   `.goi` example is validated by `TestComponentDocsExamplesValidate`.

A **common** property (every element) needs `internal/schema/schema.go`
(`common`), `internal/tree/widgets.go` (`commonProps`) and
`internal/tree/build.go` (`b()` defaults), plus the table in
`../docs/docs/components.md`.

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
  Keep app state outside `Bind` (see `../examples/counter/main.go`).
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

- The documentation site is the sibling `../docs` checkout (Docusaurus). Its
  content is `../docs/docs/`: `components.md` is the grouped index,
  `components/*.md` one page per element. They were generated by a script that
  is **not** in the repo, so edit them by hand and keep the tables in sync with
  `componentdef`.
- `../docs/docs/syntax.md` is the language reference. The layer table and the
  rationale log stay in this repo (`docs/ARCHITECTURE.md`, `docs/DECISIONS.md`),
  because they describe the code, not the site; append a decision when you make
  a non-obvious choice.

## Releasing

Only on an explicit request to publish a new version ("yeni versiyon paylaş").
Never create a tag or a release on your own.

1. Read the tags with `git tag --sort=-v:refname` and bump semantically: while
   the module is `v0.x`, a breaking change to a `.goi` file or the Go API is a
   minor bump, everything else a patch.
2. Start from a clean tree with the checks above passing; commit anything still
   open first, so the tag names a state that was verified.
3. `git tag -a vX.Y.Z -m "vX.Y.Z" && git push origin master --follow-tags`.
4. `gh release create vX.Y.Z --title vX.Y.Z --notes-file <file>`. Write the
   notes in English: a short summary of the release, then the changes grouped by
   feature, each saying what changed and what it means for an app, plus an
   upgrade note for anything that breaks. Do not paste commit subjects.
5. `gh release view vX.Y.Z` to confirm the body, and that the tag is on the
   commit you verified.

## Verifying visuals

There is no screenshot test. `../examples/gallery` is the manual test bed for
every component and page; `goicheck -layout` catches nodes that collapse to zero size,
which is the failure a syntax check cannot see.
