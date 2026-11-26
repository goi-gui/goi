package tree

import (
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/goi-gui/goi/internal/componentdef"
	"github.com/goi-gui/goi/internal/highlight"
)

// Component holds the state every Material 3 component shares. Concrete
// components embed it and use props(kind) to expose only the properties their
// type accepts.
//
// Interaction state (hover, pressed, focused) is owned by the input layer and
// only ever read by the painter, so it lives here next to the props.
type Component struct {
	NodeBase

	// Flow containers (Toolbar, ButtonGroup, List, ...) lay children out with
	// the same spacing and alignment properties as Row and Column.
	Spacing    Prop[float64]
	CrossAlign Prop[string]
	MainAlign  Prop[string]

	// Content.
	Label       Prop[string]
	Supporting  Prop[string]
	Text        Prop[string]
	Placeholder Prop[string]
	Icon        Prop[string]
	Trailing    Prop[string]
	// FilledIcon draws every icon the component paints from the filled Material
	// Symbols instance. FilledLeadingIcon and FilledTrailingIcon fill one slot,
	// for a component that wants the two to differ.
	FilledIcon         Prop[bool]
	FilledLeadingIcon  Prop[bool]
	FilledTrailingIcon Prop[bool]
	// ListItem colours: each falls back to its M3 role when zero, so a design
	// that wants its own row can tint one part without touching the others.
	LeadingIconColor  Prop[uint32]
	TrailingIconColor Prop[uint32]
	LabelColor        Prop[uint32]
	SupportingColor   Prop[uint32]
	// IconOn and IconOff override Icon for a Switch's checked state. Both empty
	// means no icon in the thumb.
	IconOn  Prop[string]
	IconOff Prop[string]
	// Language names the syntax a Code block highlights with, and ShowCopy adds
	// the copy button to a Code block's corner.
	Language Prop[string]
	ShowCopy Prop[bool]

	// Appearance.
	Variant Prop[string]
	// Thickness is the width of a progress stroke; 0 follows the component's
	// own default, which depends on the variant.
	Thickness Prop[float64]

	// State.
	Disabled Prop[bool]
	Checked  Prop[bool]
	Selected Prop[bool]
	Expanded Prop[bool]
	// AutoFocus focuses a text input when it is first shown. Only the text
	// inputs list it; the shared cell keeps tree and schema in step.
	AutoFocus Prop[bool]

	// Numeric value (slider, progress, stepper).
	Value Prop[float64]
	Min   Prop[float64]
	Max   Prop[float64]
	Step  Prop[float64]

	// Events.
	Toggle Event[bool]
	Change Event[float64]
	Edit   Event[string]
	Submit Event[string]
	Select Event[int]
	// HoverPoint fires when the pointer moves onto a different point of a
	// chart, with that point's index; -1 is "left the chart".
	HoverPoint Event[int]

	// Runtime interaction state.
	Hover   bool
	Pressed bool
	Focused bool
	// autoFocusDone records that this appearance took focus, so a frame does
	// not steal it back after the user moves on; hiding the field re-arms it.
	autoFocusDone bool
	// LabelFloat is a text field or area label's travel: 0 sits inside the
	// field, 1 above it. LabelPosed is set by the first frame, so loading a
	// page starts at rest instead of sliding every label.
	LabelFloat float64
	LabelPosed bool
	// FocusRing is set only when the focus came from the keyboard. A pointer
	// press moves focus too, but M3 only shows the ring for keyboard users.
	FocusRing bool
	// Caret for text fields.
	Caret int
	// Preedit is the IME composition that is not committed yet. It is drawn
	// after the value but is not part of Text until the input method commits.
	Preedit string
	// PreeditCaret is the caret position inside Preedit, in bytes; -1 means the
	// end (the usual case while composing).
	PreeditCaret int
	// TextScroll is how far the value text is shifted left so the caret stays
	// in view. Paint computes it; input reads it to map clicks back to indices.
	TextScroll float64
	// SelAnchor is the fixed end of a text selection; -1 means none.
	SelAnchor int
	// SelectedIndex for tab bars and navigation.
	SelectedIndex int
	// Animation chooses how a navigation indicator moves: slide, fade or none.
	// Empty means slide.
	Animation Prop[string]
	// NavFrom, NavTo, NavStart and NavActive are the live indicator transition.
	// Input writes them when a destination changes; paint draws it and clears
	// NavActive at the end.
	NavFrom, NavTo Node
	NavStart       time.Duration
	NavActive      bool
	// NavPosed marks that paint has seen the first frame of the transition, so
	// a click after an idle period starts at zero instead of part-way through.
	NavPosed bool
	// PickerMonth is the month a DatePicker shows, as year*12 + month-1; 0
	// means "the month of the picked date". PickerField is which half of a
	// TimePicker is being set: 0 the hour, 1 the minutes.
	PickerMonth int
	PickerField int

	// TogglePos is where the Switch handle sits, 0 (off) to 1 (on). TogglePosed
	// marks that the ui layer drives it, so paint animates instead of reading
	// Checked directly; a headless paint test leaves it false and gets the end
	// state at once.
	TogglePos   float64
	TogglePosed bool
}

