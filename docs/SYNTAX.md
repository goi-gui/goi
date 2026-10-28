# .ui syntax (v1)

A `.ui` file describes one window. Go code attaches behaviour; the file holds no
logic.

```
// line comment
/* block comment */

Window {
    id: main
    title: "Counter"
    width: 480
    height: 320
    background: #1b1d22

    Column {
        padding: 16
        spacing: 8
        align: center

        Text   { id: label text: "0" role: displayLarge }
        Button { id: inc   label: "Increase" icon: add }
    }
}
```

## Grammar

```
File      = Element EOF
Element   = Ident "{" { Member } "}"
Member    = Property | Element
Property  = Ident ":" Value [";"]
Value     = String | Number | Bool | Color | Ident | Map
Map       = "{" [ MapEntry { "," MapEntry } [","] ] "}"
MapEntry  = Ident ":" Value
```

Newlines are not tokens. A member is a property when the identifier is followed
by `:` and a child element when it is followed by `{`, so `name: value` and
`Name { ... }` can be written on one line or across many.

## Literals

| Form | Type | Notes |
|---|---|---|
| `"text"` | string | `\" \\ \n \t \r \uXXXX` escapes; `\uD83D\uDE00` surrogate pairs work |
| `hello` | identifier | used for enums and `id`; also accepted where a string is expected |
| `480`, `32.5`, `-1` | number | one numeric type everywhere (`float64`) |
| `true`, `false` | bool | |
| `#rgb`, `#rrggbb`, `#rrggbbaa` | color | alpha defaults to `ff`; `"#rrggbb"` quoted also works |
| `{ base: 8, md: 16 }` | map | responsive value, one entry per breakpoint |

## Elements

| Element | Children | Properties beyond the common set |
|---|---|---|
| `Window` | yes | `title`, `theme` (`light`, `dark`) |
| `Column` | yes | `spacing`, `align` |
| `Row` | yes | `spacing`, `align` |
| `Stack` | yes | `align` |
| `Positioned` | yes | `left`, `top`, `right`, `bottom` |
| `Wrap` | yes | `spacing`, `align` |
| `Grid` | yes | `columns`, `spacing`, `align` |
| `Rectangle` | no | — |
| `Text` | no | `text`, `fontSize`, `weight`, `color`, `align`, `wrap`, `role` |

`role` selects a Material 3 type-scale role (`displayLarge` … `labelSmall`) and
sets size, line height and weight together; `fontSize` and `weight` override it.

`Wrap` runs children left to right and starts a new run when the next child no
longer fits; `spacing` is both the gap inside a run and the gap between runs.

`Grid` puts children in `columns` equal-width cells. A cell is as wide as the
grid allows, so a child fills it unless it declares its own `width`; each row is
as tall as its tallest child and `spacing` separates both columns and rows.

`Stack` overlays its children, last on top. A child without a declared size
fills the stack, so the first child is often a background. `Positioned` is only
valid as a direct child of `Stack` and anchors itself instead of filling:

```
Stack {
    width: 360
    height: 200
    background: #E8DEF8
    radius: 16

    Positioned { left: 16 top: 16 width: 180 height: 88 }   // top-left corner
    Positioned { right: 16 bottom: 16 Chip { label: "New" } } // bottom-right
    Positioned { left: 0 right: 0 top: 0 height: 48 }       // across the top
}
```

`left`, `top`, `right` and `bottom` default to `-1`, meaning auto. An
unanchored axis follows the Stack's `align`. Setting both opposite edges
without a declared `width`/`height` stretches the box between them; an explicit
size wins over the fill. A box with no offsets hugs its child. Positioned
children still count toward a Stack's auto size (their anchored extent), so a
Stack inside a Column does not collapse; give the Stack a size when `right` or
`bottom` should anchor against a larger area. `Positioned` does not clip.

Everything else is a Material 3 component. They are ordinary elements, not a
separate package. Containers take children; the rest are leaves:

