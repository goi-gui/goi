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
	KeyC
	KeyV
	KeyX
)

// FocusEvent is delivered when a node gains or loses focus.
type FocusEvent struct {
	Focused bool
}

// OnClick subscribes to a click. Every node has one, so a layout primitive can
// act as a hit area; on components it is the usual button click.
func (b *NodeBase) OnClick(fn func(ClickEvent)) (off func()) { return b.Click.On(fn) }

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
