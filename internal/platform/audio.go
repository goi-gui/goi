package platform

/*
#include "sdl_shim.h"
*/
import "C"

import "unsafe"

// AudioStream is a playback device opened for pushed PCM. It is the only audio
// the framework knows: the media layer decodes, this plays.
//
// A machine without a sound card still returns a stream from OpenAudio that
// silently accepts and discards samples, so a caller never needs a nil check on
// the hot path.
type AudioStream struct {
	h     unsafe.Pointer
	Rate  int
	Chans int
}

// OpenAudio opens the default playback device at rate and channels, with signed
// 16-bit little-endian samples. It returns nil when the platforms has no audio
// subsystem at all, which callers treat as "silent".
func OpenAudio(rate, channels int) *AudioStream {
	if rate <= 0 {
		rate = 48000
	}
	if channels <= 0 {
		channels = 2
	}
	spec := C.goiAudioScreen{freq: C.int(rate), channels: C.int(channels)}
	h := C.goiOpenAudioStream(&spec)
	if h == nil {
		return nil
	}
	return &AudioStream{h: h, Rate: rate, Chans: channels}
}

// Queue copies PCM into the device's queue. The sample rate has to be the one
// the stream opened with; the media layer resamples with ffmpeg.
func (a *AudioStream) Queue(pcm []byte) error {
	if a == nil || a.h == nil || len(pcm) == 0 {
		return nil
	}
	if C.goiPutAudio(a.h, unsafe.Pointer(&pcm[0]), C.int(len(pcm))) != 0 {
		return sdlError("queue audio")
	}
	return nil
}

// Queued is how many bytes are waiting to be played, which is how a caller keeps
// its decode a fixed distance ahead of the device instead of buffering a whole
// file.
func (a *AudioStream) Queued() int {
	if a == nil || a.h == nil {
		return 0
	}
	return int(C.goiAudioQueued(a.h))
}

// Pause stops the device and drops what it had queued, which is what a seek
// needs. Resume continues from the next sample pushed.
func (a *AudioStream) Pause() {
	if a == nil || a.h == nil {
		return
	}
	C.goiPauseAudio(a.h, 1)
}

// Resume restarts playback.
func (a *AudioStream) Resume() {
	if a == nil || a.h == nil {
		return
	}
	C.goiPauseAudio(a.h, 0)
}

// Flush drops what the device has queued, so a decoder reopened at another
// position or another audio track does not play a moment of the old one first.
func (a *AudioStream) Flush() {
	if a == nil || a.h == nil {
		return
	}
	C.goiFlushAudio(a.h)
}

// Close releases the device.
func (a *AudioStream) Close() {
	if a == nil || a.h == nil {
		return
	}
	C.goiCloseAudioStream(a.h)
	a.h = nil
}

// HasAudio reports whether a playback device came up.
func HasAudio() bool { return C.goiAudioAvailable() == 1 }
