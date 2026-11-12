// Package schema defines every element and property the .goi language accepts,
// along with the literal type each property requires. It is the single place
// that knows the vocabulary; the parser stays generic.
package schema

import (
	"sort"

	"github.com/goi-gui/goi/internal/componentdef"
)

// ValueType is the literal type a property demands.
type ValueType int

// Value types. Enum and String accept an Ident and a String respectively; the
// parser keeps them distinct so diagnostics can name the expected form.
const (
	TypeString ValueType = iota
	TypeNumber
	TypeBool
	TypeColor
	TypeEnum
	TypeID
	// TypeShadow is the CSS-style shadow list: `2 2 5 #000000 inset`.
	TypeShadow
	// TypeScalar accepts a string, number or bool, which is what a table cell's
	// sort value is: whatever the row's data holds.
	TypeScalar
	// TypeRotate is the CSS rotate value: none, an angle, an axis plus angle, or
	// a vector plus angle.
	TypeRotate
)

func (t ValueType) String() string {
	switch t {
	case TypeString:
		return "string"
	case TypeNumber:
		return "number"
	case TypeBool:
		return "bool"
	case TypeColor:
		return "color"
	case TypeEnum:
		return "identifier"
	case TypeID:
		return "id"
	case TypeShadow:
		return "shadow"
	case TypeScalar:
		return "string, number or bool"
	case TypeRotate:
		return "a rotate value"
	}
	return "unknown"
}

// DirtyKind describes what a property affects when it changes.
type DirtyKind int

// Dirty kinds.
const (
	DirtyNone DirtyKind = iota
	DirtyPaint
	DirtyLayout
)

// Prop describes one property of one element.
type Prop struct {
	Name    string
	Type    ValueType
	Dirty   DirtyKind
	Enum    []string // valid values for TypeEnum
	Default any      // string/float64/bool/uint32/enum string
	// Common marks properties every element accepts.
	Common bool
}

// Element describes one element type.
type Element struct {
	Name     string
	Children bool // may contain child elements
	Attrs    []Prop
	// Events lists the event properties the element accepts. A value is always
	// a bridge handler call, never a literal.
	Events []string
}

// Prop returns the named property, or nil.
func (e *Element) Prop(name string) *Prop {
	for i := range e.Attrs {
		if e.Attrs[i].Name == name {
			return &e.Attrs[i]
		}
	}
	return nil
}

// HasEvent reports whether the element accepts the named event property.
func (e *Element) HasEvent(name string) bool {
	for _, n := range e.Events {
		if n == name {
			return true
		}
	}
	return false
}

// EventDef is one event property and the value it carries.
type EventDef struct {
	Name string
	// Type and HasValue describe the event's own payload: onToggle carries a
	// bool, onChange a number, onEdit and onSubmit a string, onSelect an index.
	// A click carries a ClickEvent, which no scalar type names.
	Type     ValueType
	HasValue bool
	// Component marks the events only a component has; onClick is on every
	// element because every node is a hit area.
	Component bool
}

// NodeMethods are the methods an event can call on an element by id, as in
// `onClick: spin.pause()`. They are the transport of an animated Image or SVG,
// and of an Audio or Video player.
var NodeMethods = []string{"play", "pause", "stop", "toggle", "seek", "volume",
	"fullscreen", "windowed", "toggleFullscreen", "audioTrack"}

// NodeMethod reports whether name is a method an event can call on an element.
func NodeMethod(name string) bool {
	for _, m := range NodeMethods {
		if m == name {
			return true
		}
	}
	return false
}

// Event returns the definition of an event property.
func Event(name string) (EventDef, bool) {
	for _, e := range eventDefs {
		if e.Name == name {
			return e, true
		}
	}
	return EventDef{}, false
}

