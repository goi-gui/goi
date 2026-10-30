//go:build linux && !x11

// Binding helpers for zwp_text_input_v3. The protocol itself comes from the
// generated ti3.c/ti3.h; this file only adds what the Go side needs and cannot
// express: a registry listener and the event listener table.

#include <string.h>
#include <wayland-client.h>
#include "ti3.h"

// Defined in the patched ti3.c: fills the core-interface slots at runtime.
void goiTextInputResolveInterfaces(void);

// Implemented in Go (textinput_wayland.go). Signatures must stay in sync.
extern void goiTextInputEnterC(void *data, struct zwp_text_input_v3 *ti, struct wl_surface *surface);
extern void goiTextInputLeaveC(void *data, struct zwp_text_input_v3 *ti, struct wl_surface *surface);
extern void goiTextInputPreeditC(void *data, struct zwp_text_input_v3 *ti, const char *text, int32_t begin, int32_t end);
extern void goiTextInputCommitC(void *data, struct zwp_text_input_v3 *ti, const char *text);
extern void goiTextInputDeleteC(void *data, struct zwp_text_input_v3 *ti, uint32_t before, uint32_t after);
extern void goiTextInputDoneC(void *data, struct zwp_text_input_v3 *ti, uint32_t serial);

static const struct zwp_text_input_v3_listener goi_ti_listener = {
	.enter = goiTextInputEnterC,
	.leave = goiTextInputLeaveC,
	.preedit_string = goiTextInputPreeditC,
	.commit_string = goiTextInputCommitC,
	.delete_surrounding_text = goiTextInputDeleteC,
	.done = goiTextInputDoneC,
};

struct goi_ti_bind {
	struct zwp_text_input_manager_v3 *manager;
	struct wl_seat *seat;
};

static void goi_registry_global(void *data, struct wl_registry *reg, uint32_t name,
                                 const char *iface, uint32_t version) {
	struct goi_ti_bind *b = data;
	if (strcmp(iface, zwp_text_input_manager_v3_interface.name) == 0) {
		b->manager = wl_registry_bind(reg, name, &zwp_text_input_manager_v3_interface,
		                              version < 1 ? version : 1);
	} else if (strcmp(iface, "wl_seat") == 0) {
		b->seat = wl_registry_bind(reg, name, &wl_seat_interface, 1);
	}
}

static void goi_registry_remove(void *data, struct wl_registry *reg, uint32_t name) {}

static const struct wl_registry_listener goi_registry_listener = {
	.global = goi_registry_global,
	.global_remove = goi_registry_remove,
};

// goiTextInputBind scans a registry for text-input-v3 and a seat. Call it
// before a display roundtrip; the results are read from the struct after it.
void goiTextInputBind(struct wl_registry *reg, struct goi_ti_bind *b) {
	goiTextInputResolveInterfaces();
	wl_registry_add_listener(reg, &goi_registry_listener, b);
}

// goiTextInputAttach installs the event listener. All state lives on the Go
// side, so the listener data pointer is unused.
void goiTextInputAttach(struct zwp_text_input_v3 *ti) {
	zwp_text_input_v3_add_listener(ti, &goi_ti_listener, NULL);
}
