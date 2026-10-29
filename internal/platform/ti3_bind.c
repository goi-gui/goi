//go:build linux && !x11

// Binding helpers for zwp_text_input_v3. The protocol itself comes from the
// generated ti3.c/ti3.h; this file only adds what the Go side needs and cannot
// express: a registry listener and the event listener table.

#include <string.h>
#include <wayland-client.h>
#include "ti3.h"

// Defined in the patched ti3.c: fills the core-interface slots at runtime.
void gouiTextInputResolveInterfaces(void);

// Implemented in Go (textinput_wayland.go). Signatures must stay in sync.
extern void gouiTextInputEnterC(void *data, struct zwp_text_input_v3 *ti, struct wl_surface *surface);
extern void gouiTextInputLeaveC(void *data, struct zwp_text_input_v3 *ti, struct wl_surface *surface);
extern void gouiTextInputPreeditC(void *data, struct zwp_text_input_v3 *ti, const char *text, int32_t begin, int32_t end);
extern void gouiTextInputCommitC(void *data, struct zwp_text_input_v3 *ti, const char *text);
extern void gouiTextInputDeleteC(void *data, struct zwp_text_input_v3 *ti, uint32_t before, uint32_t after);
extern void gouiTextInputDoneC(void *data, struct zwp_text_input_v3 *ti, uint32_t serial);

static const struct zwp_text_input_v3_listener goui_ti_listener = {
	.enter = gouiTextInputEnterC,
	.leave = gouiTextInputLeaveC,
	.preedit_string = gouiTextInputPreeditC,
	.commit_string = gouiTextInputCommitC,
	.delete_surrounding_text = gouiTextInputDeleteC,
	.done = gouiTextInputDoneC,
};

struct goui_ti_bind {
	struct zwp_text_input_manager_v3 *manager;
	struct wl_seat *seat;
};

static void goui_registry_global(void *data, struct wl_registry *reg, uint32_t name,
                                 const char *iface, uint32_t version) {
	struct goui_ti_bind *b = data;
	if (strcmp(iface, zwp_text_input_manager_v3_interface.name) == 0) {
		b->manager = wl_registry_bind(reg, name, &zwp_text_input_manager_v3_interface,
		                              version < 1 ? version : 1);
	} else if (strcmp(iface, "wl_seat") == 0) {
		b->seat = wl_registry_bind(reg, name, &wl_seat_interface, 1);
	}
}

static void goui_registry_remove(void *data, struct wl_registry *reg, uint32_t name) {}

static const struct wl_registry_listener goui_registry_listener = {
	.global = goui_registry_global,
	.global_remove = goui_registry_remove,
};

// gouiTextInputBind scans a registry for text-input-v3 and a seat. Call it
// before a display roundtrip; the results are read from the struct after it.
void gouiTextInputBind(struct wl_registry *reg, struct goui_ti_bind *b) {
	gouiTextInputResolveInterfaces();
	wl_registry_add_listener(reg, &goui_registry_listener, b);
}

// gouiTextInputAttach installs the event listener. All state lives on the Go
// side, so the listener data pointer is unused.
void gouiTextInputAttach(struct zwp_text_input_v3 *ti) {
	zwp_text_input_v3_add_listener(ti, &goui_ti_listener, NULL);
}
