# .goi syntax (v1)

A `.goi` file describes a design. Go code attaches behaviour; the file holds no
logic. A design may be one file or several: a file can import fragments and pass
them values.

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
        crossAxisAlign: center

        Text   { id: label text: "0" role: displayLarge }
        Button { id: inc   label: "Increase" icon: add }
    }
}
```

## Splitting a design across files

A file may start with `import` lines and one `props` block, before its root
element. `props` declares the typed values the fragment expects, the way Go
declares a field; the fragment reads them as `props.Name`:

```
// Sidebar.goi
props {
    Title    string
    Subtitle string
}

Column {
    width: 280
    Text { text: props.Title role: "titleLarge" }
    Text { text: props.Subtitle role: "bodySmall" }
}
```

```
// Main.goi
import "./Sidebar.goi" as Sidebar

Window {
    Column {
        Sidebar {
            Title: "Inbox"
            width: 300
        }
    }
}
```

- The import path is relative to the **importing file** and must stay inside the
  asset container. Fragments may import fragments; a cycle is an error.
- `Sidebar { }` is replaced by the fragment's root element, so the fragment
  decides its own layout and the instance places it like any other child.
- A prop is `Name Type`, with no default: a prop the instance leaves out is the
  **type's zero value** (`""`, `0`, `false`, `#00000000`), so a fragment never
  has to guard for a missing value.
- The type is written the way Go writes it: a primitive, a slice (`[]Ticket`), a
  map (`map[string]int`), a handler (`func(id string)`) or a declared `type`.
- A slice prop is walked like a bridge list: `for i, row := range props.Rows`.
  The instance passes a bridge list — `List { Rows: bridge.Tickets }` — whose
  type must match the declaration.
- A handler prop is called like a bridge handler: `onClick:
  props.OnOpen(row.ID)`. The instance passes a handler — `Row { OnOpen:
  bridge.OnOpen }` — and `event` is not available inside the fragment, because
  the fragment decides what it passes.
- The language server completes a call site: inside `Counter { … }` it offers the
  props that fragment declares (with their Go types), the properties its root
  lets the instance override, and the slots it fills; the value after a prop
  completes like the declared type. Hover explains both the alias and the prop.
- A fragment can take a value and a handler from the same bridge in one call:
  `Count: bridge.Step OnStep: bridge.onStep`. The fragment shows the value and
  calls the handler, and never mentions `bridge` itself; see
  `examples/bridge/Counter.goi`.
- An instance property whose name is a prop sets that prop. Any other property
  is applied to the fragment root, replacing the fragment's own value of the same
  name. So `Sidebar { width: 300 }` overrides the fragment's width.
- `id` is the one shared name: a fragment that declares an `id` prop takes the
  instance's `id:` as that value, so instance-id scoping is off for it. Declare
  a different prop name when the instance needs its ids namespaced.
- A fragment's root may be any element; only the file a program loads must be a
  `Window`. A file that declares props is reported as a fragment by `goicheck`.
- `props.Name` is only valid inside the file that declares it. The entry file has
  nothing to fill it in, so it is an error there; a name the block does not
  declare is an error too, with a "did you mean" hint.
- A prop's declared type is checked against every property it feeds.

### Passing children into a fragment

A fragment can take children, not just values. It marks where they go with
`$children`, and an instance puts its own child elements there:

```
// Card.goi
Column {
    gap: 5
    $children
}
```

```
// Main.goi
import "./Card.goi" as Card

Window {
    Card {
        width: 300
        Text { text: "selam" }
        Button { label: "Tamam" }
    }
}
```

- `$children` is a slot, not an element: it takes no properties and no children
  of its own. A fragment may have more than one; each gets the same children.
- Children may be anything, including other fragments. They resolve their
  imports in the **file they were written in**, so a child can use an alias that
  only `Main.goi` imports.
- Passing children to a fragment that has no `$children` is an error, reported
  where the children were written. A fragment with a slot but no children just
  draws nothing there, so it can still be previewed on its own.
- A slot may sit anywhere in the fragment body, including inside another
  fragment call, which is how a fragment forwards its children.
- Like a file with props, a file with `$children` is a fragment: `goicheck` and
  the language server do not require it to be a `Window`.

### Named slots

A fragment can take more than one group of children, one per slot. Any
`$name` in member position is a slot; `$children` is the unnamed one that
plain children go to:

```
// Card.goi
Column {
    $header
    $children
    $footer
}
```

```
// Main.goi
Card {
    $header { Text { text: "Inbox" } }
    Text { text: "The body" }        // goes to $children
    $footer { Button { label: "OK" } }
}
```