// newComponent seeds the shared defaults.
func newComponent() Component {
	return Component{
		SelAnchor:          -1,
		Label:              newProp(nil, "label", DirtyLayout, ""),
		Supporting:         newProp(nil, "supporting", DirtyLayout, ""),
		Text:               newProp(nil, "text", DirtyLayout, ""),
		Placeholder:        newProp(nil, "placeholder", DirtyLayout, ""),
		Icon:               newProp(nil, "icon", DirtyLayout, ""),
		Trailing:           newProp(nil, "trailingIcon", DirtyLayout, ""),
		FilledIcon:         newProp(nil, "filledIcon", DirtyPaint, false),
		FilledLeadingIcon:  newProp(nil, "filledLeadingIcon", DirtyPaint, false),
		FilledTrailingIcon: newProp(nil, "filledTrailingIcon", DirtyPaint, false),
		LeadingIconColor:   newProp(nil, "leadingIconColor", DirtyPaint, uint32(0)),
		TrailingIconColor:  newProp(nil, "trailingIconColor", DirtyPaint, uint32(0)),
		LabelColor:         newProp(nil, "labelColor", DirtyPaint, uint32(0)),
		SupportingColor:    newProp(nil, "supportingColor", DirtyPaint, uint32(0)),
		IconOn:             newProp(nil, "iconOn", DirtyLayout, ""),
		IconOff:            newProp(nil, "iconOff", DirtyLayout, ""),
		Language:           newProp(nil, "language", DirtyPaint, ""),
		ShowCopy:           newProp(nil, "showCopy", DirtyPaint, true),
		Variant:            newProp(nil, "variant", DirtyPaint, ""),
		Thickness:          newProp(nil, "thickness", DirtyLayout, 0.0),
		Disabled:           newProp(nil, "disabled", DirtyPaint, false),
		Checked:            newProp(nil, "checked", DirtyPaint, false),
		// A selected chip or segment shows a check in place of its leading icon,
		// which changes its width, so this one is a layout change.
		Selected:   newProp(nil, "selected", DirtyLayout, false),
		Expanded:   newProp(nil, "expanded", DirtyLayout, false),
		AutoFocus:  newProp(nil, "autoFocus", DirtyPaint, false),
		Value:      newProp(nil, "value", DirtyPaint, 0.0),
		Min:        newProp(nil, "min", DirtyPaint, 0.0),
		Max:        newProp(nil, "max", DirtyPaint, 1.0),
		Step:       newProp(nil, "step", DirtyPaint, 0.0),
		Spacing:    newProp(nil, "spacing", DirtyLayout, 0.0),
		CrossAlign: newProp(nil, "crossAxisAlign", DirtyLayout, "start"),
		MainAlign:  newProp(nil, "mainAxisAlign", DirtyLayout, "start"),
	}
}

// namedProps returns the properties listed in propsFor(kind), in that order.
func (c *Component) props(kind string) []NamedProp {
	names := propsFor(kind)
	out := make([]NamedProp, 0, commonPropCount+len(names)+8)
	out = append(out, commonProps(&c.NodeBase, len(names)+8)...)
	for _, n := range names {
		out = append(out, c.one(kind, n))
	}
	return out
}

// propsExcept is props with some names left to the concrete type, which owns
// cells the shared Component does not have. componentdef still lists those
// names, so the schema and the docs see them.
func (c *Component) propsExcept(kind string, skip ...string) []NamedProp {
	names := propsFor(kind)
	out := make([]NamedProp, 0, commonPropCount+len(names)+12)
	out = append(out, commonProps(&c.NodeBase, len(names)+12)...)
	for _, n := range names {
		own := false
		for _, s := range skip {
			if n == s {
				own = true
				break
			}
		}
		if own {
			continue
		}
		out = append(out, c.one(kind, n))
	}
	return out
}

// one builds the named property for a single shared field. kind lets an enum
// be narrowed to the values that component implements.
func (c *Component) one(kind, name string) NamedProp {
	b := &c.NodeBase
	switch name {
	case "label":
		return StringProp(b, "label", &c.Label)
	case "supporting":
		return StringProp(b, "supporting", &c.Supporting)
	case "text":
		return StringProp(b, "text", &c.Text)
	case "placeholder":
		return StringProp(b, "placeholder", &c.Placeholder)
	case "icon":
		return LayoutStringProp(b, "icon", &c.Icon)
	case "leadingIcon":
		return LayoutStringProp(b, "leadingIcon", &c.Icon)
	case "leadingIconColor":
		return ColorProp(b, "leadingIconColor", DirtyPaint, &c.LeadingIconColor)
	case "trailingIconColor":
		return ColorProp(b, "trailingIconColor", DirtyPaint, &c.TrailingIconColor)
	case "filledIcon":
		return BoolProp(b, "filledIcon", DirtyPaint, &c.FilledIcon)
	case "filledLeadingIcon":
		return BoolProp(b, "filledLeadingIcon", DirtyPaint, &c.FilledLeadingIcon)
	case "filledTrailingIcon":
		return BoolProp(b, "filledTrailingIcon", DirtyPaint, &c.FilledTrailingIcon)
	case "labelColor":
		return ColorProp(b, "labelColor", DirtyPaint, &c.LabelColor)
	case "supportingColor":
		return ColorProp(b, "supportingColor", DirtyPaint, &c.SupportingColor)
	case "trailingIcon":
		return LayoutStringProp(b, "trailingIcon", &c.Trailing)
	case "iconOn":
		return StringProp(b, "iconOn", &c.IconOn)
	case "iconOff":
		return StringProp(b, "iconOff", &c.IconOff)
	case "language":
		return StringProp(b, "language", &c.Language)
	case "showCopy":
		return BoolProp(b, "showCopy", DirtyPaint, &c.ShowCopy)
	case "variant":
		// The accepted words live in componentdef, which the schema validates
		// against. Repeating them here let the validator accept `vertical` on a
		// Divider while the node rejected it, so the enum is read once.
		//
		// A TimePicker's variant picks between a tall clock and a short field
		// card, so switching it has to lay the component out again.
		dirty := DirtyPaint
		if kind == "TimePicker" {
			dirty = DirtyLayout
		}
		return EnumProp(b, "variant", dirty, &c.Variant, componentdef.EnumValuesFor(kind, "variant"))
	case "disabled":
		return BoolProp(b, "disabled", DirtyPaint, &c.Disabled)
	case "checked":
		return BoolProp(b, "checked", DirtyPaint, &c.Checked)
	case "selected":
		// A selected chip or segment swaps its leading icon for a check, which
		// changes its width, so this is a layout change rather than a repaint.
		return BoolProp(b, "selected", DirtyLayout, &c.Selected)
	case "expanded":
		return BoolProp(b, "expanded", DirtyLayout, &c.Expanded)
	case "autoFocus":
		return BoolProp(b, "autoFocus", DirtyPaint, &c.AutoFocus)
	case "value":
		return NumberProp(b, "value", DirtyPaint, &c.Value)
	case "min":
		return NumberProp(b, "min", DirtyLayout, &c.Min)
	case "max":
		return NumberProp(b, "max", DirtyLayout, &c.Max)
	case "step":
		return NumberProp(b, "step", DirtyLayout, &c.Step)
	case "thickness":
		// A thicker stroke changes the box a progress bar needs, so it is a
		// layout property rather than a repaint.
		return NumberProp(b, "thickness", DirtyLayout, &c.Thickness)
	case "spacing":
		return NumberProp(b, "spacing", DirtyLayout, &c.Spacing)
	case "gap":
		// gap is the same cell as spacing, under the name a layout reader
		// expects. Whichever the file writes last wins.
		return NumberProp(b, "gap", DirtyLayout, &c.Spacing)
	case "crossAxisAlign":
		return EnumProp(b, "crossAxisAlign", DirtyLayout, &c.CrossAlign, CrossAxisValues)
	case "mainAxisAlign":
		return EnumProp(b, "mainAxisAlign", DirtyLayout, &c.MainAlign, MainAxisValues)
	}
	return StringProp(b, name, &c.Label)
}

