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
	GOI_EV_MOUSE_LEAVE,
	GOI_EV_MOTION,
	GOI_EV_BUTTON,
	GOI_EV_WHEEL,
	GOI_EV_KEY,
	GOI_EV_TEXT,
	GOI_EV_EDITING,
	GOI_EV_USER,
	GOI_EV_THEME,
	GOI_EV_LOCALE
};

int goiPollEvent(goiEvent *o);
int goiWaitEvent(goiEvent *o);
int goiWaitEventTimeout(goiEvent *o, int ms);
int goiPushUserEvent(void);

// Cursor kinds are platform.Cursor's order, which is SDL_SystemCursor's order
// plus two at the end: NONE hides the pointer, COUNT bounds the cached shapes.
enum {
	GOI_CURSOR_NONE = SDL_SYSTEM_CURSOR_COUNT,
	GOI_CURSOR_COUNT
};

void goiSetCursorKind(int kind);
void goiFreeCursors(void);

// goiPreferWaylandDriver asks SDL for the Wayland video driver.
void goiPreferWaylandDriver(void);

void goiWindowSizePixels(SDL_Window *win, int *w, int *h);
float goiWindowScale(SDL_Window *win);

void *goiWaylandDisplay(SDL_Window *win);
void *goiWaylandSurface(SDL_Window *win);
void *goiX11Display(SDL_Window *win);
unsigned long goiX11Window(SDL_Window *win);

// goiWin32Hwnd, goiWin32Instance and goiMetalLayer return the native handles
// the Windows and Apple renderers need; they return NULL on other platforms.
void *goiWin32Hwnd(SDL_Window *win);
void *goiWin32Instance(SDL_Window *win);
void *goiMetalLayer(SDL_Window *win);
// goiFreeMetalView releases the view a Metal window attached to itself.
void goiFreeMetalView(void);

// goiExtraWindowFlags is the platform bit a window must carry for its
// renderer: Metal on Apple, nothing elsewhere.
Uint64 goiExtraWindowFlags(void);

int goiSetTextInputArea(SDL_Window *win, int x, int y, int w, int h);

// goiInstallHitTest routes presses through the Go hit-test callback.
void goiInstallHitTest(SDL_Window *win);

// goiClipboardText returns a copy the caller frees with SDL_free.
char *goiClipboardText(void);

// goiSystemTheme reports the desktop's light/dark preference as 1 (light),
// 2 (dark), or 0 when the platform does not say.
int goiSystemTheme(void);

// goiSystemLanguage returns the desktop's preferred language as an ISO 639-1
// code ("tr", "en"), or an empty string when the platform does not say. The
// caller frees it with SDL_free.
char *goiSystemLanguage(void);

// goiAudioScreen is one SDL_AudioSpec, flattened so Go never mirrors SDL's
// struct. It is the format the audio stream is opened with.
typedef struct {
	int   freq;
	int   channels;
	int   format; // SDL_AudioFormat; 0 asks for SDL_AUDIO_S16
} goiAudioScreen;

// goiOpenAudioStream opens the default playback device and returns a stream the
// caller pushes PCM into. A NULL return means no device: the app runs silent.
void *goiOpenAudioStream(const goiAudioScreen *spec);

// goiPutAudio copies PCM into the stream. It returns 0 on success.
int goiPutAudio(void *stream, const void *data, int len);

// goiAudioQueued is how many bytes are waiting in the device: the caller uses it
// to stay a fixed distance ahead of playback and no further.
int goiAudioQueued(void *stream);

// goiAudioState pauses or resumes the device and releases the stream.
void goiPauseAudio(void *stream, int paused);
void goiCloseAudioStream(void *stream);

// goiAudioAvailable reports whether the audio subsystem came up. It is false on
// a machine with no sound card, and an app then plays silently.
int goiAudioAvailable(void);

#endif
