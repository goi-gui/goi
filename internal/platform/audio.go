//go:build !js

package platform

import (
	"sync"

	sdl "github.com/Zyko0/go-sdl3/sdl"
)

// AudioStream is the playback device, opened through SDL3.
type AudioStream struct {
	stream *sdl.AudioStream
	Rate   int
	Chans  int
}

var audioOnce sync.Once
var audioOK bool

func audioAvailable() bool {
	audioOnce.Do(func() {
		audioOK = sdl.InitSubSystem(sdl.INIT_AUDIO) == nil
	})
	return audioOK
}

func OpenAudio(rate, channels int) *AudioStream {
	if rate <= 0 {
		rate = 48000
	}
	if channels <= 0 {
		channels = 2
	}
	if !audioAvailable() {
		return nil
	}
	spec := &sdl.AudioSpec{Format: sdl.AUDIO_S16, Channels: int32(channels), Freq: int32(rate)}
	stream := sdl.AUDIO_DEVICE_DEFAULT_PLAYBACK.OpenAudioDeviceStream(spec, sdl.AudioStreamCallback(0))
	if stream == nil {
		return nil
	}
	_ = stream.ResumeDevice()
	return &AudioStream{stream: stream, Rate: rate, Chans: channels}
}

func (a *AudioStream) Queue(pcm []byte) error {
	if a == nil || a.stream == nil || len(pcm) == 0 {
		return nil
	}
	return a.stream.PutData(pcm)
}

func (a *AudioStream) Queued() int {
	if a == nil || a.stream == nil {
		return 0
	}
	n, err := a.stream.Queued()
	if err != nil {
		return 0
	}
	return int(n)
}

func (a *AudioStream) Pause() {
	if a != nil && a.stream != nil {
		_ = a.stream.PauseDevice()
	}
}

func (a *AudioStream) Resume() {
	if a != nil && a.stream != nil {
		_ = a.stream.ResumeDevice()
	}
}

func (a *AudioStream) Flush() {
	if a != nil && a.stream != nil {
		_ = a.stream.Flush()
	}
}

func (a *AudioStream) Close() {
	if a != nil && a.stream != nil {
		a.stream.Destroy()
		a.stream = nil
	}
}

func HasAudio() bool { return audioAvailable() }