// propsFor lists the shared M3 properties each component accepts.
func propsFor(kind string) []string { return componentdef.Props(kind) }

// The concrete component types. Each is a distinct Go type so ByID is
// type-checked, and each is otherwise empty: all state lives in Component.

// Button is a filled, tonal, elevated, outlined or text button.
type Button struct {
	Component
	// Color overrides the label's colour, and the icons' colour when they do
	// not name their own. Zero keeps the variant's own foreground.
	Color Prop[uint32]
	// LeadingIconSize and TrailingIconSize are the glyph boxes; 0 is the 18dp
	// button default.
	LeadingIconSize  Prop[float64]
	TrailingIconSize Prop[float64]
	// LeadingNode and TrailingNode hold the bodies built from $leading and
	// $trailing. An explicit icon wins over its slot.
	LeadingNode  Node
	TrailingNode Node
	// LabelText carries the label's type styling. It is not a child: paint
	// feeds the label and its resolved colour through the ordinary Text painter,
	// so every text property behaves as it does on a Text.
	LabelText *Text
}

// Kind implements Node.
func (*Button) Kind() string { return "Button" }
func (c *Button) namedProps() []NamedProp {
	b := &c.NodeBase
	lt := c.LabelText
	return append(c.propsExcept("Button",
		"color", "leadingIconSize", "trailingIconSize",
		"role", "fontSize", "weight", "fontWeight", "fontStyle", "align",
		"wrap", "truncate", "maxLines", "decoration", "decorationStyle",
		"decorationThickness", "decorationColor"),
		ColorProp(b, "color", DirtyPaint, &c.Color),
		NumberProp(b, "leadingIconSize", DirtyLayout, &c.LeadingIconSize),
		NumberProp(b, "trailingIconSize", DirtyLayout, &c.TrailingIconSize),
		EnumProp(b, "role", DirtyLayout, &lt.Role, textRoles),
		NumberProp(b, "fontSize", DirtyLayout, &lt.FontSize),
		NumberProp(b, "weight", DirtyLayout, &lt.Weight),
		NumberProp(b, "fontWeight", DirtyLayout, &lt.Weight),
		EnumProp(b, "fontStyle", DirtyPaint, &lt.FontStyle, FontStyleValues),
		EnumProp(b, "align", DirtyPaint, &lt.Align, AlignValues),
		BoolProp(b, "wrap", DirtyLayout, &lt.Wrap),
		BoolProp(b, "truncate", DirtyLayout, &lt.Truncate),
		NumberProp(b, "maxLines", DirtyLayout, &lt.MaxLines),
		StringProp(b, "decoration", &lt.Decoration),
		EnumProp(b, "decorationStyle", DirtyPaint, &lt.DecorationStyle, DecorationStyleValues),
		NumberProp(b, "decorationThickness", DirtyPaint, &lt.DecorationThickness),
		ColorProp(b, "decorationColor", DirtyPaint, &lt.DecorationColor),
	)
}

// slotNodes exposes the button's leading and trailing bodies to the builder.
func (c *Button) slotNodes() (*Node, *Node) { return &c.LeadingNode, &c.TrailingNode }

// ModifierColorRole is the variant colour a state or conditional modifier falls
// back to when the design did not write the colour and gave no default arm.
func (c *Button) ModifierColorRole(name string) string {
	variant := c.Variant.Get()
	if variant == "" {
		variant = "filled"
	}
	switch name {
	case "background":
		switch variant {
		case "filled":
			return "theme:primary"
		case "tonal":
			return "theme:secondaryContainer"
		case "elevated":
			return "theme:surfaceContainerLow"
		}
	case "color":
		switch variant {
		case "filled":
			return "theme:onPrimary"
		case "tonal":
			return "theme:onSecondaryContainer"
		}
		return "theme:primary"
	}
	return ""
}

// IconButton is a button that shows only an icon.
type IconButton struct {
	Component
	// IconColor overrides the glyph colour; zero keeps the variant's role.
	IconColor Prop[uint32]
}

// Kind implements Node.
func (*IconButton) Kind() string { return "IconButton" }
func (c *IconButton) namedProps() []NamedProp {
	return append(c.propsExcept("IconButton", "iconColor"),
		ColorProp(&c.NodeBase, "iconColor", DirtyPaint, &c.IconColor))
}

// FAB is a floating action button.
type FAB struct{ Component }

// Kind implements Node.
func (*FAB) Kind() string              { return "FAB" }
func (c *FAB) namedProps() []NamedProp { return c.props("FAB") }

// FABMenu opens related actions from a FAB.
type FABMenu struct{ Component }

// Kind implements Node.
func (*FABMenu) Kind() string              { return "FABMenu" }
func (c *FABMenu) namedProps() []NamedProp { return c.props("FABMenu") }

// ButtonGroup is a connected group of buttons.
type ButtonGroup struct{ Component }

// Kind implements Node.
func (*ButtonGroup) Kind() string              { return "ButtonGroup" }
func (c *ButtonGroup) namedProps() []NamedProp { return c.props("ButtonGroup") }

// SplitButton is a primary action plus a menu.
type SplitButton struct{ Component }

// Kind implements Node.
func (*SplitButton) Kind() string              { return "SplitButton" }
func (c *SplitButton) namedProps() []NamedProp { return c.props("SplitButton") }

// SegmentedButton is one segment of a segmented button group.
type SegmentedButton struct{ Component }

// Kind implements Node.
func (*SegmentedButton) Kind() string              { return "SegmentedButton" }
func (c *SegmentedButton) namedProps() []NamedProp { return c.props("SegmentedButton") }

