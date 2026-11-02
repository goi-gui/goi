# Components

Every element the `.goi` format knows, grouped by what it is for. Each name links
to its own page with a `.goi` example, its properties and a Go example.

The framework has three layers: layout primitives (`Column`, `Row`, `Stack`, …),
plain drawing nodes (`Rectangle`, `Image`, `Text`) and the Material 3 components.
They all share one property set and one way of being driven from Go.


## Layout and structure

- [Window](components/window.md) — The root of the tree.
- [Column](components/column.md) — Stacks children vertically, top to bottom.
- [Row](components/row.md) — Places children left to right.
- [TitleBar](components/titlebar.md) — A Row that moves the window when dragged.
- [Stack](components/stack.md) — Overlays children on top of each other, last child on top.
- [Positioned](components/positioned.md) — Anchors a child inside a Stack.
- [ClipRRect](components/cliprrect.md) — Cuts its children to a rounded box.
- [Wrap](components/wrap.md) — Flows children left to right and starts a new run when the next child does not fit.
- [Grid](components/grid.md) — Places children in a fixed number of equal-width columns.
- [Rectangle](components/rectangle.md) — A plain colored box.
- [Image](components/image.md) — Draws a decoded bitmap.
- [Text](components/text.md) — One run of text at a Material 3 type-scale role.

## Actions

- [Button](components/button.md) — The ordinary Material 3 button.
- [IconButton](components/iconbutton.md) — A square button that holds only an icon.
- [FAB](components/fab.md) — The floating action button: the primary action of a page.
- [FABMenu](components/fabmenu.md) — A FAB that expands into a column of actions.
- [ButtonGroup](components/buttongroup.md) — A row of related buttons that share one shape and one gap.
- [SplitButton](components/splitbutton.md) — A primary action with an attached menu trigger.
- [SegmentedButton](components/segmentedbutton.md) — A horizontal set of segments.
- [Chip](components/chip.md) — A compact, rounded element for filters, inputs, suggestions and tags.

## Selection controls

- [Checkbox](components/checkbox.md) — A binary choice with an optional label.
- [RadioButton](components/radiobutton.md) — One option in a set where exactly one is chosen.
- [Switch](components/switch.md) — A single on/off control with an optional label and optional glyph in the thumb.
- [Slider](components/slider.md) — A continuous value control.

## Text input

- [TextField](components/textfield.md) — An editable single-line field with a floating label.
- [SearchBar](components/searchbar.md) — A text field shaped as a search input, with a leading search glyph.
- [TextArea](components/textarea.md) — A multiline text field.

## Containment

- [Card](components/card.md) — A Material 3 surface that groups related content.
- [Dialog](components/dialog.md) — A modal surface for a decision or a short form.
- [BottomSheet](components/bottomsheet.md) — A surface anchored to the bottom edge.
- [SideSheet](components/sidesheet.md) — A surface anchored to a side edge, for settings or detail.
- [Divider](components/divider.md) — A one-pixel rule between content.
- [List](components/list.md) — A vertical container for `ListItem`s and `MenuItem`s.
- [ListItem](components/listitem.md) — One row in a list: leading icon, label, optional supporting line, trailing affordance.
- [Menu](components/menu.md) — A popup list of `MenuItem`s.
- [MenuItem](components/menuitem.md) — One row in a menu.
- [Tooltip](components/tooltip.md) — A short hint attached to a control.
- [Snackbar](components/snackbar.md) — A short, transient message at the bottom of the screen.

## Navigation

- [TopAppBar](components/topappbar.md) — The bar at the top of a screen: a leading icon, a title and a trailing icon.
- [NavigationBar](components/navigationbar.md) — The bottom navigation bar for the compact window class.
- [NavigationRail](components/navigationrail.md) — The side navigation rail for medium and expanded windows.
- [NavigationDrawer](components/navigationdrawer.md) — A modal drawer that lists the same destinations as the rail and bar.
- [NavigationItem](components/navigationitem.md) — One destination in a navigation bar, rail or drawer.
- [Tabs](components/tabs.md) — A tab bar that groups `Tab` children.
- [Tab](components/tab.md) — One tab in a tab bar.

## Status and progress

- [Badge](components/badge.md) — A small count or status dot, usually overlaid on an icon with `Positioned` or `Stack`.
- [LinearProgress](components/linearprogress.md) — A horizontal progress bar.
- [CircularProgress](components/circularprogress.md) — A circular progress ring.
- [LoadingIndicator](components/loadingindicator.md) — The M3 indeterminate loading shape.

