package tree

// ClickEvent is delivered to Button.OnClick handlers.
type ClickEvent struct {
	// X and Y are in logical window coordinates.
	X, Y float64
	// Button is 0 for the primary mouse button.
	Button int
}

// PanEvent is one step of a drag a GestureDetector reports: the pointer, and the
// movement since the previous step (from the press, for PanStart).
type PanEvent struct {
	X, Y   float64
	DX, DY float64
}

// MouseEvent is a pointer position a GestureDetector reports when the pointer
// enters, moves over or leaves it. It carries no button: this is where the
// pointer is, not a press.
type MouseEvent struct {
	X, Y float64
}

// ScrollEvent is delivered when a scroll area moves. X and Y are the clamped
// offsets, MaxX and MaxY are the largest offsets the content allows, and
// Progress is the primary axis at 0 (start) to 1 (end), or 0 when the content
// does not overflow. ByUser is false for a programmatic scroll, a smooth
// auto-scroll or the first layout.
type ScrollEvent struct {
	X, Y     float64
	MaxX     float64
	MaxY     float64
	Progress float64
	ByUser   bool
}

// KeyEvent is delivered for key presses and text input.
type KeyEvent struct {
	Key  Key
	Rune rune // 0 for non-text keys
	Mods Mods
}

// Mods holds modifier state.
type Mods struct {
	Shift bool
	Ctrl  bool
	Alt   bool
	Super bool
}

// Control reports whether the platform's primary shortcut modifier is held
// (Ctrl elsewhere, Super on macOS).
func (m Mods) Control() bool { return m.Ctrl || m.Super }

// Key identifies a non-text key.
type Key int

// Keys understood by the input layer.
const (
	KeyNone Key = iota
	KeyBackspace
	KeyDelete
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyEnter
	KeyTab
	KeySpace
	KeyEscape
	KeyUp
	KeyDown
	// Letter keys the editing shortcuts need; text entry itself arrives as
	// Char events, not keys.
	KeyA
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ
)

// FocusEvent is delivered when a node gains or loses focus.
type FocusEvent struct {
	Focused bool
}

// OnClick subscribes to a click. Every node has one, so a layout primitive can
// act as a hit area; on components it is the usual button click.
func (b *NodeBase) OnClick(fn func(ClickEvent)) (off func()) { return b.Click.On(fn) }

// OnClickOutside subscribes to a press outside the node's subtree, which is how
// a popover dismisses itself. The event carries the press coordinates.
func (b *NodeBase) OnClickOutside(fn func(ClickEvent)) (off func()) { return b.Outside.On(fn) }

// OnToggle subscribes to checked changes (checkbox, switch, chip, radio).
func (c *Component) OnToggle(fn func(bool)) (off func()) { return c.Toggle.On(fn) }

// OnChangeValue subscribes to numeric changes (slider, progress).
func (c *Component) OnChangeValue(fn func(float64)) (off func()) { return c.Change.On(fn) }

// OnEdit subscribes to text edits (text field, search bar).
func (c *Component) OnEdit(fn func(string)) (off func()) { return c.Edit.On(fn) }

// OnSubmit subscribes to Enter presses in a text field.
func (c *Component) OnSubmit(fn func(string)) (off func()) { return c.Submit.On(fn) }

// OnSelect subscribes to index selection (tabs, navigation, a chart point).
func (c *Component) OnSelect(fn func(int)) (off func()) { return c.Select.On(fn) }

// OnHoverPoint subscribes to a chart point coming under the pointer, with the
// point's index, or -1 when the pointer leaves the chart.
func (c *Component) OnHoverPoint(fn func(int)) (off func()) { return c.HoverPoint.On(fn) }

// OnScroll subscribes to every movement of a scrolling area. The event carries
// the clamped offsets, the maximum each axis allows, the primary-axis progress
// and whether the reader moved it.
func (b *NodeBase) OnScroll(fn func(ScrollEvent)) (off func()) { return b.Scroll.On(fn) }

// OnScrollToEnd subscribes to reaching the end of the area's primary axis.
func (b *NodeBase) OnScrollToEnd(fn func()) (off func()) {
	return b.ScrollEnd.On(func(struct{}) { fn() })
}

// OnScrollToStart subscribes to returning to the start of the primary axis.
func (b *NodeBase) OnScrollToStart(fn func()) (off func()) {
	return b.ScrollStart.On(func(struct{}) { fn() })
}
