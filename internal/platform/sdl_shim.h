#ifndef GOI_SDL_SHIM_H
#define GOI_SDL_SHIM_H

#include <SDL3/SDL.h>

// goiEvent is a flat view of the SDL event fields the framework uses. SDL's
// SDL_Event is a union, so translating in C (see sdl_shim.c) keeps the layout in
// one place and Go never mirrors it.
typedef struct {
	int   type;       // GOI_EV_*
	int   width;      // GOI_EV_RESIZE
	int   height;
	float x, y;       // GOI_EV_MOTION, GOI_EV_BUTTON
	int   button;     // GOI_EV_BUTTON, already 0-based
	int   down;
	float wheelX;     // GOI_EV_WHEEL, whole steps, positive away from the user
	float wheelY;
	int   scancode;   // GOI_EV_KEY, SDL scancode
	int   mod;        // GOI_EV_KEY, SDL keymod
	const char *text; // GOI_EV_TEXT, GOI_EV_EDITING (valid for this call only)
	int   caret;      // GOI_EV_EDITING
} goiEvent;

enum {
	GOI_EV_NONE = 0,
	GOI_EV_QUIT,
	GOI_EV_CLOSE,
	GOI_EV_RESIZE,
	GOI_EV_EXPOSE,
	GOI_EV_FOCUS_IN,
	GOI_EV_FOCUS_OUT,
	GOI_EV_MOTION,
	GOI_EV_BUTTON,
	GOI_EV_WHEEL,
	GOI_EV_KEY,
	GOI_EV_TEXT,
	GOI_EV_EDITING,
	GOI_EV_USER
};

int goiPollEvent(goiEvent *o);
int goiWaitEvent(goiEvent *o);
int goiPushUserEvent(void);

void goiSetCursorKind(int kind);
void goiFreeCursors(void);

void goiWindowSizePixels(SDL_Window *win, int *w, int *h);
float goiWindowScale(SDL_Window *win);

void *goiWaylandDisplay(SDL_Window *win);
void *goiWaylandSurface(SDL_Window *win);
void *goiX11Display(SDL_Window *win);
unsigned long goiX11Window(SDL_Window *win);

int goiSetTextInputArea(SDL_Window *win, int x, int y, int w, int h);

// goiInstallHitTest routes presses through the Go hit-test callback.
void goiInstallHitTest(SDL_Window *win);

// goiClipboardText returns a copy the caller frees with SDL_free.
char *goiClipboardText(void);

#endif
