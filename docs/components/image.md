# Image

Draws a decoded bitmap. `src` is read at build time and cached by path.

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
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `src` | string | image path, resolved next to the `.goi` file |
| `fit` | string | `cover`, `contain` or `fill` |


## From Go

```go
img := ui.MustByID[*ui.Image](w, "cover")
img.Src.Set("assets/other.png")
```

Every element also accepts the [common properties](../components.md#common-properties).
