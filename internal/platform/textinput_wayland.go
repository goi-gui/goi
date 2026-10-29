//go:build linux && !x11

package platform

/*
#cgo pkg-config: wayland-client
#include <stdlib.h>
#include <wayland-client.h>
#include "ti3.h"

struct goui_ti_bind {
	struct zwp_text_input_manager_v3 *manager;
	struct wl_seat *seat;
};

void gouiTextInputBind(struct wl_registry *reg, struct goui_ti_bind *b);
void gouiTextInputAttach(struct zwp_text_input_v3 *ti);
*/
import "C"

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.4/glfw"
)

// textInput is a zwp_text_input_v3 client bound to GLFW's own Wayland display.
// It only relays IME events: plain typing still arrives through GLFW's char
// callback, so nothing breaks when no input method runs.
type textInput struct {
	display *C.struct_wl_display
	surface *C.struct_wl_surface

	ti *C.struct_zwp_text_input_v3

	// Pending state for the current batch. Text-input v3 is double buffered:
	// events accumulate until done, and only then are they applied.
	preedit    string
	caret      int
	commit     string
	before     int
	after      int
	hasPreedit bool

	active bool
	lastX  int
	lastY  int
	lastW  int
	lastH  int
	in     Input
}

// One window, one text input. The protocol callbacks carry no useful user data
// through the generated listener table, so the client registers itself here.
var currentTextInput *textInput

//export gouiTextInputEnterC
func gouiTextInputEnterC(data unsafe.Pointer, ti *C.struct_zwp_text_input_v3, surface *C.struct_wl_surface) {
	if t := currentTextInput; t != nil {
		t.surface = surface
	}
}

//export gouiTextInputLeaveC
func gouiTextInputLeaveC(data unsafe.Pointer, ti *C.struct_zwp_text_input_v3, surface *C.struct_wl_surface) {
	if t := currentTextInput; t != nil {
		t.surface = nil
	}
}

//export gouiTextInputPreeditC
func gouiTextInputPreeditC(data unsafe.Pointer, ti *C.struct_zwp_text_input_v3, text *C.char, begin, end C.int32_t) {
	t := currentTextInput
	if t == nil {
		return
	}
	t.preedit = C.GoString(text)
	t.caret = int(begin)
	t.hasPreedit = true
	_ = end
}

//export gouiTextInputCommitC
func gouiTextInputCommitC(data unsafe.Pointer, ti *C.struct_zwp_text_input_v3, text *C.char) {
	if t := currentTextInput; t != nil {
		t.commit = C.GoString(text)
	}
}

//export gouiTextInputDeleteC
func gouiTextInputDeleteC(data unsafe.Pointer, ti *C.struct_zwp_text_input_v3, before, after C.uint32_t) {
	t := currentTextInput
	if t == nil {
		return
	}
	t.before, t.after = int(before), int(after)
}

//export gouiTextInputDoneC
func gouiTextInputDoneC(data unsafe.Pointer, ti *C.struct_zwp_text_input_v3, serial C.uint32_t) {
	t := currentTextInput
	if t == nil || t.in == nil {
		return
	}
	// Order matters: an IME deletes context, inserts the commit, then keeps the
	// rest as a new preedit.
	if t.before > 0 || t.after > 0 {
		t.in.DeleteSurrounding(t.before, t.after)
		t.before, t.after = 0, 0
	}
	if t.commit != "" {
		t.in.CommitText(t.commit)
		t.commit = ""
	}
	if t.hasPreedit {
		t.in.Preedit(t.preedit, t.caret)
		t.preedit, t.caret, t.hasPreedit = "", 0, false
	}
}

// newTextInput binds text-input-v3 on the display GLFW already owns, so no
// second connection or event loop is needed. A compositor without the protocol
// leaves IME support off; the app keeps working.
func newTextInput(display, surface unsafe.Pointer, in Input) *textInput {
	if display == nil {
		return nil
	}
	t := &textInput{
		display: (*C.struct_wl_display)(display),
		surface: (*C.struct_wl_surface)(surface),
		in:      in,
	}
	reg := C.wl_display_get_registry(t.display)
	if reg == nil {
		return nil
	}
	bind := (*C.struct_goui_ti_bind)(C.calloc(1, C.sizeof_struct_goui_ti_bind))
	if bind == nil {
		C.wl_registry_destroy(reg)
		return nil
	}
	defer C.free(unsafe.Pointer(bind))

	C.gouiTextInputBind(reg, bind)
	C.wl_display_roundtrip(t.display)
	C.wl_registry_destroy(reg)
	if bind.manager == nil || bind.seat == nil {
		return nil
	}
	t.ti = C.zwp_text_input_manager_v3_get_text_input(bind.manager, bind.seat)
	if t.ti == nil {
		return nil
	}
	C.gouiTextInputAttach(t.ti)
	currentTextInput = t
	C.wl_display_flush(t.display)
	return t
}

// setFocus enables the IME for the focused field and tells it where the caret
// is, so the candidate window opens next to the text. An inactive request
// disables it again.
func (t *textInput) setFocus(active bool, x, y, w, h int) {
	if t == nil || t.ti == nil {
		return
	}
	if active {
		// markDirty reports every edit; only a changed caret or a fresh focus
		// needs a protocol round trip.
		if t.active && x == t.lastX && y == t.lastY && w == t.lastW && h == t.lastH {
			return
		}
		if !t.active {
			C.zwp_text_input_v3_enable(t.ti)
			t.active = true
		}
		t.lastX, t.lastY, t.lastW, t.lastH = x, y, w, h
		C.zwp_text_input_v3_set_cursor_rectangle(t.ti, C.int32_t(x), C.int32_t(y), C.int32_t(w), C.int32_t(h))
		C.zwp_text_input_v3_commit(t.ti)
	} else if t.active {
		C.zwp_text_input_v3_disable(t.ti)
		C.zwp_text_input_v3_commit(t.ti)
		t.active = false
	}
	C.wl_display_flush(t.display)
}

func (t *textInput) destroy() {
	if t == nil || t.ti == nil {
		return
	}
	C.zwp_text_input_v3_destroy(t.ti)
	C.wl_display_flush(t.display)
	t.ti = nil
	currentTextInput = nil
}

// InitTextInput attaches IME support to the window. It runs after SetInput,
// because the protocol needs somewhere to deliver the composed text.
func (p *Window) InitTextInput() {
	if p.ime != nil || p.input == nil {
		return
	}
	d := glfw.GetWaylandDisplay()
	s := p.win.GetWaylandWindow()
	if d == nil || s == nil {
		return
	}
	p.ime = newTextInput(unsafe.Pointer(d), unsafe.Pointer(s), p.input)
}
