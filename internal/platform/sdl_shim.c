#include "sdl_shim.h"

#include <string.h>

// goiHitTestXY is implemented in Go; the trampoline gives SDL a C function
// pointer, which cgo cannot hand out directly.
extern int goiHitTestXY(int x, int y);

static SDL_HitTestResult goiHitTestTrampoline(SDL_Window *win, const SDL_Point *area, void *data) {
	(void)win;
	(void)data;
	return (SDL_HitTestResult)goiHitTestXY(area->x, area->y);
}

void goiInstallHitTest(SDL_Window *win) {
	SDL_SetWindowHitTest(win, goiHitTestTrampoline, NULL);
}

static int goiTranslate(SDL_Event *e, goiEvent *o) {
	memset(o, 0, sizeof *o);
	switch (e->type) {
	case SDL_EVENT_QUIT:
		o->type = GOI_EV_QUIT;
		return 1;
	case SDL_EVENT_WINDOW_CLOSE_REQUESTED:
		o->type = GOI_EV_CLOSE;
		return 1;
	case SDL_EVENT_WINDOW_PIXEL_SIZE_CHANGED:
		o->type = GOI_EV_RESIZE;
		o->width = e->window.data1;
		o->height = e->window.data2;
		return 1;
	case SDL_EVENT_WINDOW_EXPOSED:
		// The one event an interactive Wayland resize still delivers: SDL
		// throttles the resize events themselves while the grab is active.
		o->type = GOI_EV_EXPOSE;
		return 1;
	case SDL_EVENT_WINDOW_FOCUS_GAINED:
		o->type = GOI_EV_FOCUS_IN;
		return 1;
	case SDL_EVENT_WINDOW_FOCUS_LOST:
		o->type = GOI_EV_FOCUS_OUT;
		return 1;
	case SDL_EVENT_MOUSE_MOTION:
		o->type = GOI_EV_MOTION;
		o->x = e->motion.x;
		o->y = e->motion.y;
		return 1;
	case SDL_EVENT_MOUSE_BUTTON_DOWN:
	case SDL_EVENT_MOUSE_BUTTON_UP:
		o->type = GOI_EV_BUTTON;
		o->x = e->button.x;
		o->y = e->button.y;
		o->button = (int)e->button.button - 1; // SDL counts from 1
		o->down = e->button.down;
		return 1;
	case SDL_EVENT_MOUSE_WHEEL:
		o->type = GOI_EV_WHEEL;
		o->wheelX = e->wheel.x;
		o->wheelY = e->wheel.y;
		return 1;
	case SDL_EVENT_KEY_DOWN:
		o->type = GOI_EV_KEY;
		o->scancode = e->key.scancode;
		o->mod = e->key.mod;
		return 1;
	case SDL_EVENT_TEXT_INPUT:
		o->type = GOI_EV_TEXT;
		o->text = e->text.text;
		return 1;
	case SDL_EVENT_TEXT_EDITING:
		o->type = GOI_EV_EDITING;
		o->text = e->edit.text;
		o->caret = e->edit.start;
		return 1;
	case SDL_EVENT_USER:
		o->type = GOI_EV_USER;
		return 1;
	case SDL_EVENT_SYSTEM_THEME_CHANGED:
		o->type = GOI_EV_THEME;
		return 1;
	case SDL_EVENT_LOCALE_CHANGED:
		o->type = GOI_EV_LOCALE;
		return 1;
	}
	return 0;
}

int goiPollEvent(goiEvent *o) {
	SDL_Event e;
	while (SDL_PollEvent(&e)) {
		if (goiTranslate(&e, o)) {
			return 1;
		}
	}
	return 0;
}

int goiWaitEvent(goiEvent *o) {
	SDL_Event e;
	for (;;) {
		if (!SDL_WaitEvent(&e)) {
			return 0;
		}
		if (goiTranslate(&e, o)) {
			return 1;
		}
	}
}

int goiPushUserEvent(void) {
	SDL_Event e;
	memset(&e, 0, sizeof e);
	e.type = SDL_EVENT_USER;
	return SDL_PushEvent(&e) ? 1 : 0;
}

// Cursor shapes are cached in C so Go only names one of the three kinds.
static SDL_Cursor *goiCursors[3];

void goiSetCursorKind(int kind) {
	SDL_SystemCursor shape;
	if (kind < 0 || kind > 2) {
		return;
	}
	switch (kind) {
	case 1:  shape = SDL_SYSTEM_CURSOR_TEXT; break;
	case 2:  shape = SDL_SYSTEM_CURSOR_POINTER; break;
	default: shape = SDL_SYSTEM_CURSOR_DEFAULT; break;
	}
	if (!goiCursors[kind]) {
		goiCursors[kind] = SDL_CreateSystemCursor(shape);
	}
	if (goiCursors[kind]) {
		SDL_SetCursor(goiCursors[kind]);
	}
}

void goiFreeCursors(void) {
	for (int i = 0; i < 3; i++) {
		if (goiCursors[i]) {
			SDL_DestroyCursor(goiCursors[i]);
			goiCursors[i] = NULL;
		}
	}
}

void goiWindowSizePixels(SDL_Window *win, int *w, int *h) {
	if (!SDL_GetWindowSizeInPixels(win, w, h)) {
		*w = 0;
		*h = 0;
	}
}

float goiWindowScale(SDL_Window *win) {
	float s = SDL_GetWindowDisplayScale(win);
	return s > 0 ? s : 1;
}

void *goiWaylandDisplay(SDL_Window *win) {
	return SDL_GetPointerProperty(SDL_GetWindowProperties(win),
		SDL_PROP_WINDOW_WAYLAND_DISPLAY_POINTER, NULL);
}

void *goiWaylandSurface(SDL_Window *win) {
	return SDL_GetPointerProperty(SDL_GetWindowProperties(win),
		SDL_PROP_WINDOW_WAYLAND_SURFACE_POINTER, NULL);
}

void *goiX11Display(SDL_Window *win) {
	return SDL_GetPointerProperty(SDL_GetWindowProperties(win),
		SDL_PROP_WINDOW_X11_DISPLAY_POINTER, NULL);
}

unsigned long goiX11Window(SDL_Window *win) {
	return (unsigned long)SDL_GetNumberProperty(SDL_GetWindowProperties(win),
		SDL_PROP_WINDOW_X11_WINDOW_NUMBER, 0);
}

int goiSetTextInputArea(SDL_Window *win, int x, int y, int w, int h) {
	SDL_Rect r;
	r.x = x;
	r.y = y;
	r.w = w;
	r.h = h;
	return SDL_SetTextInputArea(win, &r, 0) ? 1 : 0;
}

char *goiClipboardText(void) {
	return SDL_GetClipboardText();
}

char *goiSystemLanguage(void) {
	int n = 0;
	SDL_Locale **locales = SDL_GetPreferredLocales(&n);
	if (locales == NULL || n == 0) {
		if (locales != NULL) {
			SDL_free(locales);
		}
		return SDL_strdup("");
	}
	// The first entry is the user's most preferred language. Only the language
	// subtag is reported, because "which language" is the question a design asks.
	const char *lang = locales[0]->language != NULL ? locales[0]->language : "";
	char *out = SDL_strdup(lang);
	SDL_free(locales);
	return out;
}

int goiSystemTheme(void) {
	switch (SDL_GetSystemTheme()) {
	case SDL_SYSTEM_THEME_LIGHT:
		return 1;
	case SDL_SYSTEM_THEME_DARK:
		return 2;
	default:
		return 0;
	}
}
