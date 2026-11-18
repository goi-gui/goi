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
CGO_ENABLED=0 go build -tags "nofakecgo all" ./...
CGO_ENABLED=0 go vet -tags "nofakecgo all" ./...
gofmt -l .                                   # CI fails on any output
CGO_ENABLED=0 go test -tags "nofakecgo all" ./ui ./internal/syntax ./internal/schema ./internal/layout ./internal/paint ./internal/text ./internal/hotreload ./internal/lsp ./internal/goifmt
CGO_ENABLED=0 go test -tags "nofakecgo all" ./...  # the platform is purego too
go run -tags all ./cmd/goicheck -layout -ops ../examples/*/assets/ui/*.goi  # headless render check
go run ./cmd/goifmt -l .                                # CI fails on any output
```

- The build is **cgo-free only**, so `go test -race` is not available: the
  race detector needs cgo. Run the tests without it.
- `internal/platform` loads the SDL3 runtime
  library through purego, so the build needs no C toolchain, headers or
  `pkg-config`; the runtime library (libSDL3) is the only requirement. A cgo
  build is not supported: goffi and purego each ship a fake C runtime, so
  `CGO_ENABLED=0` and `-tags nofakecgo` are mandatory, and `nofakecgo` lets
  goffi use purego's copy. Optional features are opt-in (`-tags emoji`, `svg`,
  `mermaid`, `markdown`, `code`, `net`, `dialog`); `-tags all` turns them on,
  which is what the examples and the test commands use.
- The purego platform cross-compiles for Linux, Windows and macOS. There is no
  build tag per platform: `SurfaceHandles` asks SDL which driver it got.
- The examples are separate modules in `../examples`, next to this checkout.
  Each is a goi project (`cmd/main.go`, `goi.yaml`, `assets/ui/`), so
  `make -C ../examples gallery` is `cd gallery && goi run`. They pin the
  published release in their `go.mod`.
- The `../testing` project is the local **dev version**: its `go.mod` replaces
  `github.com/goi-gui/goi` with this checkout, so it runs unreleased changes.
  The examples, in contrast, pin the published release.
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

- `.goi` has no scripting: `if`/`else`, `for … range` and `switch … case` only
  choose which children show or repeat, and an expression picks a value
  (`app.dark ? "light_mode" : "dark_mode"`). Behaviour belongs in Go.
- A list comes from `for … range` over a bridge slice, whose rows are rebuilt by
  `ui.SetData`; `ui.Instantiate`/`ui.Fill` are for a fixed set of children.
- Colours as M3 role names (`theme.primary`, `theme.surfaceContainer`,
  `theme.onSurfaceVariant`) follow the scheme; a bare role is a diagnostic, and
  hex literals are deliberately fixed. Prefer roles.
- `spacing` and `gap` are the same cell; the only intentional alias.
- `Positioned` reserves `-1` for "auto", so animation slides use
  `translateX`/`translateY`, not `left`.

## Go-side conventions

- `Bind` runs again after every hot reload: state declared inside it resets.
  Keep app state outside `Bind` (see `../examples/counter/cmd/main.go`).
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
   upgrade note for anything that breaks. Do not paste commit subjects. If the
   release changed `goipls` (a property, its hover, completion or the checker),
   the notes open with the command that updates it:
   `go install github.com/goi-gui/goi/cmd/goipls@vX.Y.Z`.
5. `gh release view vX.Y.Z` to confirm the body, and that the tag is on the
   commit you verified.

## Verifying visuals

There is no screenshot test. `../examples/gallery` is the manual test bed for
every component and page; `goicheck -layout` catches nodes that collapse to zero size,
which is the failure a syntax check cannot see.