- A bare `$header` in a fragment body is a slot marker; `$header { ... }` in an
  instance call passes the elements that land there.
- Passing a slot the fragment does not declare is an error, reported in the
  caller. An empty `$header { }` still counts as filling the slot.
- As with `$children`, a slot with nothing passed draws nothing, so the fragment
  previews on its own.

### Ids and instances

An instance that sets `id` namespaces every id inside the fragment, so one
fragment can be used any number of times and each copy still carries its own
behaviour:

```
Column {
    Card { id: first }
    Card { id: second }
}
```

`Card.goi`'s `Text { id: title }` is `first.title` in the first copy and
`second.title` in the second, and `ui.ByID[*ui.Text](w, "first.title")` finds
them. References inside the fragment follow the instance, so `side.width` still
points at that copy's own `side`. Ids written in the caller — its own children,
including the ones it passes to a slot — are not scoped, because they belong to
the caller. An instance without an `id` scopes nothing, so two such instances
still collide.

An id in a `for` row is scoped too, so `ui.Descendant` needs the prefixed name
inside a scoped instance.

A design reads another node by its id, the same way it reads `window.width`:
`width: panel.width`. The id must be declared in the same file, and the
reference reads the property at the time the value is evaluated.

The language server knows these ids: typing a value offers them, `panel.` lists
the element's properties, hover explains either half, and go-to-definition jumps
to the `id:` that declares the name.

### Conditional blocks

`if` picks one arm of elements, written the way Go writes it. The condition is an
ordinary expression and must be a bool; every arm is built when the window loads
and only the matching one is shown, so a block costs no layout for the arm it
hides.

```goi
Window {
    Column {
        if app.dark {
            Text { text: "Dark" }
        } else if window.width > 900 {
            Text { text: "Wide" }
        } else {
            Text { text: "Light" }
        }
    }
}
```

- Conditions may read `app.theme`, `app.dark`, `app.os`, `window.width`,
  `window.height`, `bridge.*`, `props.*` and any `id.prop` that is a bool, and
  combine them with comparisons, `&&`, `||`, `!` and `?:`.
- An arm holds elements and blocks only. An `if` inside a `Text` is an error,
  because a leaf has no children to switch.
- Blocks nest: an inner `if` shows only where its outer arm also shows.
- A Go-set bool property re-evaluates the condition on the next frame, so
  `id.checked` is a fully reactive condition without any extra wiring.
- Ids inside arms are registered like any other id, so two arms cannot declare
  the same one.
- An `if` inside a `for` body sees the loop variables.

### Loops

`for` repeats a body with Go's range rules:

```goi
Column {
    for i, user := range bridge.users {
        if user.Age > 17 {
            Text { text: user.FullName }
        }
    }
}
```

- A slice gives the index and the element (`for i, row := range bridge.rows`).
- A map gives the key and the value, in sorted key order so a frame is stable.
- A number counts from zero: `for i := range 5`.
- `for range list` is legal when neither name is needed, and `_` ignores one:
  `for _, row := range bridge.rows`.
- A single name binds the key, as in Go: `for i := range bridge.rows`.
- Rows are rebuilt when the list is set with `ui.SetData`, and they keep their
  ids on the nodes rather than registering them, so two rows do not collide.

### Declarations: values and handlers

A design declares the values it reads and the handlers it calls in one `bridge`
block. `data { ... }` is the original spelling and still works; `type` and
`data <Name>` work alike for a row type. Declarations are written like Go fields:
`Name Type`, with no colon and no default.

```goi
bridge {
    Loading bool
    Error   string
    Count   int
    Tickets []Ticket
    OnOpen  func(id string)
    OnFilter func(query string, limit uint)
}

// The shape of one row. A list names the type it holds.
type Ticket {
    ID        string
    Title     string
    Requester string
    Unread    bool
}
```

```goi
Window {
    Column {
        if bridge.Loading {
            Text { text: "Loading…" }
        } else {
            Text { text: formatf("%d tickets", bridge.Count) }
            for _, ticket := range bridge.Tickets {
                ListItem {
                    label: ticket.Title
                    supporting: ticket.Requester
                    selected: ticket.Unread
                    onClick: bridge.OnOpen(ticket.ID)
                }
            }
        }
    }
}
```

Go supplies the values and the handler bodies:

```go
ui.SetData(w, "Loading", true)
ui.SetData(w, "Count", 3)
ui.SetData(w, "Tickets", items)          // any slice
ui.SetHandler(w, "OnOpen", func(id string) { open(id) })
```

