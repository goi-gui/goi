# Decisions

One entry per phase: what was decided, what else was considered, why.

## Phase 0 — tech freeze

**WebGPU bindings: `github.com/gogpu/wgpu` v0.34.5 (pure Go).**

Evaluated:
- `cogentcore/webgpu` — unmaintained, its README points elsewhere. Rejected.
- `go-webgpu/webgpu` — works, but crashes: `wgpu-native` panics with
  `invalid callback` in `wgpuInstanceRequestAdapter`. The callback-passing path
  (goffi callbacks reconstructed from the wrong ABI) is broken in v0.5.5 +
  goffi v0.6.7 on linux/amd64. Reproduced with `RequestAdapter(nil)` alone, so
  it is not our surface code.
- `gogpu/wgpu` — same author's successor: pure Go HAL (Vulkan/Metal/DX12/GLES)
  with WebGPU-shaped API, no `wgpu-native` .so needed, no callback FFI.
  Instances, adapters and device creation are direct calls. Chosen.

Consequence: `wgpu.Init()` does not exist; `CreateInstance(nil)` is enough.

**Linking: `-ldflags=-linkmode=internal` on Linux when any cgo package is
present.** GLFW is cgo, which forces the external linker by default, and the
external linker rejects goffi's `//go:cgo_import_dynamic` relocations
(`unhandled relocation ... SDYNIMPORT`, upstream goffi#72). Internal linking is
the documented workaround. Keep it in the example build commands and later in
`make` targets; the framework itself must not require it once the GLFW package
is separated. Also `CGO_ENABLED=0` + `-tags nofakecgo` is the pure-Go path if it
is ever needed.

**Surface formats: pick a non-sRGB unorm format** (`BGRA8Unorm`, else
`RGBA8Unorm`). sRGB formats darken/mute authored hex colors unless the shader
converts, and a UI toolkit wants raw literal fidelity. Verified visually at 1x
and simulated 2x.

**Surface creation:** Linux uses `CreateSurfaceUnsafe` with an explicit
`SurfaceTargetFromWaylandSurface` / `SurfaceTargetFromXlibWindow` built from
GLFW's native handles. Wayland is the default build; X11 is `-tags x11`. Do not
rely on `CreateSurface`'s environment sniffing — it guesses from `XDG_SESSION_TYPE`.

**HiDPI:** framebuffer size drives the surface, `GetContentScale` compensates.
The whole layout is in logical pixels; the viewport uniform carries the scale so
the fragment shader can widen AA by `0.5/scale`.

**Rendering approach:** instanced quads (6 vertices, one instance per rect) with
an SDF fragment shader doing per-corner radii, borders and AA. One draw call per
batch. This is what the display list will feed.

**Known issues / unverified:**
- darwin/arm64 and windows/amd64 were not built here (no machine). The pure-Go
  HAL supports them per its CI, but treat as unverified until someone builds.
- Wayland surface must be created *after* the window is shown; creating it on a
  hidden window yields a nil `wl_surface` under GLFW.
- `wgpu-native`-style `SurfaceTexture.Release` does not exist; the surface owns
  it (nothing to release per frame).

## Design decisions locked before Phase 1

- **Single source of truth for validation output:** semantic validation returns
  diagnostics only. There is no second "validated AST" type; instantiation reads
  the syntax AST and asks the schema for default values.
- **Hot reload keeps app state in the app, not the framework.** `Bind` re-runs
  after every reload, so app state must be declared outside it. The framework
  only carries widget-internal state (focus, TextInput value/caret, scroll
  offset) and Go-set property values whose `.ui` literal did not change.
- **Widgets in v1:** Text, Button, TextInput. Checkbox/Scroll/Image deferred.
- **`opacity` deferred** — it needs offscreen layers to be correct.
- **Numbers are `float64`**, colors are `uint32` (0xRRGGBBAA).
- **`visible: false`** removes the node from layout and hit-testing entirely.

## Phase 1–3 — parser, schema, tree

- **No newline tokens.** The parser distinguishes `name: value` from
  `Name { ... }` with two-token lookahead. Fewer token kinds, and one-line
  elements work.
- **Validation returns diagnostics only**, plus a map of element → id. No
  "validated AST": instantiation reads the syntax AST and asks the schema for
  defaults, so there is one model, not two.
- **`Prop[T comparable]`** so `Set` can skip no-ops; numbers are all `float64`,
  colors are `uint32` (0xRRGGBBAA).
- **Per-node concrete fields, not a `map[string]any`.** A `[]NamedProp` table
  (name, dirty kind, boxed getter, literal setter, CopyFrom) is built once and
  only when something asks for it — that is what hot reload and the builder use.
- **`ui.Window` is a handle, not a node**; the root node is `ui.WindowNode`. The
  handle is what `Bind` receives, so `ByID` takes the handle and the node type is
  a separate generic parameter.
- **`Container` is shared** by Column/Row/Stack, with thin named types so
  `ByID[*ui.Column]` is type-checked.

## Phase 4 — layout

- **Own flexbox-lite.** No cgo Yoga, no dependency. Padding and border are
  applied once per node in `children()`, which fixed a class of double-inset bugs.
- **Auto-sized containers measure their children**; only `Window` fills its
  parent. Everything else with no declared size is "hug content".
- **A child of the root fills the client area** when it declares no size, which
  is what makes `flex: 1` on a top-level Column behave as expected.
- Layout is pure and takes a `Measurer`; tests use `FakeMeasurer`, the app passes
  a real one backed by the text library.

## Phase 5–8 — paint, render, text

- **One rectangle pipeline, one text pipeline, one draw call each.** Fills are
  instanced quads with an SDF fragment shader (per-corner radius, border, AA
  widened by `0.5/scale`). Text is textured quads over one `R8Unorm` atlas.
- **Colors are converted to 0..1 floats at upload time.** (A first cut passed
  0..255 floats and every fill rendered white — worth remembering.)
- **Non-sRGB surface format**, so authored hex colors are exact.
- **Grayscale atlas, not SDF.** Rasterizer is a small scanline fill with 4x
  vertical supersampling. `shortcut:` in the code marks the missing eviction; an
  atlas overflow skips a glyph instead of corrupting the texture.
- **Clipping is recorded per op but not yet applied by the renderer.** Layout
  keeps children inside their parent, so the visible result matches; real
  scissoring is the next renderer task. Marked `shortcut:` in `Render`.
- **Text is measured by the real shaper** in the app and by `FakeMeasurer` in
  layout tests, so layout tests stay headless and font-free.

## Phase 6–7 — input and dirty rendering

- The loop is: poll, frame, then block in `WaitEvents` only when nothing is
  pending. That is what keeps an idle window at ~0% CPU (measured 0 frames while
  idle).
- Hit-testing walks children last-first with the inherited clip rect, and press
  and release must land on the same node.
- Layout dirt propagates to ancestors; a layout-dirty frame repaints everything.

## Phase 9 — widgets

- **Text, Button, TextInput only in v1.** Checkbox, Scroll and Image are
  deferred; the schema and paint layer are ready for them.
- `TextInput` caret arithmetic is byte-based on rune boundaries, so Turkish
  characters do not split and shift the caret correctly.
- Text editing (insert, backspace, delete, arrows, home/end, Enter) is
  implemented; selection, clipboard and IME arrive with v1.1.
- `hoverBackground` is a property; the input layer only sets the hover flag.

## Phase 10 — hot reload

- The watcher watches the **directory**, not the file, so editors that save by
  rename keep working. 80ms debounce.
- Parsing and validation run on the watcher goroutine; only the swap is posted.
- On errors the last good tree stays and `App.OnError` (or stderr) gets a
  `file:line:col` diagnostic.
- **`Bind` runs again after every reload**; app state lives outside it. The
  framework carries by id: focus, `TextInput` text and caret, and every property
  Go wrote whose `.ui` literal did not change. This is covered by
  `TestCounterSurvivesReload`.

## Phase 11 — docs and CI

- CI is a native build matrix (ubuntu/macos/windows), not a cross-compile check:
  GLFW is cgo, so "compiles for" is not "works on".
- Known unverified: no macOS or Windows machine was available while building
  this, so those two are exercised only by CI.
