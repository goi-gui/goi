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
  implemented; selection, clipboard and IME commits were added later (see the
  text-field notes near the end of this file).
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
  `height`), like Flutter, and a flex child now gives space back when its
  content overflows, so `flex: 1` bounds the page instead of growing to fit it. `ScrollOffset` clamps on read rather than on write,
  so an offset set before a relayout comes back when the content grows again.
  Wheel and drag both scroll the nearest scrollable ancestor of the pointer;
  a drag past 4dp cancels the click it started on.
- **Ripples are driven by the frame clock, not by the painter.** `startRipple`
  records the press point, `tick(dt)` advances a 0.45s life (expand to 60%,
  then fade) on the UI goroutine, and the state layer maps the progress to a
  disc. The disc is clipped to the component's own box, not only to its
  ancestors (the page), and carries `ClipRadius` so it is rounded to the pill or
  card it sits in; without either, the disc spills past the button while it
  expands or shows a square edge at the corners.
  The clock is wall time clamped to 0.1s per frame, so an idle app does not
  burn frames and a stall does not skip the animation.
- **Text selection is a pair of byte offsets on `Component`.** `Caret` is the
  moving end and `SelAnchor` the fixed one (-1 when nothing is selected); both
  snap to rune boundaries, so Turkish characters cannot be split. The painter
  draws the band in the value box, the caret from the same shaped advances, and
  input maps pointer x by measuring prefix widths with the same measurer, so
  clicks land between the glyphs they follow. Click places the caret,
  shift-click and shift+arrows extend, drag extends from the press, double click
  selects a rune class (word, number, punctuation) and triple click selects all.
- **Clipboard goes through the platform, not a framework cache.** Ctrl+C/X/V
  copy, cut and paste the selected range (paste replaces it); the platform layer
  already wraps GLFW's clipboard. Paste flattens newlines to spaces because a
  TextField is one line. Copy with no selection is a no-op, and the interactor
  takes get/put hooks so tests do not touch the real clipboard.
- **IME: commits are supported, composition preview is not.** GLFW on Linux
  delivers only committed text (through the char callback), so the framework
  treats every committed rune as insertion at the caret over the selection;
  dead keys and composed characters therefore arrive as normal text. Preedit,
  the underlined composing run and the candidate window need a native
  text-input bridge (Wayland `zwp_text_input_v3`, XIM on X11) that GLFW does not
  expose; a long value also does not scroll horizontally inside its field yet.
- **Wrap and Grid are layout containers, not new components.** `Wrap` uses the
  existing `spacing` for the gap inside a run and between runs, so there is no
  second knob; `align` places shorter children inside their run (`stretch`
  fills it). `Grid` takes a
  `columns` count (default 2) and makes each cell tight in width like
  `GridView`, while a row is as tall as its tallest child (no fixed aspect
  ratio). Unlike Material components they have no intrinsic width in a Row
  beyond the sum of their cells. Their height depends on the width they actually
  receive, so the layout recomputes it after the cross size is known rather than
  trusting the parent's available width.
- **Positioned is a Stack child, not a layout of its own.** Like Flutter, the
  box anchors to the Stack's content edges (`left`/`top`/`right`/`bottom`, `-1`
  meaning auto), stretches between two opposite edges only when it has no
  declared size, and otherwise hugs its child or follows the Stack's `align` on
  an unanchored axis. It is a real element rather than parent data on the child,
  because `.ui` has no slot for parent-owned attributes; the validator enforces
  that it sits directly in a Stack. Positioned children still contribute their
  anchored extent to a Stack's auto size, unlike Flutter, so a stack made only
  of positioned children does not collapse to zero inside a Column. It does not
  clip, so shadows inside a positioned box paint.
- **Responsive properties are maps, applied by the frame loop.** A property may
  be `{ base: ..., sm: ..., md: ..., lg: ..., xl: ... }` on Tailwind's min-width
  scale (640/768/1024/1280 logical px). The builder records the map and applies
  `base` immediately, so a headless tree and the initial window size are
  sensible; `Tree.ApplyBreakpoints(width)` then picks the largest tier at or
  below the width and marks the node dirty only when the value actually changes.
  `id` stays scalar, maps do not nest, and the node's pre-map value (its
  constructor or schema default) is the fallback when `base` is absent. The file
  owns a responsive property: `Prop.Set` cannot win, because the next frame
  rewrites it. Maps are syntax, not a new property type, so every existing
  element accepts them without schema changes.