`ui.SetHandler` belongs in `Bind`, which runs again after every hot reload, so
re-registering there is enough. A handler is any Go function; its parameters are
converted from the design's values: a number may feed any Go numeric type, a
bool and a color may feed a `string` (as `true`/`false` and `#rrggbb`).

- **Types.** Every primitive Go has is accepted: `bool`, `string`, `int`,
  `int8`…`int64`, `uint`…`uint64`, `uintptr`, `float32`, `float64`, `byte`,
  `rune`, plus `color` for an M3 colour. A list is `[]T`, a map is `map[K]V`
  (the key is a string), and a handler is `func(a string)`.
- **Zero values.** A variable the app never sets reads its type's zero value:
  `""`, `0`, `false`, transparent black, an empty list or map. No declaration
  takes a default; Go is the only place a value starts.
- **Handlers.** `OnOpen func(id string)` declares a handler with its parameters;
  `func()` takes none. A handler is never a value: it can only be called in an
  event property.
- **Calls.** `bridge.OnOpen(ticket.ID)` is the whole value of an event property.
  It is not an expression: you cannot use it inside `+`, a comparison or a
  non-event property.
- **Fire time.** Arguments are evaluated when the event fires, not every frame,
  so `ticket.ID` is the row that exists at click time.
- **`event`.** The event's own payload is available as `event`: `onToggle`
  passes a bool, `onChange` a number, `onEdit` and `onSubmit` a string,
  `onSelect` an index. `onClick` carries no scalar value.
- **Events.** Every element accepts `onClick`. Components also accept
  `onToggle`, `onChange`, `onEdit`, `onSubmit` and `onSelect` where they emit
  them. The value is always a bridge call; a literal is rejected.
- **Any file may declare them.** A fragment owns its own `bridge { ... }` block
  and the types its loops walk; the declarations merge into the one design the
  app sees, so `ui.SetData(w, "Rows", …)` feeds a list a fragment drew.
- **Go values.** `ui.SetData` takes a slice of structs — a field is matched by its
  `goi` tag, then by the field name with a lower first letter. An **unexported
  field is ignored**, and so is a field tagged `goi:"-"`. A slice of
  `map[string]any` works too. Numbers are converted, so an `int` field reads in
  a `number` position. A `nil` slice is an empty list, which is what the first
  frame has before the fetch returns.

```go
type User struct {
    ID        string `goi:"id"`
    FullName  string `goi:"fullName"`
    Points    []int  `goi:"points"`
    private   bool   // ignored: unexported
    Ignored   string `goi:"-"` // ignored: the tag says so
}
```

- **Rebuilding.** A list variable rebuilds only the loops that walk it, and only
  when it is set. A scalar keeps its nodes and just re-reads the value, which is
  why `if bridge.Loading` costs one visibility toggle.
- **Row ids** are not registered, exactly like an `ui.Instantiate` copy, because
  every row would otherwise collide: reach a part of a row with `ui.Descendant`.
- **Hooks.** `ui.OnEach` runs for every row the loop builds, including rebuilt
  ones, and gets the item the row was built from. It must be safe to call twice.
- **Validation.** `goicheck` and the language server check the declarations, the
  value a loop walks, every `row.Field` against its type, the type a field feeds,
  and every handler call: the name, the argument count, the argument types and
  the `event` payload.
- **Runtime errors.** Calling a handler that Go never registered, or passing it
  arguments it cannot accept, is reported through the app's error hook instead
  of killing the frame.
- `ui.Fill` and `template: true` remain for a list built by hand — inserting one
  row, or a container that is not a `.goi` block.

## Grammar

