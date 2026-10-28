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

        Text   { id: label text: "0" fontSize: 32 color: #ffffff }
        Button { id: inc   text: "Increase" background: #3b82f6 color: #ffffff }
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
| `Window` | yes | `title` |
| `Column` | yes | `spacing`, `align` |
| `Row` | yes | `spacing`, `align` |
| `Stack` | yes | `align` |
| `Rectangle` | no | — |
| `Text` | no | `text`, `fontSize`, `color`, `align`, `wrap` |
| `Button` | no | `text`, `fontSize`, `color`, `hoverBackground` |
| `TextInput` | no | `text`, `placeholder`, `fontSize`, `color` |

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
event declarations — behaviour lives in Go. No animations, theming, custom
components or imports (planned for v2).
