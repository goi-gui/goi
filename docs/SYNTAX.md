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
element. `props` declares the typed values the fragment expects; the fragment
reads them as `props.name`:

```
// Sidebar.goi
props {
    title: string = "Mail"
    subtitle: string
}

Column {
    width: 280
    Text { text: props.title role: "titleLarge" }
    Text { text: props.subtitle role: "bodySmall" }
}
```

```
// Main.goi
import "./Sidebar.goi" as Sidebar

Window {
    Column {
        Sidebar {
            title: "Inbox"
            width: 300
        }
    }
}
```

- The import path is relative to the **importing file** and must stay inside the
  asset container. Fragments may import fragments; a cycle is an error.
- `Sidebar { }` is replaced by the fragment's root element, so the fragment
  decides its own layout and the instance places it like any other child.
- A prop is `name: type`, with an optional `= default`. The type is `bool`,
  `string`, `number` or `color`: a prop becomes a value in the design, so a list
  or a row shape has no place here.
- A prop the instance leaves out is its default; with no default it is the
  **type's zero value** (`""`, `0`, `false`, `#00000000`), so a fragment never
  has to guard for a missing value.
- An instance property whose name is a prop sets that prop. Any other property
  is applied to the fragment root, replacing the fragment's own value of the same
  name. So `Sidebar { width: 300 }` overrides the fragment's width.
- A fragment's root may be any element; only the file a program loads must be a
  `Window`. A file that declares props is reported as a fragment by `goicheck`.
- `props.name` is only valid inside the file that declares it. The entry file has
  nothing to fill it in, so it is an error there; a name the block does not
  declare is an error too, with a "did you mean" hint.
- A prop's declared type is checked against every property it feeds. The value an
  instance passes is checked against that property, exactly like any other
  literal, so `Sidebar { title: 5 }` is an error where `title` needs a string.

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

An id in an `{#each}` row is scoped too, so `ui.Descendant` needs the prefixed
name inside a scoped instance.

A design reads another node by its id, the same way it reads `window.width`:
`width: panel.width`. The id must be declared in the same file, and the
reference reads the property at the time the value is evaluated.

The language server knows these ids: typing a value offers them, `panel.` lists
the element's properties, hover explains either half, and go-to-definition jumps
to the `id:` that declares the name.

### Conditional blocks

`{#if}` picks one arm of elements. The condition is an ordinary expression and
must be a bool; every arm is built when the window loads and only the matching
one is shown, so a block costs no layout for the arm it hides.

```goi
Window {
    Column {
        {#if app.dark}
            IconButton { icon: dark_mode }
        {:else if window.width > 900}
            IconButton { icon: desktop }
        {:else}
            IconButton { icon: light_mode }
        {/if}
    }
}
```

- The arms are ordinary children of the enclosing element: a block adds no box,
  no spacing and no flex item of its own. In a `Row` the visible arm is laid out
  as a row item, exactly as if it were written there directly.
- Conditions may read `app.theme`, `app.dark`, `app.os`, `window.width`,
  `window.height` and any `id.prop` that is a bool, and combine them with
  comparisons, `&&`, `||`, `!` and `?:`.
- An arm holds elements only. A `{#if}` inside a `Text` is an error, because a
  leaf has no children to switch.
- Blocks nest: an inner `{#if}` shows only where its outer arm also shows.
- A Go-set bool property re-evaluates the condition on the next frame, so
  `id.checked` is a fully reactive condition without any extra wiring.
- Ids inside arms are registered like any other id, so two arms cannot declare
  the same one.
- An `{#if}` inside an `{#each}` arm sees the loop variable, so a row can switch
  part of itself on its own fields.

### Bridge: values and handlers

A design declares the values it reads and the handlers it calls in one `bridge`
block. `data { ... }` is the original spelling and still works; `type` and
`data <name>` work alike for a row shape.

```goi
bridge {
	loading: bool
	error: string
	count: number
	tickets: [ticket]
	onOpen: func(id: string)
	onFilter: func(query: string, limit: number)
}

// The shape of one row. A list names the shape it holds.
type ticket {
	id: string
	title: string
	requester: string
	unread: bool
}
```

```goi
Window {
	Column {
		{#if bridge.loading}
			Text { text: "Loading…" }
		{:else}
			Text { text: bridge.count }
			{#each bridge.tickets as ticket}
				ListItem {
					label: ticket.title
					supporting: ticket.requester
					selected: ticket.unread
					onClick: bridge.onOpen(ticket.id)
				}
			{:else}
				Text { text: bridge.error }
			{/each}
		{/if}
	}
}
```

Go supplies the values and the handler bodies:

```go
ui.SetData(w, "loading", true)
ui.SetData(w, "count", "3 tickets")
ui.SetData(w, "tickets", items)          // any slice
ui.SetHandler(w, "onOpen", func(id string) { open(id) })
```