```
File      = { Import | Props | Bridge | Shape } Element EOF
Import    = "import" String "as" Ident
Props     = "props" "{" { Decl } "}"
Bridge    = ("bridge" | "data") "{" { Decl } "}"
Shape     = ("type" | "data") Ident "{" { Decl } "}"
Decl      = Ident Type [";"]
Type      = Primitive | "[]" Type | "map" "[" Ident "]" Type
          | "func" "(" [ Ident Type { "," Ident Type } ] ")"
Primitive = "bool" | "string" | "color" | Go numeric types | Ident
Element   = Ident "{" { Member } "}"
Member    = Property | Element | If | For | "$" Ident [ "{" { Member } "}" ]
If        = "if" Value "{" { Member } "}"
            { "else" "if" Value "{" { Member } "}" }
            [ "else" "{" { Member } "}" ]
For       = "for" [ Ident [ "," Ident ] ":=" ] "range" Value "{" { Member } "}"
Property  = Ident ":" Value [";"]
EventHandler = Ident ":" ("bridge" | "data" | "props") "." Ident
               "(" [ Value { "," Value } ] ")"
Value     = String | Number | Bool | Color | Ident | Map | Call | Expression
Call      = "formatf" "(" Value { "," Value } ")"
Map       = "{" [ MapEntry { "," MapEntry } [","] ] "}"
MapEntry  = Ident ":" Value
Expression = Or [ "?" Value ":" Value ]
Or         = And { "||" And }
And        = Equality { "&&" Equality }
Equality   = Compare { ("==" | "!=") Compare }
Compare    = Sum { ("<" | "<=" | ">" | ">=") Sum }
Sum        = Term { ("+" | "-") Term }
Term       = Unary { ("*" | "/") Unary }
Unary      = ("!" | "-") Unary | Factor
Factor     = Number | Number "%" | String | Bool | Color | Ident
           | Ident "." Ident | "(" Expression ")"
```

Newlines are not tokens. A member is a property when the identifier is followed
by `:` and a child element when it is followed by `{`, so `Name: value` and
`Name { ... }` can be written on one line or across many. A declared type is
`Name Type`, with no colon: the colon is what tells a property from a
declaration. A function call appears either as an event property's whole value
(the bridge handler) or as the `formatf` builtin inside a `Value`.

## Literals

| Form | Type | Notes |
|---|---|---|
| `"text"` | string | `\" \\ \n \t \r \uXXXX` escapes; `\uD83D\uDE00` surrogate pairs work |
| `hello` | identifier | used for enums and `id`; also accepted where a string is expected |
| `480`, `32.5`, `-1` | number | one numeric type everywhere (`float64`) |
| `true`, `false` | bool | |
| `#rgb`, `#rrggbb`, `#rrggbbaa` | color | alpha defaults to `ff`; `"#rrggbb"` quoted also works |
| `surfaceContainerHigh` | color role | a theme name, so it follows light/dark; any role in the M3 scheme, typo hints included |
| `{ base: 8, md: 16 }` | map | responsive value, one entry per breakpoint |

## Elements

| Element | Children | Properties beyond the common set |
|---|---|---|
| `Window` | yes | `title`, `theme` (`light`, `dark`, or a name the app registered), `titleBar` (`system`, `custom`) |
| `Column` | yes | `spacing` (`gap`), `align` |
| `Row` | yes | `spacing` (`gap`), `align` |
| `TitleBar` | yes | `spacing` (`gap`), `align`; a press that misses its clickable children moves the window |
| `Stack` | yes | `align` |
| `Positioned` | yes | `left`, `top`, `right`, `bottom`, `translateX`, `translateY` |
| `Wrap` | yes | `spacing` (`gap`), `align` |
| `Grid` | yes | `columns`, `spacing` (`gap`), `align` |
| `Rectangle` | no | — |
| `Image` | no | `src`, `fit` (`cover`, `contain`, `fill`), plus `radius` |
| `Text` | no | `text`, `fontSize`, `weight`, `color`, `align`, `wrap`, `role`, `selectable` |

`theme` names a colour scheme. `light` and `dark` are built in; the app adds
more with `ui.WithTheme(name, theme)`, and an unknown name falls back to light
with a diagnostic on `App.OnError`. `app.dark` reports the resolved scheme's
mood, so a custom dark theme still reads as dark in a conditional.

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

The language server completes icon names from the embedded font, so the list is
whatever `Library.IconFace` can draw. A name that starts with a digit (`10k`,
`3d`) needs quotes; every other name is a bare identifier (`icon: add`).

`Switch` also takes `iconOn` and `iconOff`, which override `icon` for the
checked and unchecked state. Any of them empty means no glyph in the thumb; a
switch with an icon keeps the larger thumb in both states so the glyph has room,
inset 4dp on every side. The glyph is `Primary` on the checked thumb and the
surface colour (white in the light scheme) on the unchecked one.

`SegmentedButton` segments form a group with their contiguous siblings. By
default the group is multi-select (checkbox): clicking a segment toggles it.
`variant: single` makes it single-select (radio): picking a segment clears the
others, and clicking the selected segment keeps it selected.

`NavigationDrawer` is the panel only; it has no open/closed state of its own.
A modal drawer is a `Stack` with the page first, then a scrim `Rectangle` and a
`Positioned` drawer with `visible: false`; Go flips `SetVisible`, and animates
the drawer's `translateX` from `-width` to 0 so it slides in from the edge.
Select events from a navigation container report the index among its
destinations, so a header, spacer or action inside the same rail does not shift
the page list.

