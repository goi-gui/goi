# SVG

Draws an SVG file. `currentColor` in the file is replaced by `color`, so one
icon can follow the theme instead of shipping a coloured copy per state.

[All components](../components.md)

## In a `.goi` file

```goi
SVG {
	src:    "assets/logo.svg"
	width:  48
	height: 48
}

SVG {
	id:     mark
	src:    "assets/check.svg"
	color:  primary
	width:  24
	height: 24
}

SVG {
	src:    "https://example.com/icon.svg"
	color:  secondary
	width:  32
	height: 32
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `src` | string | a path next to the `.goi` file, or an `http`/`https` URL |
| `fit` | string | `contain` (default), `cover` or `fill` |
| `color` | color | replaces the file's `currentColor`; a role name or a hex literal |

The file is parsed and rasterized with the SVG's own `viewBox` aspect, at most
1024 px on the long edge, so `fit` behaves the same as on [Image](image.md). If
`width` and `height` are omitted, the `viewBox` size is the element's intrinsic
size; a declared `width` or `height` overrides it.

`color` is a normal colour property, which means a Material role works:

```goi
SVG { src: "assets/check.svg" color: onSurfaceVariant }
```

When the scheme changes, the role resolves and the icon is rasterized again. A
colour's alpha becomes the drawing opacity, because SVG hex colours have no
alpha channel.

A local path is read and rasterized while the window is built, so the first frame
already has it. A remote `src` is fetched in the background exactly like an
`Image`: the node lays out at its declared size straight away and paints a faint
wash that breathes until the file lands. Nothing waits for the network — a slow
host cannot hold up the first frame or a hot reload.

What happens to a remote SVG:

- The URL is fetched once per app, not once per node, and the bytes are cached,
  so a reload (or a second colour) does not download it again.
- The download is capped at 32 MiB and times out after 20 seconds. Anything
  larger, a non-200 status or a parse failure is recorded in `LoadErr` on the
  node and the placeholder stops.
- Parsing and rasterizing happen on the UI goroutine once the bytes arrive, and
  the node is marked for layout because the `viewBox` may have changed its size.
- A fetch that lands after a hot reload fills in the new nodes; the tree is
  searched by `src` when the data arrives rather than holding old pointers.

## From Go

```go
icon := ui.MustByID[*ui.SVG](w, "mark")
icon.Color.Set(0xFF0000FF) // re-rasterized on the next frame
icon.Src.Set("assets/other.svg")
icon.Src.Set("https://example.com/other.svg")
```

`Color.Set` and `Src.Set` are picked up by the frame loop, so an icon can change
with interaction. A new `Src` re-reads (or re-fetches) the file and re-runs
layout when its `viewBox` changes the intrinsic size.

Every element also accepts the [common properties](../components.md#common-properties).