- **Expressions are re-evaluated on each layout, not wired as observers.** A
  numeric property may be a reference (`id.prop`), arithmetic (`+ - * /`,
  parentheses) or a percentage. A reference reads the target's property cell,
  so `abc.width / 2` follows a Go `Set` or a resize on the next frame without a
  binding registry or subscription lifetime to manage; a cycle simply fails to
  evaluate and leaves the previous value. Percentages are parsed as their own
  literal and resolved during layout, because the parent's content box is only
  known then; the validator limits them to `width`/`height` and rejects a
  reference to a percentage-valued property, which would depend on a different
  parent. Expressions and responsive maps share one write path: the selected
  value is stored and applied only when it changes, so a breakpoint can switch
  between a number and an expression without leaving both active.
- **Visibility is a common property, not a per-component flag.** `visible` is a
  `Prop[bool]` on `NodeBase`, so every element and Material component accepts
  `visible: false` and breakpoint maps, and Go can call `SetVisible` on any node.
  Layout, paint, hit-testing and scroll extent read the same cell, so a hidden
  component is fully absent rather than merely painted transparent.
- **Only surfaces clip their children, not layout flow.** `NodeBase.ClipsChildren`
  is true for scroll viewports, `Window`, `Stack` and painted components; `Column`,
  `Row`, `Toolbar` and `ButtonGroup` let content spill. Clipping every node to its
  content box cut the shadow of any elevated component in a content-sized row
  and the focus ring at the edge of the column it sat in. A scrolling column
  still clips its viewport, so content that needs shadow room keeps a padding
  (the gallery columns use 16dp).
- **Split button is two segments, not one pill with a divider.** M3 Expressive
  uses a 2dp gap, a 48dp menu segment and different corner radii per segment:
  full at the outer ends and 4dp at the gap. Each segment gets its own fill,
  shadow and state layer; the trailing segment shows `arrow_drop_down`. Hover
  and press only move the state layer, never the radii. Both segments share one
  `Click`, so pressing either runs the same handler.
- **Slider follows the Expressive XS geometry.** The 16dp track is split by a
  6dp gap on each side of the handle, with full radius at the outer ends and 2dp
  at the gap. The handle is a 4x44dp pill that narrows to 2dp while pressed or
  focused, and the inactive end carries the 4dp stop dot. Pointer values map
  across the handle-centre range, not the old 10dp inset.
- **Segmented buttons are one group per contiguous run of siblings.** The
  default is multi-select: each segment toggles, so a second click clears it.
  `variant: single` gives radio behaviour: picking a segment clears the others,
  and the selected segment cannot be cleared by clicking it again. The group
  rounds only the run's outer ends, so a label next to a group does not break
  the shape.
- **Overlays are painted in display-list order, not by pipeline.** Shapes and
  glyphs used to go to two separate pipelines and were drawn shapes-then-text, so
  every label in the window landed on top of a later drawer or dialog. Glyph
  quads now share the shape instance buffer as kind 4 with the atlas bound once,
  which keeps the window at one draw call and makes tree order the paint order.
- **A modal navigation drawer is app state plus three plain elements.** The
  `.ui` file holds a `Positioned` drawer and a scrim `Rectangle` with
  `visible: false`; Go toggles `SetVisible`. Any node now carries `OnClick`, so
  the scrim and the app-bar icon are hit areas without becoming components, and
  `win.OnKey` closes the drawer on Escape even when nothing is focused.
  `NavigationDrawer` itself stays a painted panel: the framework does not own
  open/closed state.
- **SearchBar edits like a TextField.** It shares the field hit-testing, focus,
  caret mapping and selection, but starts its text 52dp in (after the search
  icon) and paints the selection band on the same 20dp line box as the caret.
  The mapping lives once in `fieldTextStart`, so pointer and paint cannot
  disagree. Search has no floating label band, so the caret is vertically
  centred instead of sitting in the value row of a TextField.