Containers that lay children out (`ButtonGroup`, `Toolbar`, `Carousel`, `Tabs`,
`NavigationBar`, `NavigationRail`, `NavigationDrawer`, `List`, `Menu`, `Card`,
`Dialog`, `BottomSheet`, `SideSheet`, `DatePicker`, `TimePicker`) also accept
`spacing`, `gap` and `align`. `gap` is the same cell as `spacing`, under the name
a layout reader expects.

`Positioned` also takes `translateX` and `translateY`, which shift the anchored
box without changing its anchors. They exist for enter/exit animation: `left`
and `top` reserve `-1` for "auto", so a slide off screen animates the translate
instead.

Common to every element: `id`, `width`, `height`, `minWidth`, `maxWidth`,
`margin`, `padding`, `background`, `borderColor`, `borderWidth`, `radius`,
`visible`, `flex`, `alignSelf`. `marginTop`, `marginRight`, `marginBottom`,
`marginLeft` and `paddingTop`, `paddingRight`, `paddingBottom`, `paddingLeft`
override one edge and leave the others following `margin`/`padding`:

```
Column  { padding: 16  paddingLeft: 4 }   // 4 left, 16 the other sides
```

`align` is one of `start`, `center`, `end`, `stretch`. `alignSelf` uses the same
values and overrides the parent's `align` for one child.

`width` and `height` default to `-1`, meaning automatic. `visible: false`
removes the element from layout, painting and hit-testing entirely; it works on
every element, Material components included, and accepts a breakpoint map like
any other property (`visible: { base: true, md: false }`). From Go,
`SetVisible(false)` on any node does the same at runtime.

## Expressions and percentages

Every property accepts an expression, not just the numeric ones: a color, a
string, a bool or an enum can be conditional too. A reference reads another
element's property by id (`id.property`), so the value follows that element
automatically. Operators are `+ - * /`, comparisons `== != < <= > >=`, `&& ||
!`, a conditional `cond ? a : b` and parentheses, with the usual precedence.
`+` adds two numbers and joins anything else as text, so `bridge.Price *
float64(bridge.Count)` is arithmetic while `"n: " + bridge.Count` is a string.
`-`, `*` and `/` are numeric only. A type mismatch, division by zero or a reference cycle
leaves the property at its previous value instead of failing the frame.

### formatf

`formatf` builds a string with Go's `fmt` verbs:

```goi
Text { text: formatf("Count is %d", bridge.Count) }
Text { text: formatf("%s: %0.2f", props.Title, props.Price) }
```

The first argument is the format string; every later argument fills one verb.
Because a design number is a `float64`, `%d` converts it to an integer and
`%0.2f` keeps it floating, so both spellings work on the same value. `%v` prints
any value as Go would, and a color prints as `#rrggbb`.

A `formatf` result is a string, so it feeds `text`, a `string` prop, a `label` or
an enum-or-string position; feeding a `number` property is an error. It is
re-evaluated on every frame, exactly like any other expression, so a
`ui.SetData` write reaches the text without extra wiring.

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

- Exactly one root element, and it must be `Window` — unless the file declares
  `props`, which makes it a fragment that an `import` pulls in.
- `id` must match `[a-z][A-Za-z0-9_]*` and be unique within the file.
- `Positioned` must be a direct child of `Stack`.
- Unknown element, unknown property, wrong literal type, unknown enum value,
  unknown or repeated breakpoint, nested map, responsive `id` and duplicate ids
  are errors, each reported with `file:line:col`, the offending line and a
  caret. A small edit distance adds a "did you mean" hint. A handler call is
  checked the same way: the name must be declared in `bridge`, and the argument
  count, argument types and `event` payload must match the declaration.
- The parser recovers, so one typo does not hide the rest of the file.

## What is not in v1

No bindings and no handler bodies — a design declares a handler's name and
parameters, but the body is always Go. A list from Go is a `bridge` variable the
app sets; `ui.Fill` covers a list built by hand.
Theming is limited to the built-in light and dark Material 3 schemes; dynamic
color and custom palettes are not exposed yet.

`Window.theme` picks the scheme and every colour a component draws comes from it,
so set it from Go (`win.Root().Theme.Set("dark")`) and the whole window follows.
Leave the root's `background` unset when the page should follow the scheme: the
renderer then fills with the theme surface. A declared `background` is a fixed
colour and stays put across a theme change, which is what to use for a deliberate
brand surface and not for the page.