`ui.SetHandler` belongs in `Bind`, which runs again after every hot reload, so
re-registering there is enough. A handler is any Go function; its parameters are
converted from the design's values: a number may feed an `int`, a `float64` or a
`string`, and a bool or a color may feed a `string` (as `true`/`false` and
`#rrggbb`).

- **Types.** A scalar is `bool`, `string`, `number` or `color`. A list is
  `[shape]` or the older `shape[]`, and the shape must be declared. A field of a
  shape may itself be a list, so `type group { name: string, items: [item] }`
  works.
- **Handlers.** `onOpen: func(id: string)` declares a handler with its
  parameters; `func()` takes none. A handler parameter is a scalar:
  `bool`, `string`, `number` or `color`. A handler is never a value: it can only
  be called in an event property.
- **Calls.** `bridge.onOpen(ticket.id)` is the whole value of an event property.
  It is not an expression: you cannot use it inside `+`, a comparison or a
  non-event property.
- **Fire time.** Arguments are evaluated when the event fires, not every frame,
  so `ticket.id` is the row that exists at click time.
- **`event`.** The event's own payload is available as `event`: `onToggle`
  passes a bool, `onChange` a number, `onEdit` and `onSubmit` a string,
  `onSelect` an index. `onClick` carries no scalar value. Declare the parameter
  type the payload fits: `onChange: bridge.onVolume(event)` with
  `onVolume: func(v: number)`.
- **Events.** Every element accepts `onClick`. Components also accept
  `onToggle`, `onChange`, `onEdit`, `onSubmit` and `onSelect` where they emit
  them. The value is always a bridge call; a literal is rejected.
- **Any file may declare them.** A fragment owns its own `bridge { ... }` block
  and the shapes its `{#each}` walks; the declarations merge into the one design
  the app sees, so `ui.SetData(w, "rows", …)` feeds a list a fragment drew.
- **Go values.** `ui.SetData` takes a slice of structs — matched by `json` tag,
  then by the field name with a lower first letter — or a slice of
  `map[string]any`. Numbers are converted, so an `int` field reads in a `number`
  position. A `nil` slice is an empty list, which is what the first frame has
  before the fetch returns.
- **Rebuilding.** A list variable rebuilds only the `{#each}` blocks that walk
  it, and only when it is set. A scalar keeps its nodes and just re-reads the
  value, which is why `{#if bridge.loading}` costs one visibility toggle.
- **The `{:else}` arm** shows when the list is empty or not yet set.
- **Row ids** are not registered, exactly like an `ui.Instantiate` copy, because
  every row would otherwise collide: reach a part of a row with `ui.Descendant`.
  An id inside the `{:else}` arm is a normal, registered id.
- **Hooks.** `ui.OnEach` runs for every row the block builds, including rebuilt
  ones, and gets the item the row was built from. It must be safe to call twice.
- **Validation.** `goicheck` and the language server check the declarations, the
  list an `{#each}` walks, every `row.field` against its shape, the type a field
  feeds, and every handler call: the name, the argument count, the argument
  types and the `event` payload. `bridge.x` in a value position must not be a
  list or a handler; a list is only valid as an `{#each}` source.
- **Runtime errors.** Calling a handler that Go never registered, or passing it
  arguments it cannot accept, is reported through the app's error hook instead
  of killing the frame.
- `ui.Fill` and `template: true` remain for a list built by hand — inserting one
  row, or a container that is not a `.goi` block.

## Grammar

```
File      = { Import | Props | Bridge | Shape } Element EOF
Import    = "import" String "as" Ident
Props     = "props" "{" { PropDecl } "}"
PropDecl  = Ident ":" ScalarType [ "=" Value ] [";"]
ScalarType = "bool" | "string" | "number" | "color"
Bridge    = ("bridge" | "data") "{" { VarDecl } "}"
Shape     = ("type" | "data") Ident "{" { FieldDecl } "}"
VarDecl   = Ident ":" Type [";"]
FieldDecl = Ident ":" Type [";"]
Type      = "bool" | "string" | "number" | "color"
          | "[" Ident "]" | Ident "[]"
          | "func" "(" [ Ident ":" Type { "," Ident ":" Type } ] ")"
Element   = Ident "{" { Member } "}"
Member    = Property | Element | Block | "$" Ident [ "{" { Member } "}" ]
Block     = "{#" "if" Value "}" { Member }
            { "{:else if" Value "}" { Member } }
            [ "{:else}" { Member } ]
            "{/if}"
          | "{#" "each" ("bridge" | "data") "." Ident "as" Ident "}" { Member }
            [ "{:else}" { Member } ]
            "{/each}"
Property  = Ident ":" Value [";"]
EventHandler = Ident ":" ("bridge" | "data") "." Ident
               "(" [ Value { "," Value } ] ")"
Value     = String | Number | Bool | Color | Ident | Map | Expression | "$" Ident
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
Factor     = Number | Number "%" | String | Bool | Color | Ident | Ident "." Ident | "$" Ident | "(" Expression ")"
```

