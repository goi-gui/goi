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
- **Widgets in v1:** Text, Button, TextInput. `Image` is still deferred.
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

## Phase 11 — post-review fixes

- **Clipping moved into the fragment shader, not the scissor.** The first attempt
  batched ops by clip and used `SetScissorRect` with `FirstInstance`, which
  requires the `IndirectFirstInstance` feature and failed validation on Vulkan.
  Each instance now carries its clip rectangle and the shader discards fragments
  outside it: one draw call per pipeline, no per-run state, same visual result.
  A WGSL `any(vec < vec)` comparison also failed in naga's SPIR-V backend, so the
  test is written as four scalar comparisons. Both are worth remembering.
- **Colors are uploaded as 0..1 floats.** An earlier version passed 0..255 and
  every fill drew white; `rgba()` divides by 255 in one place.

## Phase 12 — Material 3 components

- **Components are core elements, not a package.** The 40 component kinds are
  built-in .ui elements (`Button`, `Chip`, `Dialog`, …) so a file needs no
  imports and `ByID` stays type-checked (`*ui.Button`). `internal/componentdef`
  is the single list of kinds and their property subsets; `schema` validates
  against it and `tree` builds prop tables from it, so they cannot drift.
  `internal/schema/components_test.go` writes every kind into a file and fails if
  one does not validate.
- **One shared `Component` state, per-kind props.** All components embed
  `tree.Component` (label, icon, variant, checked, value, events, interaction
  state). Each kind exposes only its own subset through `propsFor`, keeping the
  "unknown property" diagnostics honest.
- **Bundled Roboto 400/500/700 and Material Symbols Outlined, not system fonts.**
  M3's metrics and icon names depend on them. Both are fetched from the Google
  Fonts CDN as `woff` (which go-text parses) and embedded; Roboto covers all
  Turkish glyphs, Material Symbols is looked up by ligature (`home`, `add`, …),
  so no icon-name table is needed. Atlas keys now include the font tag, because
  three Roboto weights and the icon font share one atlas.
- **Theme tokens live in `internal/m3`.** Baseline light and dark schemes, the
  full type scale, the shape scale and elevation constants. Every painter reads
  those, so a window's `theme: dark` swaps the whole scheme. Custom/dynamic
  palettes are not exposed.
- **Display list grew `Shadow`, `Circle` and `Arc`.** The shader kind now
  distinguishes filled circle, ring/arc and shadow, instead of treating every
  non-rect as a ring. Shadows grow the quad by the blur radius and evaluate the
  SDF against the original shape, so the falloff reaches zero at the quad edge
  and the shape drawn on top hides the solid middle.
- **Container components reuse the Row/Column layout.** `ButtonGroup`, `Toolbar`,
  `Carousel`, `Tabs`, `NavigationBar` run their children horizontally;
  `NavigationRail` and `NavigationDrawer` vertically; `Tabs` and `NavigationBar`
  split the width equally. This kept the layout engine at one algorithm instead
  of a second widget layout system. `internal/layout` decides horizontal flow
  from the kind, and intrinsic size sums children for a row and maximises for a
  column.
- **Text gets an M3 role.** `Text.role` selects a type-scale step; `fontSize` and
  `weight` still override it, so existing files keep working. Buttons and other
  components measure their labels with the same style they paint.
- **Vertical centering uses font metrics, not a guessed baseline.** A line box is
  centred when its baseline sits at `box centre + (ascent - descent) / 2`; for
  the bundled fonts that is 0.342em for Roboto and 0.5em for Material Symbols.
  Every label and icon goes through `textIn`/`iconIn`, so the same rule applies
  to buttons, fields, list items, tabs and pickers. Icons are glyphs of an icon
  font, so their ink lands in the same centred line box as text.
- **A navigation destination's arrangement comes from its parent kind, not its
  width.** Bar and rail items stack the icon over the label; drawer items put
  them in a row. Width was a proxy that broke as soon as a bar was wide enough
  for its equal-width items to look like a drawer.
- **A border is its own anti-aliased band.** The fill and the border are
  disjoint coverages blended in premultiplied space and converted back to
  straight alpha, so the shared edge is not darkened twice. The band uses its
  own signed distance (`abs(d + bw/2) - bw/2`) rather than being the leftover of
  the fill coverage: with the leftover, a 1dp border loses half its coverage
  along curves and turns into a dotted line at some radii.
- **Shape edges snap to whole device pixels in the renderer.** A 1dp border on
  a box at `y = 113.2` covers two rows at 90% and 10%, which reads as a broken,
  ragged line, and a 1dp divider turns into a two-row grey smear. `snapRect`
  rounds both edges to the device grid (half a logical pixel at 2x), so a border,
  divider or outline lands on whole rows. Neighbouring boxes snap from their
  absolute edges, so they stay flush.
- **Text rasterizes at device resolution and each glyph snaps to that grid.**
  Covering a fractional box centres a run at a fractional baseline; the linear
  sampler then smears every stem over two columns, and small or light text goes
  grey (measured: darkest pixel 73 instead of 28 on the same label). The face is
  created at `size * scale`, and the pen origin, each glyph's x and the baseline
  are rounded in device pixels, so a glyph covers exactly the texels the atlas
  holds. Advance widths keep the shaper's spacing; each glyph moves by at most
  half a device pixel.
- **Interaction stays in the input layer.** Hover/pressed/focused are plain
  fields on `Component`; painters draw the state layer (8/10/10% of the
  foreground), and `disabled` applies M3's 38%/12% treatment.
- **Scrolling is a common `scroll: true` property, not a Scroll element.** A
  `Row` scrolls horizontally and every other element vertically, so the axis
  follows the container's flow instead of a second `axis` property; the offset
  lives on `NodeBase` (`SetScrollOffset`/`ScrollOffset`) and paint/hit-testing
  shift children by the same amount. The content clips to the content box,
  which is what makes the container look scrollable. The layout does not grow a
  scroll container: it still asks for a bounded main-axis size (`flex: 1` or
  `height`), like Flutter. `ScrollOffset` clamps on read rather than on write,
  so an offset set before a relayout comes back when the content grows again.
  Wheel and drag both scroll the nearest scrollable ancestor of the pointer;
  a drag past 4dp cancels the click it started on.
- **Ripples are driven by the frame clock, not by the painter.** `startRipple`
  records the press point, `tick(dt)` advances a 0.45s life (expand to 60%,
  then fade) on the UI goroutine, and the state layer maps the progress to a
  disc. The disc carries `ClipRadius` so it is rounded to the pill or card it
  sits in; without it the corners of a pressed pill show a square edge.
  The clock is wall time clamped to 0.1s per frame, so an idle app does not
  burn frames and a stall does not skip the animation.
- **What is intentionally shallow for now:** DatePicker and TimePicker render a
  month grid and a clock dial but do not edit a date; FABMenu renders the FAB and
  leaves its expanded item list to app content; Slider drags but has no keyboard
  handling. There is no scrollbar, no fling/inertia and no mouse-wheel axis
  other than the container's. These are visual and behavioural follow-ups, not
  missing elements.
- **Not in v1 still:** text selection, clipboard and IME; `Image`, scrollbars
  and full accessibility. The previous phase notes that say "Checkbox deferred"
  are superseded: Checkbox, RadioButton, Switch and the rest of the component set
  now exist.
