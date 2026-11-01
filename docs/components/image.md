# Image

Draws a decoded bitmap. `src` is either a path next to the `.goi` file or an
`http`/`https` URL, and either way the element takes part in layout immediately:
the bitmap arrives when it arrives.

[All components](../components.md)

## In a `.goi` file

```goi
Image {
	src: "assets/cover.png"
	fit: cover
	width: 240
	height: 135
	radius: 12
}

Image {
	src: "https://picsum.photos/600/400"
	fit: cover
	width: 240
	height: 135
	radius: 12
	background: surfaceContainerHighest
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `src` | string | a path next to the `.goi` file, or an `http`/`https` URL |
| `fit` | string | `cover`, `contain` or `fill` |

A local path is read and decoded while the window is built, so the first frame
already has it. A remote `src` is fetched in the background: the node lays out at
its declared size straight away and paints a faint wash that breathes until the
bitmap lands. Nothing else waits for the network — a slow host cannot hold up the
window's first frame or a hot reload, and a broken URL leaves a coloured
placeholder instead of a hole.

What happens to a remote image:

- The URL is fetched once per app, not once per node: two `Image` elements with
  the same `src` share one request, and the decoded bitmap is cached so a reload
  does not fetch it again.
- The download is capped at 32 MiB and times out after 20 seconds. Anything
  larger, a non-200 status or a decode failure is recorded in `LoadErr` on the
  node and the placeholder stays.
- Decoding happens off the UI goroutine, and the result is handed back with
  `App.Post`, so a large JPEG cannot drop a frame.
- A fetch that lands after a hot reload fills in the new nodes; the tree is
  searched by `src` when the data arrives rather than holding old pointers.

## From Go

```go
img := ui.MustByID[*ui.Image](w, "cover")
img.Src.Set("https://example.com/other.png")
```

`Src.Set` only updates the string: the bitmap is loaded when the tree is built or
reloaded, not on every property change. Point at the URL in the `.goi` file, or
reload the design after setting it.

Every element also accepts the [common properties](../components.md#common-properties).