Newlines are not tokens. A member is a property when the identifier is followed
by `:` and a child element when it is followed by `{`, so `name: value` and
`Name { ... }` can be written on one line or across many. An `EventHandler` is
the only place a call may appear: it is an event property's whole value, never
part of a `Value`.

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
`+` concatenates two strings. A type mismatch, division by zero or a reference
cycle leaves the property at its previous value instead of failing the frame.

Two namespaces are reserved for expressions:

| Reference | Type | Meaning |
|---|---|---|
| `app.theme` | string | the resolved scheme's name: `light`, `dark` or a registered one |
| `app.mode` | `light` \| `dark` | the resolved scheme's mood |
| `app.systemMode` | `light` \| `dark` | the desktop's preference |
| `app.dark` | bool | shorthand for `app.mode == "dark"` |
| `app.os` | string | `runtime.GOOS`: `linux`, `darwin`, `windows`, … |
| `window.width`, `window.height` | number | logical client size, updated on resize |

Any other name before the dot is read in order as a `props` entry, an `{#each}`
loop variable, a `bridge` (or `data`) variable, or an element id declared in the
same file:

```
Text { text: props.title }
Text { text: bridge.count }
Text { text: searchField.text }
Rectangle { width: panel.width / 2 }
```

So a design can react to the scheme or the platform without any Go code:

```
TopAppBar { trailingIcon: app.dark ? "light_mode" : "dark_mode" }
Text { text: app.dark ? "Dark scheme" : "Light scheme" color: app.dark ? onSurface : onPrimary }
Rectangle { visible: window.width >= 640 width: window.width < 640 ? 240 : 320 }
TitleBar { visible: app.os == "linux" }
```

`app.mode` is a value, not a flag, so it can feed a property that expects an
enum or a string, and it can be compared:

```
IconButton { icon: app.mode == "dark" ? "light_mode" : "dark_mode" }
Text { text: app.mode == "dark" ? "Dark scheme" : "Light scheme" }
```

`app.systemMode` is what the desktop asked for. Naming it as the theme makes a
design follow the system setting with no Go code, and the window switches as soon
as the desktop does:

```
Window {
    theme: app.systemMode
}
```

A platform that does not report a preference reads as `light`. `app.theme` still
reports the *name*, so `theme: brand` keeps `app.theme == "brand"` while
`app.mode` says whether that scheme is light or dark.

Because these expressions read live state, they are re-evaluated every frame;
an unchanged result does not mark anything dirty, so an idle window stays idle.

A property starts out owned by its expression. The first time Go sets it, Go
takes it over and the expression stops writing that property, so a scheme the
user picked is not reverted by `theme: app.systemMode`. Hot reload keeps the
rule, and editing that property's literal in the `.goi` file gives it back to
the expression.

```
Window {
    theme: app.systemMode          // follows the desktop until Go sets it
    Switch { checked: app.dark }   // likewise: a click wins
}
```

```
Row {
    Rectangle { id: side width: 200 height: 40 }
    Rectangle { width: side.width / 2 height: 40 }
    Rectangle { width: (side.width + 40) * 0.5 height: 40 }
}
```

`width` and `height` also accept a percentage of the space the parent offers:
`width: 50%` is half of the parent's content box, so it follows a resize without
a breakpoint. A percentage may be part of an expression (`width: 50% - 8`).
Percentages are only valid for `width` and `height`; a property whose value is a
percentage cannot be referenced, because it depends on its own parent.

Expressions are resolved before layout; percentages are resolved during layout,
when the parent's content size is known. Like responsive values, they are owned
by the file: a later `Prop.Set` from Go is overwritten on the next frame. To read
Go state from a design, put it in a property another element can reference.

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

No bindings, no loops and no handler bodies — a design declares a handler's name
and parameters, but the body is always Go. A list grows with `ui.Instantiate`
from a hidden template, not with a `.goi` `for`.
Theming is limited to the built-in light and dark Material 3 schemes; dynamic
color and custom palettes are not exposed yet.

`Window.theme` picks the scheme and every colour a component draws comes from it,
so set it from Go (`win.Root().Theme.Set("dark")`) and the whole window follows.
Leave the root's `background` unset when the page should follow the scheme: the
renderer then fills with the theme surface. A declared `background` is a fixed
colour and stays put across a theme change, which is what to use for a deliberate
brand surface and not for the page.