`Button`, `IconButton`, `FAB`, `FABMenu`, `ButtonGroup`, `SplitButton`,
`SegmentedButton`, `Chip`, `Badge`, `Checkbox`, `RadioButton`, `Switch`,
`Slider`, `TextField`, `SearchBar`, `Tooltip`, `Card`, `Dialog`, `BottomSheet`,
`SideSheet`, `Divider`, `List`, `ListItem`, `Menu`, `MenuItem`, `Snackbar`,
`TopAppBar`, `NavigationBar`, `NavigationRail`, `NavigationDrawer`,
`NavigationItem`, `Tabs`, `Tab`, `LinearProgress`, `CircularProgress`,
`LoadingIndicator`, `DatePicker`, `TimePicker`, `Carousel`, `Toolbar`.

They share one vocabulary: `label`, `supporting`, `text`, `placeholder`, `icon`,
`trailingIcon` (icon names are Material Symbols ligatures such as `home`),
`variant`, `size`, `disabled`, `checked`, `selected`, `expanded`, `value`,
`min`, `max`, `step`. Each kind accepts only the subset that makes sense for it;
`internal/componentdef` is the single table that the schema and the node tree
both read, so an element can never validate without existing.

Containers that lay children out (`ButtonGroup`, `Toolbar`, `Carousel`, `Tabs`,
`NavigationBar`, `NavigationRail`, `NavigationDrawer`, `List`, `Menu`, `Card`,
`Dialog`, `BottomSheet`, `SideSheet`, `DatePicker`, `TimePicker`) also accept
`spacing` and `align`.

Common to every element: `id`, `width`, `height`, `minWidth`, `maxWidth`,
`margin`, `padding`, `background`, `borderColor`, `borderWidth`, `radius`,
`visible`, `flex`, `alignSelf`.

`align` is one of `start`, `center`, `end`, `stretch`. `alignSelf` uses the same
values and overrides the parent's `align` for one child.

`width` and `height` default to `-1`, meaning automatic. `visible: false`
removes the element from layout and from hit-testing entirely.

## Responsive values

Any property except `id` also accepts a map keyed by breakpoint:

```
Column {
    padding: { base: 8, md: 16 }
    Grid {
        columns: { base: 1, md: 2, lg: 3 }
        Card { label: "One" }
        Card { label: "Two" }
    }
}
```

Breakpoints are min-widths in logical pixels, like Tailwind. The entry with the
largest minimum that is not above the window width wins.

| Tier | Applies from |
|---|---|
| `base` | 0 |
| `sm` | 640 |
| `md` | 768 |
| `lg` | 1024 |
| `xl` | 1280 |

`{ base: 1, md: 2, lg: 3 }` is therefore 1 below 768, 2 from 768 and 3 from
1024. The `base` entry, when present, is applied as soon as the file is loaded,
so it also sizes the initial window when used on `Window.width`.

`base` is optional. Without it the property keeps its normal default below the
lowest tier that is present. `id` cannot be responsive and a tier cannot contain
another map.

The tree stores the map; the frame loop rewrites those properties before layout
whenever the window width changes. The file owns a responsive property: a value
set from Go with `Prop.Set` is replaced on the next frame.

## Rules and diagnostics

- Exactly one root element, and it must be `Window`.
- `id` must match `[a-z][A-Za-z0-9_]*` and be unique within the file.
- `Positioned` must be a direct child of `Stack`.
- Unknown element, unknown property, wrong literal type, unknown enum value,
  unknown or repeated breakpoint, nested map, responsive `id` and duplicate ids
  are errors, each reported with `file:line:col`, the offending line and a
  caret. A small edit distance adds a "did you mean" hint.
- The parser recovers, so one typo does not hide the rest of the file.

## What is not in v1

No expressions, no bindings, no loops, no conditionals, no scripting, and no
event declarations — behaviour lives in Go. No animations, custom components or
imports (planned for v2). Theming is limited to the built-in light and dark
Material 3 schemes; dynamic color and custom palettes are not exposed yet.