// CursorValues lists the CSS cursor names a design may write. Every CSS value
// is accepted; the platform maps the ones SDL has no shape for to the closest
// one, which is what a browser does with a cursor the OS lacks.
var CursorValues = []string{
	"auto", "default", "none",
	"context-menu", "help", "pointer", "progress", "wait",
	"cell", "crosshair", "text", "vertical-text",
	"alias", "copy", "move", "no-drop", "not-allowed", "grab", "grabbing",
	"all-scroll", "col-resize", "row-resize",
	"n-resize", "e-resize", "s-resize", "w-resize",
	"ne-resize", "nw-resize", "se-resize", "sw-resize",
	"ew-resize", "ns-resize", "nesw-resize", "nwse-resize",
	"zoom-in", "zoom-out",
}

var eventDefs = []EventDef{
	{Name: "onClick", Type: TypeString},
	{Name: "onToggle", Type: TypeBool, HasValue: true, Component: true},
	{Name: "onChange", Type: TypeNumber, HasValue: true, Component: true},
	{Name: "onEdit", Type: TypeString, HasValue: true, Component: true},
	{Name: "onSubmit", Type: TypeString, HasValue: true, Component: true},
	{Name: "onSelect", Type: TypeNumber, HasValue: true, Component: true},
	{Name: "onHover", Type: TypeNumber, HasValue: true, Component: true},
	// Media: the payload is the position in seconds.
	{Name: "onPlay", Type: TypeNumber, HasValue: true, Component: true},
	{Name: "onPause", Type: TypeNumber, HasValue: true, Component: true},
	{Name: "onEnd", Type: TypeNumber, HasValue: true, Component: true},
}

