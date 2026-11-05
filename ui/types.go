// Package ui is the public API of the goi declarative GUI framework.
//
// A .goi file describes the window; Go code finds elements by id and attaches
// behaviour:
//
//	win.Bind(func(w *ui.Window) error {
//		label := ui.MustByID[*ui.Text](w, "label")
//		ui.MustByID[*ui.Button](w, "inc").OnClick(func(ui.ClickEvent) {
//			label.Text.Set(strconv.Itoa(n))
//		})
//		return nil
//	})
//
// All widgets and events are re-exported from the internal tree, whose types
// are the actual storage: setting label.Text marks the tree dirty and schedules
// a frame.
package ui

import (
	"github.com/goi-gui/goi/internal/m3"
	"github.com/goi-gui/goi/internal/tree"
)

// Component is the shared Material 3 component state (label, icon, variant,
// checked, value and the events).
type Component = tree.Component

// Prop is a typed property cell. Set is a no-op when the value is unchanged.
type Prop[T comparable] = tree.Prop[T]

// Event is a typed event with On/Emit.
type Event[E any] = tree.Event[E]

// NodeBase is the state shared by every node: id, parent, children, bounds and
// dirty flags.
type NodeBase = tree.NodeBase

// Node is any element in the tree.
type Node = tree.Node

// Rect is a layout rectangle in logical pixels.
type Rect = tree.Rect

// Theme is a complete Material 3 colour scheme. Start from LightTheme or
// DarkTheme and change the roles you want, then register it with WithTheme.
type Theme = m3.Theme

// Colors is the set of Material 3 colour roles, as 0xRRGGBBAA.
type Colors = m3.Colors

// LightTheme returns the baseline light scheme. Copy it before editing, so the
// built-in one stays intact.
func LightTheme() *Theme { return m3.Light() }

// DarkTheme returns the baseline dark scheme.
func DarkTheme() *Theme { return m3.Dark() }

// Mode is a light or dark scheme: the mood a design shows, which
// `Theme.Dark` on the resolved scheme decides.
type Mode = tree.Mode

// Mode values. ModeUnknown is only what SystemMode reports when the desktop
// does not say; a window always resolves to light or dark.
const (
	ModeLight   = tree.ModeLight
	ModeDark    = tree.ModeDark
	ModeUnknown = tree.ModeUnknown
)

// DirtyKind says what a change affects.
type DirtyKind = tree.DirtyKind

// Dirty kinds.
const (
	DirtyPaint  = tree.DirtyPaint
	DirtyLayout = tree.DirtyLayout
)

// Widget types.
type (
	// WindowNode is the root node of a tree; ui.Window is the handle you get in
	// Bind and is not itself a node.
	WindowNode = tree.Window
	// Layout primitives.
	Column     = tree.Column
	Row        = tree.Row
	TitleBar   = tree.TitleBar
	Stack      = tree.Stack
	Positioned = tree.Positioned
	Wrap       = tree.Wrap
	Grid       = tree.Grid
	Rectangle  = tree.Rectangle
	Image      = tree.Image
	SVG        = tree.SVG
	Text       = tree.Text

	// Material 3 components.
	Button           = tree.Button
	IconButton       = tree.IconButton
	FAB              = tree.FAB
	FABMenu          = tree.FABMenu
	ButtonGroup      = tree.ButtonGroup
	SplitButton      = tree.SplitButton
	SegmentedButton  = tree.SegmentedButton
	Chip             = tree.Chip
	Badge            = tree.Badge
	Checkbox         = tree.Checkbox
	RadioButton      = tree.RadioButton
	Switch           = tree.Switch
	Slider           = tree.Slider
	TextField        = tree.TextField
	SearchBar        = tree.SearchBar
	Tooltip          = tree.Tooltip
	Card             = tree.Card
	Dialog           = tree.Dialog
	BottomSheet      = tree.BottomSheet
	SideSheet        = tree.SideSheet
	Divider          = tree.Divider
	List             = tree.List
	ListItem         = tree.ListItem
	Menu             = tree.Menu
	MenuItem         = tree.MenuItem
	Snackbar         = tree.Snackbar
	TopAppBar        = tree.TopAppBar
	NavigationBar    = tree.NavigationBar
	NavigationRail   = tree.NavigationRail
	NavigationDrawer = tree.NavigationDrawer
	NavigationItem   = tree.NavigationItem
	Tabs             = tree.Tabs
	Tab              = tree.Tab
	LinearProgress   = tree.LinearProgress
	CircularProgress = tree.CircularProgress
	LoadingIndicator = tree.LoadingIndicator
	DatePicker       = tree.DatePicker
	TimePicker       = tree.TimePicker
	TextArea         = tree.TextArea
	ClipRRect        = tree.ClipRRect
	Code             = tree.Code
	Carousel         = tree.Carousel
	Toolbar          = tree.Toolbar
)

// Event types.
type (
	ClickEvent = tree.ClickEvent
	KeyEvent   = tree.KeyEvent
	Mods       = tree.Mods
	FocusEvent = tree.FocusEvent
)

// Keys.
const (
	KeyNone      = tree.KeyNone
	KeyBackspace = tree.KeyBackspace
	KeyDelete    = tree.KeyDelete
	KeyLeft      = tree.KeyLeft
	KeyRight     = tree.KeyRight
	KeyHome      = tree.KeyHome
	KeyEnd       = tree.KeyEnd
	KeyEnter     = tree.KeyEnter
	KeyTab       = tree.KeyTab
	KeySpace     = tree.KeySpace
	KeyEscape    = tree.KeyEscape
	KeyUp        = tree.KeyUp
	KeyDown      = tree.KeyDown
)
