# SVG

Draws an SVG file. `currentColor` in the file is replaced by `color`, so one
icon can follow the theme instead of shipping a coloured copy per state.

[All components](../components.md)

## In a `.goi` file

```goi
SVG {
	src: "assets/logo.svg"
	width: 48
	height: 48
}

SVG {
	id: mark
	src: "assets/check.svg"
	color: primary
	width: 24
	height: 24
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `src` | string | a path next to the `.goi` file |
| `fit` | string | `contain` (default), `cover` or `fill` |
| `color` | color | replaces the file's `currentColor`; a role name or a hex literal |

The file is read, parsed and rasterized once while the window is built. The
raster uses the SVG's own `viewBox` aspect and a fixed resolution (at most
1024 px on the long edge), so `fit` behaves the same as on [Image](image.md). If `width` and `height` are omitted,
the `viewBox` size is the element's intrinsic size; a declared `width` or
`height` overrides it.

`color` is a normal colour property, which means a Material role works:

```goi
SVG { src: "assets/check.svg" color: onSurfaceVariant }
```

When the scheme changes, the role resolves and the icon is rasterized again. A
colour's alpha becomes the drawing opacity, because SVG hex colours have no
alpha channel.

`src` must be a local asset. Unlike `Image`, an `http`/`https` URL is recorded in
`LoadErr` and the node keeps its background; fetching an SVG would need the
placeholder that only `Image` currently has.

## From Go

```go
icon := ui.MustByID[*ui.SVG](w, "mark")
icon.Color.Set(0xFF0000FF) // re-rasterized on the next frame
icon.Src.Set("assets/other.svg")
```

`Color.Set` and `Src.Set` are picked up by the frame loop, so an icon can change
with interaction. `Src.Set` re-reads the file and re-runs layout when the new
`viewBox` changes the intrinsic size.

Every element also accepts the [common properties](../components.md#common-properties).