// Chip is an assist, filter, input or suggestion chip.
type Chip struct{ Component }

// Kind implements Node.
func (*Chip) Kind() string              { return "Chip" }
func (c *Chip) namedProps() []NamedProp { return c.props("Chip") }

// Badge is a small count or status dot.
type Badge struct{ Component }

// Kind implements Node.
func (*Badge) Kind() string              { return "Badge" }
func (c *Badge) namedProps() []NamedProp { return c.props("Badge") }

// Checkbox selects one or more items.
type Checkbox struct{ Component }

// Kind implements Node.
func (*Checkbox) Kind() string              { return "Checkbox" }
func (c *Checkbox) namedProps() []NamedProp { return c.props("Checkbox") }

// RadioButton selects one item from a set.
type RadioButton struct{ Component }

// Kind implements Node.
func (*RadioButton) Kind() string              { return "RadioButton" }
func (c *RadioButton) namedProps() []NamedProp { return c.props("RadioButton") }

// Switch turns an option on or off.
type Switch struct{ Component }

// Kind implements Node.
func (*Switch) Kind() string              { return "Switch" }
func (c *Switch) namedProps() []NamedProp { return c.props("Switch") }

// Slider picks a value along a track. `variant: range` adds End as the second
// handle, and `variant: centered` fills from Center instead of the minimum.
type Slider struct {
	Component
	// Orientation is "horizontal" (the default) or "vertical".
	Orientation Prop[string]
	End         Prop[float64]
	// Center is NaN when unset, which means the midpoint of min and max.
	Center Prop[float64]
	// Grab is the handle the pointer took: 0 is Value, 1 is End. Paint uses it
	// to narrow only the handle being dragged.
	Grab int
	// ShowTooltip shows the value above the handle while it is hovered or
	// dragged, naming the handle a range slider would move.
	ShowTooltip Prop[bool]
	// HoverGrab is the handle under the pointer: 0 is Value, 1 is End.
	HoverGrab int
	// RangeEvent fires in range mode with both values, low first.
	RangeEvent Event[[2]float64]
}

// Kind implements Node.
func (*Slider) Kind() string { return "Slider" }

func (c *Slider) namedProps() []NamedProp {
	return append(c.propsExcept("Slider", "orientation", "end", "center", "showTooltip"),
		EnumProp(&c.NodeBase, "orientation", DirtyLayout, &c.Orientation,
			componentdef.EnumValuesFor("Slider", "orientation")),
		NumberProp(&c.NodeBase, "end", DirtyPaint, &c.End),
		NumberProp(&c.NodeBase, "center", DirtyPaint, &c.Center),
		BoolProp(&c.NodeBase, "showTooltip", DirtyPaint, &c.ShowTooltip),
	)
}

// Range reports the two values of a range slider, low first.
func (c *Slider) Range() (float64, float64) {
	a, b := c.Value.Get(), c.End.Get()
	if a > b {
		return b, a
	}
	return a, b
}

// SetRange sets both handles, clamped to min/max and kept in order.
func (c *Slider) SetRange(a, b float64) {
	lo, hi := c.Min.Get(), c.Max.Get()
	if hi <= lo {
		return
	}
	if a > b {
		a, b = b, a
	}
	c.Value.Set(math.Max(lo, math.Min(hi, a)))
	c.End.Set(math.Max(lo, math.Min(hi, b)))
}

// OnRange subscribes to a range slider's changes, with both values, low first.
func (c *Slider) OnRange(fn func(lo, hi float64)) (off func()) {
	return c.RangeEvent.On(func(v [2]float64) { fn(v[0], v[1]) })
}

// TextField is a filled or outlined text input. LeadingNode and TrailingNode
// hold the bodies built from its $leading and $trailing slots; an explicit
// leadingIcon or trailingIcon wins over the slot, the way a list row works.
type TextField struct {
	Component
	LeadingNode  Node
	TrailingNode Node
	// Type filters typed text: empty or "text" takes everything, "numeric"
	// digits, "alpha" letters, "alphanumeric" both, and any other value is
	// read as the set of characters to allow.
	Type Prop[string]
	// Mask formats the value: # is a digit, @ a letter, * either, and every
	// other character is a literal the field inserts itself.
	Mask Prop[string]
	// Color overrides the colour of the value, its label and its placeholder.
	// Zero keeps the scheme's onSurface.
	Color Prop[uint32]
	// Obscure hides the value the way a password field does: one dot per rune,
	// in the value, the selection, the caret and the IME composition alike.
	Obscure Prop[bool]
}

// Kind implements Node.
func (*TextField) Kind() string { return "TextField" }
func (c *TextField) namedProps() []NamedProp {
	b := &c.NodeBase
	return append(c.propsExcept("TextField", "color", "type", "mask", "obscureText"),
		StringProp(b, "type", &c.Type),
		StringProp(b, "mask", &c.Mask),
		ColorProp(b, "color", DirtyPaint, &c.Color),
		BoolProp(b, "obscureText", DirtyPaint, &c.Obscure))
}

// slotNodes exposes the field's icon-square bodies to the builder.
func (c *TextField) slotNodes() (*Node, *Node) { return &c.LeadingNode, &c.TrailingNode }

// SetObscureText hides or shows the value: a hidden field paints one dot per
// character and keeps the caret on the character it covers.
func (c *TextField) SetObscureText(v bool) { c.Obscure.Set(v) }

// MaskRune is what an obscured field paints for a character. One rune per rune
// of the value is what keeps the caret and the selection on the characters they
// cover, so the field looks the same length as the text it hides.
const MaskRune = "•"

// AutoFocusPending reports whether this field still has to take focus for its
// current appearance.
func (c *Component) AutoFocusPending() bool { return c.AutoFocus.Get() && !c.autoFocusDone }

// MarkAutoFocus records that the current appearance took focus.
func (c *Component) MarkAutoFocus() { c.autoFocusDone = true }

// ResetAutoFocus re-arms the field for a later appearance, or after autoFocus
// was turned off or the field was disabled.
func (c *Component) ResetAutoFocus() { c.autoFocusDone = false }

// Obscured reports whether the element is a text field hiding its value.
func (c *Component) Obscured() bool {
	tf, ok := c.Node().(*TextField)
	return ok && tf.Obscure.Get()
}

