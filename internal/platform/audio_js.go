//go:build js

package platform

// AudioStream is the browser's playback device. A browser has no ffmpeg, so
// nothing decodes into it yet; the type keeps the media layer compiling and
// silent instead of absent.
type AudioStream struct {
	Rate  int
	Chans int
}

// OpenAudio returns nil: without a decoder there is nothing to play, and a nil
// stream already means "silent" on every other platform.
func OpenAudio(rate, channels int) *AudioStream { return nil }

// Queue accepts and discards samples, so a caller needs no nil check.
func (a *AudioStream) Queue(pcm []byte) error { return nil }

// Queued is always 0, which a caller reads as "the device needs more".
func (a *AudioStream) Queued() int { return 0 }

func (a *AudioStream) Pause()  {}
func (a *AudioStream) Resume() {}
func (a *AudioStream) Flush()  {}
func (a *AudioStream) Close()  {}

// HasAudio reports whether a decoder and a device are both available.
func HasAudio() bool { return false }
