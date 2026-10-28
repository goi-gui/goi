package tree

// ClickEvent is delivered to Button.OnClick handlers.
type ClickEvent struct {
	// X and Y are in logical window coordinates.
	X, Y float64
	// Button is 0 for the primary mouse button.
	Button int
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
	KeyEscape
	KeyUp
	KeyDown
)

// FocusEvent is delivered when a node gains or loses focus.
type FocusEvent struct {
	Focused bool
}

// OnClick subscribes to a click. Every component has it.
func (c *Component) OnClick(fn func(ClickEvent)) (off func()) { return c.Click.On(fn) }

// OnToggle subscribes to checked changes (checkbox, switch, chip, radio).
func (c *Component) OnToggle(fn func(bool)) (off func()) { return c.Toggle.On(fn) }

// OnChangeValue subscribes to numeric changes (slider, progress).
func (c *Component) OnChangeValue(fn func(float64)) (off func()) { return c.Change.On(fn) }

// OnEdit subscribes to text edits (text field, search bar).
func (c *Component) OnEdit(fn func(string)) (off func()) { return c.Edit.On(fn) }

// OnSubmit subscribes to Enter presses in a text field.
func (c *Component) OnSubmit(fn func(string)) (off func()) { return c.Submit.On(fn) }

// OnSelect subscribes to index selection (tabs, navigation).
func (c *Component) OnSelect(fn func(int)) (off func()) { return c.Select.On(fn) }