// LabelFloats reports whether a text field or area's label belongs above the
// field: a value, an IME composition, a placeholder or focus puts it there.
func (c *Component) LabelFloats() bool {
	return c.Text.Get() != "" || c.Preedit != "" || c.Focused || c.Placeholder.Get() != ""
}

// FieldGutters reports whether a text field spends its 40dp icon column on a
// side: an explicit icon and a $leading/$trailing slot both take it.
func (c *Component) FieldGutters() (leading, trailing bool) {
	tf, ok := c.Node().(*TextField)
	if !ok {
		return false, false
	}
	return c.Icon.Get() != "" || tf.LeadingNode != nil, c.Trailing.Get() != "" || tf.TrailingNode != nil
}

// FieldText is the value a field paints: s, or one MaskRune per rune when the
// field hides it.
func FieldText(s string, obscure bool) string {
	if !obscure {
		return s
	}
	return strings.Repeat(MaskRune, utf8.RuneCountInString(s))
}

// FieldIndex maps a byte index in a field's value to the byte index the same
// character occupies in FieldText, so the caret and the selection follow the
// dots.
func FieldIndex(s string, i int, obscure bool) int {
	if !obscure {
		return i
	}
	if i < 0 {
		i = 0
	}
	if i > len(s) {
		i = len(s)
	}
	return utf8.RuneCountInString(s[:i]) * len(MaskRune)
}

// FieldValueIndex is the other direction: an index in the masked text back to
// the value, so a click between two dots places the caret between the
// characters they stand for.
func FieldValueIndex(s string, i int, obscure bool) int {
	if !obscure {
		return i
	}
	if i <= 0 {
		return 0
	}
	pos := 0
	for n := i / len(MaskRune); n > 0 && pos < len(s); n-- {
		_, size := utf8.DecodeRuneInString(s[pos:])
		pos += size
	}
	return pos
}

// SearchBar is a rounded search input. LeadingNode and TrailingNode hold the
// bodies built from its $leading and $trailing slots; a $leading body replaces
// the built-in search glyph, and an explicit trailingIcon wins over the slot,
// the way a list row works.
type SearchBar struct {
	Component
	LeadingNode  Node
	TrailingNode Node
}

// Kind implements Node.
func (*SearchBar) Kind() string              { return "SearchBar" }
func (c *SearchBar) namedProps() []NamedProp { return c.props("SearchBar") }

// slotNodes exposes the bar's icon-square bodies to the builder.
func (c *SearchBar) slotNodes() (*Node, *Node) { return &c.LeadingNode, &c.TrailingNode }

// Tooltip is a small label attached to a control.
type Tooltip struct{ Component }

// Kind implements Node.
func (*Tooltip) Kind() string              { return "Tooltip" }
func (c *Tooltip) namedProps() []NamedProp { return c.props("Tooltip") }

// Card groups content about a single subject.
type Card struct{ Component }

// Kind implements Node.
func (*Card) Kind() string              { return "Card" }
func (c *Card) namedProps() []NamedProp { return c.props("Card") }

// Dialog asks for a decision in a modal layer.
type Dialog struct{ Component }

// Kind implements Node.
func (*Dialog) Kind() string              { return "Dialog" }
func (c *Dialog) namedProps() []NamedProp { return c.props("Dialog") }

// BottomSheet is a surface anchored to the bottom edge.
type BottomSheet struct{ Component }

// Kind implements Node.
func (*BottomSheet) Kind() string              { return "BottomSheet" }
func (c *BottomSheet) namedProps() []NamedProp { return c.props("BottomSheet") }

// SideSheet is a surface anchored to a side edge.
type SideSheet struct{ Component }

// Kind implements Node.
func (*SideSheet) Kind() string              { return "SideSheet" }
func (c *SideSheet) namedProps() []NamedProp { return c.props("SideSheet") }

// Divider separates content.
type Divider struct{ Component }

// Kind implements Node.
func (*Divider) Kind() string              { return "Divider" }
func (c *Divider) namedProps() []NamedProp { return c.props("Divider") }

// List is a vertical index of items.
type List struct{ Component }

// Kind implements Node.
func (*List) Kind() string              { return "List" }
func (c *List) namedProps() []NamedProp { return c.props("List") }

// ListItem is one row of a list. LeadingNode and TrailingNode hold the nodes
// built from its $leading and $trailing slots; an explicit leadingIcon or
// trailingIcon property wins over the slot, so the same design can take either.
type ListItem struct {
	Component
	LeadingNode  Node
	TrailingNode Node
}

// Kind implements Node.
func (*ListItem) Kind() string              { return "ListItem" }
func (c *ListItem) namedProps() []NamedProp { return c.props("ListItem") }

// slotNodes exposes the row's icon-square bodies to the builder.
func (c *ListItem) slotNodes() (*Node, *Node) { return &c.LeadingNode, &c.TrailingNode }

// Table is a data grid: TableColumn children declare the columns, TableRow
// children hold the cells, and the header sorts on click.
type Table struct {
	Component
	Striped       Prop[bool]
	ShowHeader    Prop[bool]
	StickyHeader  Prop[bool]
	RowHeight     Prop[float64]
	HeaderHeight  Prop[float64]
	SortColumn    Prop[float64]
	SortAscending Prop[bool]
	// Selectable is none, single or multiple; none is the default.
	Selectable  Prop[string]
	SelectedRow Prop[float64]
	// SelectionColor is the band under selected rows; zero follows
	// SecondaryContainer.
	SelectionColor Prop[uint32]
	// SortEvent fires when a header is clicked, with the column index and the
	// new direction. SortBy from Go does not fire it, because the caller
	// already knows.
	SortEvent Event[SortOrder]
}

// Kind implements Node.
func (*Table) Kind() string { return "Table" }

