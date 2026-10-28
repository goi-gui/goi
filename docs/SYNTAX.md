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
Value     = String | Number | Bool | Color | Ident
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

## Elements

| Element | Children | Properties beyond the common set |
|---|---|---|
| `Window` | yes | `title`, `theme` (`light`, `dark`) |
| `Column` | yes | `spacing`, `align` |
| `Row` | yes | `spacing`, `align` |
| `Stack` | yes | `align` |
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

## Rules and diagnostics

- Exactly one root element, and it must be `Window`.
- `id` must match `[a-z][A-Za-z0-9_]*` and be unique within the file.
- Unknown element, unknown property, wrong literal type, unknown enum value and
  duplicate ids are errors, each reported with `file:line:col`, the offending
  line and a caret. A small edit distance adds a "did you mean" hint.
- The parser recovers, so one typo does not hide the rest of the file.

## What is not in v1

No expressions, no bindings, no loops, no conditionals, no scripting, and no
event declarations — behaviour lives in Go. No animations, custom components or
imports (planned for v2). Theming is limited to the built-in light and dark
Material 3 schemes; dynamic color and custom palettes are not exposed yet.