## Date and time

- [DatePicker](components/datepicker.md) — A calendar surface for picking a date.
- [TimePicker](components/timepicker.md) — A clock dial for picking a time.

## Media and grouping

- [Carousel](components/carousel.md) — A horizontally scrolling row of cards.
- [Toolbar](components/toolbar.md) — A horizontal strip of compact controls.
- [Code](components/code.md) — A syntax-highlighted code block.

## Common properties

These apply to every element and are not repeated on the component pages.

| Property | Type | Notes |
|---|---|---|

| `id` | string | assigned by the builder; the handle Go looks up |
| `width, height` | number | declared size; `-1` (default) means auto |
| `minWidth, maxWidth` | number | bounds for the resolved width |
| `margin` | number | outer spacing on all sides |
| `marginTop/Right/Bottom/Left` | number | per-side override; unset follows `margin` |
| `padding` | number | inner spacing on all sides |
| `paddingTop/Right/Bottom/Left` | number | per-side override; unset follows `padding` |
| `background` | color | theme role or `#RRGGBB`; only where the element has no container |
| `borderColor, borderWidth` | color, number | outline around the box |
| `radius` | number | corner radius |
| `visible` | bool | removes the element from layout, painting and hit-testing |
| `template` | bool | marks a hidden row prototype for `ui.Fill` |
| `flex` | number | share of the parent's leftover main-axis space |
| `alignSelf` | enum | `start`, `center`, `end`, `stretch`; overrides the parent's align |
| `scroll` | bool | scroll when content overflows the box |

A scrolling element takes a gesture along its own axis only: a notched wheel
scrolls vertically and **Shift + wheel** scrolls a sideways element, while a
touchpad's two-finger swipe needs no modifier and keeps whichever axis the hand
moved. A gesture that reaches the end of a child carries on into the page above
it, so the pointer never looks stuck over a small scroller.

`marginTop`, `marginRight`, `marginBottom`, `marginLeft` (and the same four for
`padding`) override one edge and leave the others following `margin`/`padding`:

```goi
Column {
	padding: 16
	paddingLeft: 4        // 4 on the left, 16 on the other three sides
}
```

## Containers and `gap`

Containers lay their children out in a row or a column and accept `spacing`,
its alias `gap`, and `align`: `ButtonGroup`, `Toolbar`, `Carousel`, `FABMenu`,
`Tabs`, `NavigationBar`, `NavigationRail`, `NavigationDrawer`, `List`, `Menu`,
`Card`, `Dialog`, `BottomSheet`, `SideSheet`, `DatePicker`, `TimePicker`.

## Events

| Go call | Emitted by |
|---|---|
| `OnClick(func(ui.ClickEvent))` | every element; `ClickEvent` has `X`, `Y`, `Button` |
| `OnToggle(func(bool))` | Checkbox, RadioButton, Switch, Chip |
| `OnChangeValue(func(float64))` | Slider |
| `OnEdit(func(string))` | TextField, SearchBar (every keystroke) |
| `OnSubmit(func(string))` | TextField, SearchBar (Enter) |
| `OnSelect(func(int))` | Tabs, NavigationBar, NavigationRail, List, Menu |

## Themes

`theme` names a colour scheme. `light` and `dark` are built in; the app registers
more by name:

```go
brand := *ui.LightTheme()
brand.Name = "brand"
brand.Colors.Primary = 0x00695CFF
app.LoadWindowFile("app.goi", ui.WithTheme("brand", &brand))
```

```goi
Window { theme: brand }
```

`ui.LightTheme()`/`ui.DarkTheme()` return a copy you can edit; `ui.Theme` and
`ui.Colors` are the exported types. An unknown name falls back to light and
reports through `App.OnError`. `app.dark` follows the resolved scheme.

## Finding elements from Go

```go
btn, err := ui.ByID[*ui.Button](w, "save")   // typed, fails when missing or mistyped
btn := ui.MustByID[*ui.Button](w, "save")    // panics; fine in Bind
node, err := w.ByID("save")                  // untyped
```

A list of unknown length is a row marked `template: true` plus `ui.Fill`, which
replaces the container's rows from a slice in one call. `ui.Instantiate` and
`ui.Remove` are the low-level clone and detach, for a row added one at a time.

A fragment instance can be the template row too, which keeps the row's design in
its own file: `Row { template: true supporting: "x" }` hides the expanded
fragment and `ui.Fill` copies it like any other template.