func (c *Table) namedProps() []NamedProp {
	return append(c.propsExcept("Table",
		"striped", "showHeader", "stickyHeader", "rowHeight", "headerHeight",
		"sortColumn", "sortAscending", "selectable", "selectedRow", "selectionColor"),
		BoolProp(&c.NodeBase, "striped", DirtyPaint, &c.Striped),
		BoolProp(&c.NodeBase, "showHeader", DirtyLayout, &c.ShowHeader),
		BoolProp(&c.NodeBase, "stickyHeader", DirtyLayout, &c.StickyHeader),
		NumberProp(&c.NodeBase, "rowHeight", DirtyLayout, &c.RowHeight),
		NumberProp(&c.NodeBase, "headerHeight", DirtyLayout, &c.HeaderHeight),
		NumberProp(&c.NodeBase, "sortColumn", DirtyLayout, &c.SortColumn),
		BoolProp(&c.NodeBase, "sortAscending", DirtyLayout, &c.SortAscending),
		EnumProp(&c.NodeBase, "selectable", DirtyPaint, &c.Selectable, TableSelectableValues),
		NumberProp(&c.NodeBase, "selectedRow", DirtyPaint, &c.SelectedRow),
		ColorProp(&c.NodeBase, "selectionColor", DirtyPaint, &c.SelectionColor),
	)
}

// SortOrder names a table sort: which column, by its key when it has one, and
// the direction.
type SortOrder struct {
	Column    int
	Key       string
	Ascending bool
}

// OnSort subscribes to header clicks.
func (c *Table) OnSort(fn func(SortOrder)) (off func()) { return c.SortEvent.On(fn) }

// TableColumn declares one column: its label, the key Go can sort by, its width
// (0 shares the leftover space), alignment and whether the header sorts.
type TableColumn struct {
	Component
	Key      Prop[string]
	Align    Prop[string]
	Sortable Prop[bool]
	Numeric  Prop[bool]
}

// Kind implements Node.
func (*TableColumn) Kind() string { return "TableColumn" }

func (c *TableColumn) namedProps() []NamedProp {
	return append(c.propsExcept("TableColumn", "key", "align", "sortable", "numeric"),
		StringProp(&c.NodeBase, "key", &c.Key),
		EnumProp(&c.NodeBase, "align", DirtyPaint, &c.Align, AlignValues),
		BoolProp(&c.NodeBase, "sortable", DirtyPaint, &c.Sortable),
		BoolProp(&c.NodeBase, "numeric", DirtyPaint, &c.Numeric),
	)
}

// TableRow is one row; its children are TableCells, one per column.
type TableRow struct{ Component }

// Kind implements Node.
func (*TableRow) Kind() string { return "TableRow" }

func (c *TableRow) namedProps() []NamedProp { return c.props("TableRow") }

// TableCell is one cell in a row. Its children are what a reader sees, so the
// cell can hold a Text, a Chip, anything; Value is the machine-readable string
// sorting uses, and may differ from what is drawn (`"31"` over a formatted
// date).
type TableCell struct {
	Component
	Value Prop[string]
	Align Prop[string]
}

// Kind implements Node.
func (*TableCell) Kind() string { return "TableCell" }

func (c *TableCell) namedProps() []NamedProp {
	return append(c.propsExcept("TableCell", "value", "align"),
		ScalarProp(&c.NodeBase, "value", DirtyLayout, &c.Value),
		EnumProp(&c.NodeBase, "align", DirtyPaint, &c.Align, AlignValues),
	)
}

// Menu is a list of actions in an overlay.
type Menu struct{ Component }

// Kind implements Node.
func (*Menu) Kind() string              { return "Menu" }
func (c *Menu) namedProps() []NamedProp { return c.props("Menu") }

// MenuItem is one action in a menu.
type MenuItem struct{ Component }

// Kind implements Node.
func (*MenuItem) Kind() string              { return "MenuItem" }
func (c *MenuItem) namedProps() []NamedProp { return c.props("MenuItem") }

// Snackbar is a short message at the bottom of the screen.
type Snackbar struct{ Component }

// Kind implements Node.
func (*Snackbar) Kind() string              { return "Snackbar" }
func (c *Snackbar) namedProps() []NamedProp { return c.props("Snackbar") }

// TopAppBar is the header of a screen.
type TopAppBar struct{ Component }

// Kind implements Node.
func (*TopAppBar) Kind() string              { return "TopAppBar" }
func (c *TopAppBar) namedProps() []NamedProp { return c.props("TopAppBar") }

// NavigationBar is the bottom navigation for compact windows.
type NavigationBar struct{ Component }

// Kind implements Node.
func (*NavigationBar) Kind() string { return "NavigationBar" }
func (c *NavigationBar) namedProps() []NamedProp {
	return append(c.props("NavigationBar"),
		EnumProp(&c.NodeBase, "animation", DirtyPaint, &c.Animation, componentdef.EnumValues("animation")))
}

// NavigationRail is the side navigation for medium windows.
type NavigationRail struct{ Component }

// Kind implements Node.
func (*NavigationRail) Kind() string { return "NavigationRail" }
func (c *NavigationRail) namedProps() []NamedProp {
	return append(c.props("NavigationRail"),
		EnumProp(&c.NodeBase, "animation", DirtyPaint, &c.Animation, componentdef.EnumValues("animation")))
}

// NavigationDrawer is the side panel navigation for expanded windows.
type NavigationDrawer struct{ Component }

// Kind implements Node.
func (*NavigationDrawer) Kind() string { return "NavigationDrawer" }
func (c *NavigationDrawer) namedProps() []NamedProp {
	return append(c.props("NavigationDrawer"),
		EnumProp(&c.NodeBase, "animation", DirtyPaint, &c.Animation, componentdef.EnumValues("animation")))
}

// NavigationItem is one destination in a bar, rail or drawer. Each part has a
// colour of its own, in both states: the label and the icon read their plain
// colour when idle and their selected one while active, and the indicator
// behind them takes SelectedContainerColor.
type NavigationItem struct {
	Component
	IconColor              Prop[uint32]
	SelectedLabelColor     Prop[uint32]
	SelectedIconColor      Prop[uint32]
	SelectedContainerColor Prop[uint32]
}

// Kind implements Node.
func (*NavigationItem) Kind() string { return "NavigationItem" }
func (c *NavigationItem) namedProps() []NamedProp {
	b := &c.NodeBase
	return append(c.propsExcept("NavigationItem",
		"iconColor", "selectedLabelColor", "selectedIconColor", "selectedContainerColor"),
		ColorProp(b, "iconColor", DirtyPaint, &c.IconColor),
		ColorProp(b, "selectedLabelColor", DirtyPaint, &c.SelectedLabelColor),
		ColorProp(b, "selectedIconColor", DirtyPaint, &c.SelectedIconColor),
		ColorProp(b, "selectedContainerColor", DirtyPaint, &c.SelectedContainerColor),
	)
}

