# Architecture

```
 .ui file
    │
    ▼
internal/syntax ── parse ──▶ AST (immutable, positions kept)
    │
    ▼
internal/schema ── validate ──▶ diagnostics          (no second AST)
    │
    ▼
internal/tree ── instantiate ──▶ node tree + id index
    │                              Prop[T], Event[E]
    ├──▶ internal/layout  (pure, Measurer interface)  ▶ bounds
    ├──▶ internal/paint   (pure)                      ▶ display list
    └──▶ internal/render  (WebGPU only)               ▶ pixels
                ▲
      internal/platform (GLFW window, input translation)
                ▲
              ui/ (public API, window handle, event loop, hot reload)
```

## Layers and what may import what

| Package | May import | Owns |
|---|---|---|
| `internal/syntax` | stdlib | lexer, parser, AST, diagnostics |
| `internal/schema` | syntax | element/property vocabulary, validation |
| `internal/tree` | syntax, schema | live nodes, props, events, id index |
| `internal/layout` | tree | flexbox-lite, measurement |
| `internal/paint` | tree | display list |
| `internal/text` | — | shaping, rasterization, glyph atlas |
| `internal/render` | paint, text, gogpu/wgpu | GPU pipelines, texture upload |
| `internal/platform` | go-gl/glfw | window, native handles, input |
| `internal/hotreload` | fsnotify | file watching |
| `ui` | all of the above | public API, event loop |

Nothing below `ui` imports `ui`, so there are no cycles. Only `render` and
`platform` talk to the GPU or the window system, which is what makes
`syntax`/`schema`/`tree`/`layout`/`paint`/`text` reachable from headless tests.

## Two-layer model

The AST is the parser's output and is never mutated: nodes keep a pointer to the
`*syntax.Element` they came from so hot reload can compare literals. All runtime
state — values, handlers, bounds, dirty flags — lives in the node tree.

Validation returns diagnostics only. Instantiation reads the syntax AST directly
and asks the schema for defaults, so there is no "validated AST" to keep in sync.

## Properties

`Prop[T comparable]` is a value cell that knows its owner and its dirty kind.
`Set` compares first, so assigning an identical value does nothing. Every node
publishes a `[]NamedProp` table — name, dirty kind, a boxed getter, a
literal setter and a `CopyFrom` — which is what hot reload and the builder use.
No reflection, no `map[string]any` per node.

## Dirty tracking

Two kinds: `DirtyPaint` (colors, radius) and `DirtyLayout` (size, text, padding,
visibility, flex, alignment). Layout dirt propagates to every ancestor, because a
size change can move them; a layout-dirty frame repaints everything. A property
change calls `Hooks.Marked`, which sets `App.needsFrame` and wakes the event
loop. The loop blocks in `WaitEvents` when nothing is pending, so an idle window
costs ~0% CPU.

## Threading

The node tree belongs to the UI goroutine. Everything public that touches it is
called from `Bind` or from a function passed to `App.Post`, which queues work and
posts an empty GLFW event to wake the loop. Hot reload parses and validates on
the watcher goroutine and posts only the tree swap.

## Rendering

The display list is plain structs (`Fill`, `Text`) with absolute coordinates and
an inherited clip rectangle. The renderer turns fills into instanced quads with
an SDF fragment shader (per-corner radius, border, anti-aliased edge) and text
into textured quads sampling a single-channel glyph atlas. One draw call per
kind.

## Hot reload

`Bind` runs again after every reload, so the app's own state must live outside
it. The framework carries only what it owns:

- widget state by id: `TextInput` contents and caret, focus;
- every property Go wrote, unless the new file changed that property's literal.

If the new file has errors the old tree keeps running and the diagnostics are
reported through `App.OnError` (or printed) with `file:line:col`.
