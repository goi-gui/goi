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

// OnClick subscribes to the button's click event.
func (b *Button) OnClick(fn func(ClickEvent)) (off func()) { return b.Click.On(fn) }

// OnChange subscribes to text edits.
func (t *TextInput) OnChange(fn func(string)) (off func()) { return t.Change.On(fn) }

// OnSubmit subscribes to Enter presses.
func (t *TextInput) OnSubmit(fn func(string)) (off func()) { return t.Submit.On(fn) }