// Breadcrumb is a trail of destinations: its children are the crumbs, and it
// draws the separator between them, so a design composes whatever each crumb
// needs (a Text, a link, an Icon) instead of naming fields.
type Breadcrumb struct {
	Component
	Separator      Prop[string]
	SeparatorColor Prop[uint32]
	SeparatorSize  Prop[float64]
}

// Kind implements Node.
func (*Breadcrumb) Kind() string { return "Breadcrumb" }
func (c *Breadcrumb) namedProps() []NamedProp {
	b := &c.NodeBase
	return append(c.propsExcept("Breadcrumb", "separator", "separatorColor", "separatorSize"),
		StringProp(b, "separator", &c.Separator),
		ColorProp(b, "separatorColor", DirtyPaint, &c.SeparatorColor),
		NumberProp(b, "separatorSize", DirtyLayout, &c.SeparatorSize),
	)
}

// Tabs is a row of tabs.
type Tabs struct{ Component }

// Kind implements Node.
func (*Tabs) Kind() string              { return "Tabs" }
func (c *Tabs) namedProps() []NamedProp { return c.props("Tabs") }

// Tab is one tab.
type Tab struct{ Component }

// Kind implements Node.
func (*Tab) Kind() string              { return "Tab" }
func (c *Tab) namedProps() []NamedProp { return c.props("Tab") }

// LinearProgress shows progress along a line.
type LinearProgress struct{ Component }

// Kind implements Node.
func (*LinearProgress) Kind() string              { return "LinearProgress" }
func (c *LinearProgress) namedProps() []NamedProp { return c.props("LinearProgress") }

// CircularProgress shows progress on a ring.
type CircularProgress struct{ Component }

// Kind implements Node.
func (*CircularProgress) Kind() string              { return "CircularProgress" }
func (c *CircularProgress) namedProps() []NamedProp { return c.props("CircularProgress") }

// Icon is one Material Symbols glyph. Colour is its own cell, the way a
// LoadingIndicator's is: the glyph is the component, so a design tints it
// directly rather than through a background. The box comes from the common
// `size` (or width/height) properties, and the glyph fills its shorter side.
type Icon struct {
	Component
	Color Prop[uint32]
}

// Kind implements Node.
func (*Icon) Kind() string { return "Icon" }
func (c *Icon) namedProps() []NamedProp {
	return append(c.propsExcept("Icon", "color"),
		ColorProp(&c.NodeBase, "color", DirtyPaint, &c.Color),
	)
}

// LoadingIndicator is the expressive loading indicator: a rotating, morphing
// shape. Color is its own cell because the shape is the component, so a design
// tints it directly rather than through a background.
type LoadingIndicator struct {
	Component
	Color Prop[uint32]
}

// Kind implements Node.
func (*LoadingIndicator) Kind() string { return "LoadingIndicator" }
func (c *LoadingIndicator) namedProps() []NamedProp {
	return append(c.propsExcept("LoadingIndicator", "color"),
		ColorProp(&c.NodeBase, "color", DirtyPaint, &c.Color),
	)
}

// Skeleton is a placeholder for content that has not arrived: a rounded block
// whose shimmer says "loading". Size and radius come from the common box
// properties, so one element stands in for a line of text or a card. Color is
// the block and Highlight the band that sweeps across it; Animated turns the
// sweep off, which is what a screenshot or a reduced-motion build wants.
type Skeleton struct {
	Component
	Color     Prop[uint32]
	Highlight Prop[uint32]
	Animated  Prop[bool]
}

// Kind implements Node.
func (*Skeleton) Kind() string { return "Skeleton" }
func (c *Skeleton) namedProps() []NamedProp {
	return append(c.propsExcept("Skeleton", "color", "highlight", "animated"),
		ColorProp(&c.NodeBase, "color", DirtyPaint, &c.Color),
		ColorProp(&c.NodeBase, "highlight", DirtyPaint, &c.Highlight),
		BoolProp(&c.NodeBase, "animated", DirtyPaint, &c.Animated),
	)
}

// DatePicker picks a date.
type DatePicker struct{ Component }

// Kind implements Node.
func (*DatePicker) Kind() string              { return "DatePicker" }
func (c *DatePicker) namedProps() []NamedProp { return c.props("DatePicker") }

// ColorPicker picks a colour from a saturation/value square, a hue bar and an
// alpha bar. The value is `#rrggbb`, or `#rrggbbaa` when translucent, in Text;
// the hex field edits it.
type ColorPicker struct {
	Component
	// Hue, Sat and Val are the HSV the controls show. Hue survives a black or
	// grey value, which the hex alone cannot express.
	Hue, Sat, Val float64
	// Alpha is 0 (transparent) to 1 (opaque).
	Alpha float64
	// HexActive is true while the hex field takes typing, which is when the
	// component is also the interactor's text focus.
	HexActive bool
	// Cancel fires when Cancel is pressed and OK when OK or Enter confirms,
	// both with the current value.
	Cancel Event[string]
	OK     Event[string]
	// picked is the Text the HSV above was read from, so a value set from Go
	// re-seeds the controls once instead of on every frame.
	picked string
}

// Kind implements Node.
func (*ColorPicker) Kind() string              { return "ColorPicker" }
func (c *ColorPicker) namedProps() []NamedProp { return c.props("ColorPicker") }

// OnCancel subscribes to Cancel, with the current value.
func (c *ColorPicker) OnCancel(fn func(string)) (off func()) { return c.Cancel.On(fn) }

// OnOK subscribes to OK, with the picked value.
func (c *ColorPicker) OnOK(fn func(string)) (off func()) { return c.OK.On(fn) }

// HSV returns the colour the controls show, following Text when the design or
// the app changed it.
func (c *ColorPicker) HSV() (h, s, v float64) {
	if text := c.Text.Get(); text != c.picked {
		if h, s, v, a, ok := hexHSVA(text); ok {
			c.Hue, c.Sat, c.Val, c.Alpha = h, s, v, a
		}
		c.picked = text
	}
	return c.Hue, c.Sat, c.Val
}

// SetHSV writes the colour and returns it, keeping the alpha it already has.
func (c *ColorPicker) SetHSV(h, s, v float64) string {
	return c.SetHSVA(h, s, v, c.Alpha)
}