var common = []Prop{
	{Name: "id", Type: TypeID, Dirty: DirtyNone, Common: true},
	{Name: "width", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true}, // -1 = auto
	{Name: "height", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "minWidth", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0, Common: true},
	{Name: "maxWidth", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "minHeight", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0, Common: true},
	{Name: "maxHeight", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "margin", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0, Common: true},
	{Name: "padding", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0, Common: true},
	{Name: "paddingX", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "paddingY", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	// -1 means "follow margin/padding", so a side can be set on its own.
	{Name: "marginTop", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "marginRight", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "marginBottom", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "marginLeft", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "paddingTop", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "paddingRight", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "paddingBottom", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "paddingLeft", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "background", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0), Common: true},
	{Name: "borderColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0), Common: true},
	{Name: "borderWidth", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0, Common: true},
	// A side width or colour overrides borderWidth/borderColor for that side;
	// -1 follows borderWidth, and 0 follows borderColor.
	{Name: "borderTopWidth", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "borderRightWidth", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "borderBottomWidth", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "borderLeftWidth", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0, Common: true},
	{Name: "borderTopColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0), Common: true},
	{Name: "borderRightColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0), Common: true},
	{Name: "borderBottomColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0), Common: true},
	{Name: "borderLeftColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0), Common: true},
	{Name: "radius", Type: TypeNumber, Dirty: DirtyPaint, Default: 0.0, Common: true},
	// A side radius rounds both corners on that side; a corner radius rounds
	// that one corner. -1 follows the broader property.
	{Name: "radiusTop", Type: TypeNumber, Dirty: DirtyPaint, Default: -1.0, Common: true},
	{Name: "radiusRight", Type: TypeNumber, Dirty: DirtyPaint, Default: -1.0, Common: true},
	{Name: "radiusBottom", Type: TypeNumber, Dirty: DirtyPaint, Default: -1.0, Common: true},
	{Name: "radiusLeft", Type: TypeNumber, Dirty: DirtyPaint, Default: -1.0, Common: true},
	{Name: "radiusTopLeft", Type: TypeNumber, Dirty: DirtyPaint, Default: -1.0, Common: true},
	{Name: "radiusTopRight", Type: TypeNumber, Dirty: DirtyPaint, Default: -1.0, Common: true},
	{Name: "radiusBottomRight", Type: TypeNumber, Dirty: DirtyPaint, Default: -1.0, Common: true},
	{Name: "radiusBottomLeft", Type: TypeNumber, Dirty: DirtyPaint, Default: -1.0, Common: true},
	// The font family a node draws in; children inherit it.
	{Name: "fontFamily", Type: TypeString, Dirty: DirtyLayout, Default: "", Common: true},
	// A CSS-style shadow: on Text it shadows the glyphs, everywhere else the
	// box. It paints outside the layout box, so it never changes layout.
	{Name: "shadow", Type: TypeShadow, Dirty: DirtyPaint, Common: true},
	{Name: "visible", Type: TypeBool, Dirty: DirtyLayout, Default: true, Common: true},
	{Name: "flex", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0, Common: true},
	{Name: "alignSelf", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "", Common: true},
	{Name: "scroll", Type: TypeBool, Dirty: DirtyPaint, Default: false, Common: true},
	// Scrollbar and scroll-shadow behaviour on any scrollable element.
	{Name: "hideScrollbar", Type: TypeBool, Dirty: DirtyPaint, Default: false, Common: true},
	{Name: "autoHideScrollbar", Type: TypeBool, Dirty: DirtyPaint, Default: true, Common: true},
	{Name: "scrollbarWidth", Type: TypeNumber, Dirty: DirtyPaint, Default: 4.0, Common: true},
	{Name: "scrollbarColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0), Common: true},
	{Name: "scrollShadow", Type: TypeBool, Dirty: DirtyPaint, Default: false, Common: true},
	{Name: "scrollShadowColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0), Common: true},
	// opacity fades the node and its subtree. It also accepts a percentage.
	{Name: "opacity", Type: TypeNumber, Dirty: DirtyPaint, Default: 1.0, Common: true},
	// zIndex orders a node among its siblings when painting; layout ignores it.
	{Name: "zIndex", Type: TypeNumber, Dirty: DirtyPaint, Default: 0.0, Common: true},
	// cursor is the pointer shape, named the way CSS names it.
	{Name: "cursor", Type: TypeEnum, Dirty: DirtyPaint, Enum: CursorValues, Default: "", Common: true},
	{Name: "template", Type: TypeBool, Dirty: DirtyLayout, Default: false, Common: true},
	// rotate turns the element and everything under it, the way CSS rotate
	// does. Layout ignores it, so the box keeps its place.
	{Name: "rotate", Type: TypeRotate, Dirty: DirtyPaint, Common: true},
}

func withCommon(extra ...Prop) []Prop {
	out := make([]Prop, len(common), len(common)+len(extra))
	copy(out, common)
	return append(out, extra...)
}

// textRoles are the M3 type scale names accepted by Text.role.
var textRoles = []string{
	"displayLarge", "displayMedium", "displaySmall",
	"headlineLarge", "headlineMedium", "headlineSmall",
	"titleLarge", "titleMedium", "titleSmall",
	"bodyLarge", "bodyMedium", "bodySmall",
	"labelLarge", "labelMedium", "labelSmall",
}

var elements = func() []Element {
	out := []Element{
		{Name: "Window", Children: true, Attrs: withCommon(
			Prop{Name: "title", Type: TypeString, Dirty: DirtyNone, Default: ""},
			// Any scheme the app defined with Window.DefineTheme; `light` and
			// `dark` are always available.
			Prop{Name: "theme", Type: TypeString, Dirty: DirtyPaint, Default: "light"},
			// system keeps the platform's title bar; custom hides it, so the
			// design draws its own inside a TitleBar.
			Prop{Name: "titleBar", Type: TypeEnum, Dirty: DirtyNone, Enum: []string{"system", "custom"}, Default: "system"},
			// The window chrome: the platform reads both on every frame, so a
			// design or Go can flip them without a reload.
			// `resizable` is the readable spelling; `disableResize` is kept so an
			// older design keeps working. Either false turns resizing off.
			Prop{Name: "resizable", Type: TypeBool, Dirty: DirtyNone, Default: true},
			Prop{Name: "disableResize", Type: TypeBool, Dirty: DirtyNone, Default: false},
			Prop{Name: "minimizable", Type: TypeBool, Dirty: DirtyNone, Default: true},
			Prop{Name: "maximizable", Type: TypeBool, Dirty: DirtyNone, Default: true},
			// The two state props: writing one asks the window to move there, and
			// the platform reports the real state back every frame.
			Prop{Name: "minimized", Type: TypeBool, Dirty: DirtyNone, Default: false},
			Prop{Name: "maximized", Type: TypeBool, Dirty: DirtyNone, Default: false},
			Prop{Name: "fullscreen", Type: TypeBool, Dirty: DirtyNone, Default: false},
		)},
		{Name: "Column", Children: true, Attrs: withCommon(
			Prop{Name: "spacing", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "gap", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "crossAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "start"},
			Prop{Name: "mainAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "spaceBetween", "spaceAround", "spaceEvenly"}, Default: "start"},
		)},
		{Name: "Row", Children: true, Attrs: withCommon(
			Prop{Name: "spacing", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "gap", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "crossAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "start"},
			Prop{Name: "mainAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "spaceBetween", "spaceAround", "spaceEvenly"}, Default: "start"},
		)},
		{Name: "TitleBar", Children: true, Attrs: withCommon(
			Prop{Name: "spacing", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "gap", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "crossAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "start"},
			Prop{Name: "mainAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "spaceBetween", "spaceAround", "spaceEvenly"}, Default: "start"},
		)},
		{Name: "Stack", Children: true, Attrs: withCommon(
			Prop{Name: "crossAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "start"},
			// An overlay places one box at a time, so only the geometric
			// alignments mean anything; spaceBetween has nothing to space.
			Prop{Name: "mainAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "start"},
		)},
		{Name: "ClipRRect", Children: true, Attrs: withCommon(
			Prop{Name: "crossAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "start"},
			Prop{Name: "mainAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "start"},
		)},
		{Name: "Positioned", Children: true, Attrs: withCommon(
			Prop{Name: "left", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0}, // -1 = auto
			Prop{Name: "top", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0},
			Prop{Name: "right", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0},
			Prop{Name: "bottom", Type: TypeNumber, Dirty: DirtyLayout, Default: -1.0},
			Prop{Name: "translateX", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "translateY", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
		)},
		{Name: "Wrap", Children: true, Attrs: withCommon(
			Prop{Name: "spacing", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "gap", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "crossAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "start"},
			Prop{Name: "mainAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "spaceBetween", "spaceAround", "spaceEvenly"}, Default: "start"},
		)},
		{Name: "Grid", Children: true, Attrs: withCommon(
			Prop{Name: "columns", Type: TypeNumber, Dirty: DirtyLayout, Default: 2.0},
			Prop{Name: "spacing", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "gap", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "crossAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "start"},
			Prop{Name: "mainAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "spaceBetween", "spaceAround", "spaceEvenly"}, Default: "start"},
		)},
		{Name: "Rectangle", Children: false, Attrs: withCommon()},
		{Name: "Image", Children: false, Attrs: withCommon(
			Prop{Name: "src", Type: TypeString, Dirty: DirtyLayout, Default: ""},
			Prop{Name: "fit", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"cover", "contain", "fill"}, Default: "cover"},
			Prop{Name: "paused", Type: TypeBool, Dirty: DirtyPaint, Default: false},
			Prop{Name: "loop", Type: TypeBool, Dirty: DirtyPaint, Default: true},
		)},
		{Name: "SVG", Children: false, Attrs: withCommon(
			Prop{Name: "src", Type: TypeString, Dirty: DirtyLayout, Default: ""},
			Prop{Name: "fit", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"cover", "contain", "fill"}, Default: "contain"},
			Prop{Name: "color", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
			Prop{Name: "paused", Type: TypeBool, Dirty: DirtyPaint, Default: false},
			Prop{Name: "loop", Type: TypeBool, Dirty: DirtyPaint, Default: true},
		)},
		{Name: "Text", Children: false, Attrs: withCommon(
			Prop{Name: "text", Type: TypeString, Dirty: DirtyLayout, Default: ""},
			Prop{Name: "fontSize", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "weight", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "fontWeight", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "fontStyle", Type: TypeEnum, Dirty: DirtyPaint, Enum: []string{"normal", "italic"}, Default: "normal"},
			Prop{Name: "decoration", Type: TypeString, Dirty: DirtyPaint, Default: "none"},
			Prop{Name: "decorationStyle", Type: TypeEnum, Dirty: DirtyPaint, Enum: []string{"solid", "wavy", "dotted", "double", "dashed"}, Default: "solid"},
			Prop{Name: "decorationThickness", Type: TypeNumber, Dirty: DirtyPaint, Default: 0.0},
			Prop{Name: "decorationColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
			Prop{Name: "selectable", Type: TypeBool, Dirty: DirtyPaint, Default: false},
			Prop{Name: "color", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
			Prop{Name: "align", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end"}, Default: "start"},
			Prop{Name: "wrap", Type: TypeBool, Dirty: DirtyLayout, Default: false},
			Prop{Name: "role", Type: TypeEnum, Dirty: DirtyLayout, Enum: textRoles, Default: "bodyMedium"},
		)},
	}
	// Media players. They are not Material components: their chrome is a control
	// bar, not a shape from the type scale, so the properties are declared here
	// beside Text and Image.
	out = append(out,
		Element{Name: "Audio", Attrs: withCommon(
			Prop{Name: "src", Type: TypeString, Dirty: DirtyLayout, Default: ""},
			Prop{Name: "controls", Type: TypeBool, Dirty: DirtyLayout, Default: true},
			Prop{Name: "autoplay", Type: TypeBool, Dirty: DirtyPaint, Default: false},
			Prop{Name: "loop", Type: TypeBool, Dirty: DirtyPaint, Default: false},
			Prop{Name: "muted", Type: TypeBool, Dirty: DirtyPaint, Default: false},
			Prop{Name: "volume", Type: TypeNumber, Dirty: DirtyPaint, Default: 1.0},
			Prop{Name: "startAt", Type: TypeNumber, Dirty: DirtyPaint, Default: 0.0},
			Prop{Name: "buffer", Type: TypeNumber, Dirty: DirtyPaint, Default: 1.5},
			Prop{Name: "subtitles", Type: TypeString, Dirty: DirtyPaint, Default: ""},
			Prop{Name: "audioTrack", Type: TypeNumber, Dirty: DirtyPaint, Default: 0.0},
			Prop{Name: "subtitleFontSize", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "subtitleColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
			Prop{Name: "subtitleFontWeight", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "subtitleShadow", Type: TypeShadow, Dirty: DirtyPaint, Default: "none"},
			Prop{Name: "subtitleBackground", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
			Prop{Name: "controlsIconColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
			Prop{Name: "controlsTextColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
			Prop{Name: "controlsTrackColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
		)},
		Element{Name: "Video", Attrs: withCommon(
			Prop{Name: "src", Type: TypeString, Dirty: DirtyLayout, Default: ""},
			Prop{Name: "controls", Type: TypeBool, Dirty: DirtyLayout, Default: true},
			Prop{Name: "autoplay", Type: TypeBool, Dirty: DirtyPaint, Default: false},
			Prop{Name: "loop", Type: TypeBool, Dirty: DirtyPaint, Default: false},
			Prop{Name: "muted", Type: TypeBool, Dirty: DirtyPaint, Default: false},
			Prop{Name: "volume", Type: TypeNumber, Dirty: DirtyPaint, Default: 1.0},
			Prop{Name: "startAt", Type: TypeNumber, Dirty: DirtyPaint, Default: 0.0},
			Prop{Name: "buffer", Type: TypeNumber, Dirty: DirtyPaint, Default: 1.5},
			Prop{Name: "subtitles", Type: TypeString, Dirty: DirtyPaint, Default: ""},
			Prop{Name: "audioTrack", Type: TypeNumber, Dirty: DirtyPaint, Default: 0.0},
			Prop{Name: "subtitleFontSize", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "subtitleColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
			Prop{Name: "subtitleFontWeight", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
			Prop{Name: "subtitleShadow", Type: TypeShadow, Dirty: DirtyPaint, Default: "none"},
			Prop{Name: "subtitleBackground", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
			Prop{Name: "controlsIconColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
			Prop{Name: "controlsTextColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
			Prop{Name: "controlsTrackColor", Type: TypeColor, Dirty: DirtyPaint, Default: uint32(0)},
			Prop{Name: "fit", Type: TypeEnum, Dirty: DirtyPaint, Enum: []string{"cover", "contain", "fill"}, Default: "contain"},
			Prop{Name: "fullscreen", Type: TypeBool, Dirty: DirtyLayout, Default: false},
		)},
	)

	// Every Material 3 component, with the properties componentdef declares.
	for _, k := range componentdef.Kinds {
		var attrs []Prop
		if componentdef.Flow(k.Name) {
			// Containers that lay their children out in a row or column accept
			// the same spacing and alignment as Row and Column.
			attrs = append(
				attrs,
				Prop{Name: "spacing", Type: TypeNumber, Dirty: DirtyLayout, Default: 0.0},
				Prop{Name: "crossAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "stretch"}, Default: "start"},
				Prop{Name: "mainAxisAlign", Type: TypeEnum, Dirty: DirtyLayout, Enum: []string{"start", "center", "end", "spaceBetween", "spaceAround", "spaceEvenly"}, Default: "start"},
			)
		}
		for _, name := range componentdef.Props(k.Name) {
			p := Prop{Name: name}
			def, hasDef := componentdef.Default(name)
			switch componentdef.TypeOfFor(k.Name, name) {
			case componentdef.Bool:
				p.Type, p.Dirty, p.Default = TypeBool, DirtyPaint, false
			case componentdef.Number:
				p.Type, p.Dirty, p.Default = TypeNumber, DirtyPaint, 0.0
				// A progress bar runs 0..1 by default; a chart reads a zero max
				// as "follow the data".
				if name == "max" && !componentdef.Chart(k.Name) {
					p.Default = 1.0
				}
			case componentdef.Enum:
				p.Type, p.Dirty, p.Enum = TypeEnum, DirtyLayout, componentdef.EnumValuesFor(k.Name, name)
			case componentdef.Scalar:
				p.Type, p.Dirty, p.Default = TypeScalar, DirtyLayout, ""
			default:
				p.Type, p.Dirty, p.Default = TypeString, DirtyLayout, ""
			}
			if hasDef {
				p.Default = def
			}
			attrs = append(attrs, p)
		}
		out = append(out, Element{Name: k.Name, Children: k.Container, Attrs: withCommon(attrs...)})
	}
	// Every element is a hit area; only components have the value-carrying
	// events. The list stays in one place so the validator, the builder and the
	// language server cannot drift apart.
	for i := range out {
		out[i].Events = append(out[i].Events, "onClick")
		if out[i].Name == "Audio" || out[i].Name == "Video" {
			out[i].Events = append(out[i].Events, "onPlay", "onPause", "onEnd")
			continue
		}
		if _, ok := componentdef.Lookup(out[i].Name); ok {
			for _, ev := range eventDefs {
				if ev.Component {
					out[i].Events = append(out[i].Events, ev.Name)
				}
			}
		}
	}
	return out
}()

var elementIndex = func() map[string]*Element {
	m := make(map[string]*Element, len(elements))
	for i := range elements {
		m[elements[i].Name] = &elements[i]
	}
	return m
}()

// Lookup returns the element definition by name.
func Lookup(name string) (*Element, bool) {
	e, ok := elementIndex[name]
	return e, ok
}

// ElementNames returns all element names, sorted, for suggestions.
func ElementNames() []string {
	names := make([]string, 0, len(elements))
	for _, e := range elements {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	return names
}

// RootNames returns the elements allowed as a file root.
func RootNames() []string { return []string{"Window"} }
