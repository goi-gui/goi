// Package ui is the public API of the goui declarative GUI framework.
//
// A .ui file describes the window; Go code finds elements by id and attaches
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
	"github.com/ksckaan1/goui/internal/tree"
)

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
	Column     = tree.Column
	Row        = tree.Row
	Stack      = tree.Stack
	Rectangle  = tree.Rectangle
	Text       = tree.Text
	Button     = tree.Button
	TextInput  = tree.TextInput
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
	KeyEscape    = tree.KeyEscape
	KeyUp        = tree.KeyUp
	KeyDown      = tree.KeyDown
)