// SetHSVA writes the colour with an alpha and returns it, as `#rrggbb` when
// opaque and `#rrggbbaa` otherwise.
func (c *ColorPicker) SetHSVA(h, s, v, a float64) string {
	// Keep 360 rather than folding it to 0 so the marker stays at the right
	// edge of the hue bar; the conversions treat it as the same red.
	if h < 0 || h > 360 {
		h = math.Mod(h, 360)
		if h < 0 {
			h += 360
		}
	}
	c.Hue, c.Sat, c.Val, c.Alpha = h, clamp01(s), clamp01(v), clamp01(a)
	hex := hsvHex(c.Hue, c.Sat, c.Val, c.Alpha)
	c.picked = hex
	c.Text.Set(hex)
	return hex
}

// Hex is the current colour, read the way the picker shows it.
func (c *ColorPicker) Hex() string {
	h, s, v := c.HSV()
	return hsvHex(h, s, v, c.Alpha)
}

// ValidHex reads the typed value as a colour, accepting a missing `#` on a bare
// hex run. It updates HSV when it parses and returns the normalized spelling.
func (c *ColorPicker) ValidHex() (string, bool) {
	text := strings.TrimSpace(c.Text.Get())
	if text != "" && text[0] != '#' && isHexRun(text) {
		text = "#" + text
	}
	h, s, v, a, ok := hexHSVA(text)
	if !ok {
		return "", false
	}
	c.Hue, c.Sat, c.Val, c.Alpha = h, s, v, a
	c.picked = c.Text.Get()
	return hsvHex(h, s, v, a), true
}

// CommitHex normalizes the field to a full `#rrggbb`, falling back to the last
// colour that parsed. It is what OK, blur and a wrong-length value end on.
func (c *ColorPicker) CommitHex() string {
	hex, ok := c.ValidHex()
	if !ok {
		hex = c.Hex()
	}
	c.picked = hex
	c.Text.Set(hex)
	return hex
}

// TimePicker picks a time. Variant "dial" shows the clock and "input" the two
// text fields.
type TimePicker struct {
	Component
	// HourFormat is 12 (the default, with an AM/PM switch) or 24. The value is
	// always stored as 24-hour; only the display and the dial change.
	HourFormat Prop[float64]
	// PickBuffer and PickDigits are the digits typed into the active field
	// since it was focused, which is what lets a second digit complete "07".
	PickBuffer, PickDigits int
	// Cancel fires when the Cancel action is pressed, with the current value.
	Cancel Event[string]
	// OK fires when the OK action or Enter confirms, with the value.
	OK Event[string]
}

// Kind implements Node.
func (*TimePicker) Kind() string { return "TimePicker" }
func (c *TimePicker) namedProps() []NamedProp {
	return append(c.props("TimePicker"),
		NumberProp(&c.NodeBase, "hourFormat", DirtyPaint, &c.HourFormat),
	)
}

// OnCancel subscribes to the Cancel action of a time picker.
func (c *TimePicker) OnCancel(fn func(string)) (off func()) { return c.Cancel.On(fn) }

// OnOK subscribes to the OK action of a time picker, with the picked value.
func (c *TimePicker) OnOK(fn func(string)) (off func()) { return c.OK.On(fn) }

// TextArea is a multiline text field.
type TextArea struct{ Component }

// Kind implements Node.
func (*TextArea) Kind() string              { return "TextArea" }
func (c *TextArea) namedProps() []NamedProp { return c.props("TextArea") }

// Code is a syntax-highlighted code block.
type Code struct {
	Component
	// SelA and SelB are the reader's selection in the drawn text's byte
	// offsets. A Code block is always selectable; only a Text node needs the
	// property, since prose and code are read differently.
	SelA, SelB int
	// The highlight is recomputed only when the text or the language changes,
	// so a still block costs a string compare per frame instead of a lexer run.
	hlLang, hlText string
	hlSrc          string
	hlSpans        []highlight.Span
}

// Kind implements Node.
func (*Code) Kind() string { return "Code" }

func (c *Code) namedProps() []NamedProp { return c.props("Code") }

// Source is the text the block draws. Tabs are expanded here because the font
// gives a tab no advance of its own, which would flatten all indentation; the
// selection and the copy button use the same string, so what is highlighted is
// what is copied.
func (c *Code) Source() string {
	src, _ := c.highlight()
	return src
}

// Spans returns the highlighted runs for the current text and language,
// recomputing them only when one of the two changed.
func (c *Code) Spans() []highlight.Span {
	_, spans := c.highlight()
	return spans
}

func (c *Code) highlight() (string, []highlight.Span) {
	raw, lang := c.Text.Get(), c.Language.Get()
	if raw == c.hlText && lang == c.hlLang {
		return c.hlSrc, c.hlSpans
	}
	c.hlText, c.hlLang = raw, lang
	c.hlSrc = strings.ReplaceAll(raw, "\t", "    ")
	c.hlSpans = highlight.Spans(c.hlSrc, lang)
	return c.hlSrc, c.hlSpans
}

// Carousel shows a scrollable row of items whose width follows the scroll: the
// leading card is full size and the next one grows into place.
type Carousel struct {
	Component
	// Shrink is the share of a card's width kept when it is not the leading one.
	Shrink Prop[float64]
}

// Kind implements Node.
func (*Carousel) Kind() string { return "Carousel" }
func (c *Carousel) namedProps() []NamedProp {
	return append(c.propsExcept("Carousel", "shrink"),
		NumberProp(&c.NodeBase, "shrink", DirtyLayout, &c.Shrink))
}

// CarouselItem is one card in a Carousel. It clips its child to a rounded box,
// paints a label over a bottom scrim and takes a press like a card.
type CarouselItem struct{ Component }

// Kind implements Node.
func (*CarouselItem) Kind() string              { return "CarouselItem" }
func (c *CarouselItem) namedProps() []NamedProp { return c.props("CarouselItem") }

// Toolbar is a floating row of actions.
type Toolbar struct{ Component }

// Kind implements Node.
func (*Toolbar) Kind() string              { return "Toolbar" }
func (c *Toolbar) namedProps() []NamedProp { return c.props("Toolbar") }

// M3 exposes the shared component state to the painter.
func (c *Component) M3() *Component { return c }