- **Text fields scroll their value instead of overflowing.** `Component.TextScroll`
  is the horizontal shift paint computes each frame so the caret stays inside
  the text box; glyphs and the selection band are clipped to that box, and
  `fieldTextStart` subtracts the same shift, so a click maps to the character
  actually under the pointer. Unfocused fields show the start of the value.
  SearchBar uses the same rule with its own 52dp inset.
- **Dragging a selection past the edge auto-scrolls.** While a text selection
  drag is live, `tick` keeps moving the x used for caret mapping at 600dp/s
  when the pointer is outside the field's text box, so text that started
  off-screen can be selected without releasing. Only that mapping x moves, not
  the pointer, so releasing still selects what it points at. The field's own
  text box comes from `fieldTextBox`, shared with the caret mapping.
- **Word editing follows the Linux convention.** Ctrl+Left/Right move the caret
  by word; Ctrl+Backspace and Ctrl+Delete remove one word plus its separator
  per press, and Alt works as the second word-delete modifier. A live selection
  still wins over the modifier, so Ctrl+A then delete behaves like any editor.
- **IME composition is a separate field, not part of the value.** `Component.Preedit`
  and `PreeditCaret` hold text the input method has not committed; paint draws
  it at the caret with an underline, the caret sits inside it, and the
  caret-follow scroll counts its width. It only enters `Text` on commit, so a
  cancelled composition leaves no trace and nothing downstream sees half-chosen
  characters. `DeleteSurrounding` is implemented because an IME corrects text it
  already committed.
- **The IME connection is bound on GLFW's own Wayland display.** `zwp_text_input_v3`
  is a second client on the display GLFW already owns, so there is no extra
  connection or event loop. The caret rectangle is pushed on focus and after
  every edit so the candidate window sits next to the text. `internal/platform/ti3.c`
  is wayland-scanner output with one change: the core `wl_surface`/`wl_seat`
  interface pointers are resolved with `dlsym` at runtime, because Go's internal
  linker cannot resolve those data relocations and this project needs internal
  linking for WebGPU. Other platforms get a no-op.
- **Text wrap is greedy word breaking, shared by layout and paint.** `wrap: true`
  breaks the value at spaces against the element's width (or the parent's when
  no width is declared), and a single word wider than the column is broken
  between runes. `text.WrapLines` is the one implementation: layout asks it for
  the line count to reserve height, paint asks it for the lines to draw, so the
  block never clips its own last line. Runs of spaces collapse to one, which is
  right for UI copy and not for preformatted text.
- **Weights snap to a loaded face.** The library carries Roboto 300, 400, 500,
  700 and 900; `text.NearestWeight` picks the closest for any declared value, and
  both the measurer and the painter call it, so a `weight: 600` measures and
  draws with bold instead of measuring regular and drawing bold.
- **Glyph fill is nonzero winding, not even-odd.** TrueType outlines overlap
  themselves: the stem and crossbar of a "t" are separate contours that cross,
  and a bowl meets its stem. An even-odd fill counts the overlap as a hole, so
  every place two strokes meet showed a white gap. The rasterizer now tracks the
  winding direction of each scanline crossing and fills where the winding is
  nonzero; counters still carve because they are wound the opposite way. Two
  tests cover it: an artificial overlap must be solid, and a rasterized "t" must
  have no transparent pixel enclosed by ink.
- **What is intentionally shallow for now:** DatePicker and TimePicker render a
  month grid and a clock dial but do not edit a date; FABMenu renders the FAB and
  leaves its expanded item list to app content; Slider drags but has no keyboard
  handling. There is no scrollbar, no fling/inertia and no mouse-wheel axis
  other than the container's. These are visual and behavioural follow-ups, not
  missing elements.
- **Not in v1 still:** `Image`, scrollbars and full accessibility. IME
  composition now shows as underlined preedit on Wayland; per-clause styling of
  the composition (the protocol's cursor range) is drawn as one run. The previous
  phase notes that say "Checkbox deferred" are superseded: Checkbox,
  RadioButton, Switch and the rest of the component set now exist.
