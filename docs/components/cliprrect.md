# ClipRRect

Cuts its children to a rounded box.

[All components](../components.md)

## In a `.goi` file

```goi
ClipRRect {
	radius: 24
	width: 96
	height: 96
	Image { src: "avatar.jpg" fit: cover }
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `radius` | number | corner radius of the cut, in dp |
| `align` | enum | placement of a child smaller than the box |
| `padding` | number | inset the cut from the box |

`ClipRRect` takes children like `Stack`: an undeclared child fills the box, and
a `Positioned` child can be anchored inside it.

## Behaviour

Everything the children draw is clipped to the box, corners included. A child
that is larger than the box keeps its size and is cut, not scaled — that is the
difference between this and a rounded `Image`.

A `radius` of 0 still clips to the box; use it to stop a scrolled row or an
overflowing child from leaking past its container.

## From Go

```go
clip := ui.MustByID[*ui.ClipRRect](w, "avatar")
clip.Radius.Set(48)
```

Every element also accepts the [common properties](../components.md#common-properties).
