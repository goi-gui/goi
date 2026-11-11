package tree

import (
	"time"

	"github.com/goi-gui/goi/internal/img"
)

// Media is the playback state an Audio or Video element shares: the source, the
// switches a design sets, and the clock the app reads.
//
// The decoding lives in the ui layer, which owns the ffmpeg processes; a node
// only records what the design and Go asked for.
type Media struct {
	NodeBase

	Src Prop[string]
	// Controls draws the play/pause bar over the element. Off means the design
	// draws its own, which is what a custom player does.
	Controls Prop[bool]
	Autoplay Prop[bool]
	Loop     Prop[bool]
	Muted    Prop[bool]
	// Volume is 0..1.
	Volume Prop[float64]
	// StartAt is where playback begins, in seconds.
	StartAt Prop[float64]

	// Runtime state, written by the ui layer.
	//
	// Playing is what the player was asked for; Paused and Ended say why it is
	// not moving. Position and Duration are the clock the control bar and the
	// app read.
	Playing  bool
	Paused   bool
	Ended    bool
	Position time.Duration
	Duration time.Duration
	// Buffering is true while the decoder has not produced its first frame.
	Buffering bool
	LoadErr   error
	// RenderedSrc is the src the current player was opened for.
	RenderedSrc string

	// Playback, End and Seeked fire when the state changes.
	Playback Event[bool]
	End      Event[time.Duration]
	Seeked   Event[time.Duration]
}

// OnPlay subscribes to the player starting, with the position it starts from.
func (m *Media) OnPlay(fn func(time.Duration)) (off func()) {
	return m.Playback.On(func(on bool) {
		if on {
			fn(m.Position)
		}
	})
}

// OnPause subscribes to the player stopping at a position.
func (m *Media) OnPause(fn func(time.Duration)) (off func()) {
	return m.Playback.On(func(on bool) {
		if !on {
			fn(m.Position)
		}
	})
}

// OnEnd subscribes to the end of the file, with its duration.
func (m *Media) OnEnd(fn func(time.Duration)) (off func()) { return m.End.On(fn) }

// OnSeeked subscribes to a seek, with the new position.
func (m *Media) OnSeeked(fn func(time.Duration)) (off func()) { return m.Seeked.On(fn) }

// mediaProps is the property table a media element exposes: the common box plus
// the playback switches.
func mediaProps(m *Media) []NamedProp {
	return append(commonProps(&m.NodeBase),
		StringProp(&m.NodeBase, "src", &m.Src),
		BoolProp(&m.NodeBase, "controls", DirtyLayout, &m.Controls),
		BoolProp(&m.NodeBase, "autoplay", DirtyPaint, &m.Autoplay),
		BoolProp(&m.NodeBase, "loop", DirtyPaint, &m.Loop),
		BoolProp(&m.NodeBase, "muted", DirtyPaint, &m.Muted),
		NumberProp(&m.NodeBase, "volume", DirtyPaint, &m.Volume),
		NumberProp(&m.NodeBase, "startAt", DirtyPaint, &m.StartAt),
	)
}

// Audio plays a sound file. With `controls: true` it draws a compact bar with a
// play button and a scrubber; with the property off it keeps its box and stays
// silent on screen, which is what a design that draws its own transport wants.
type Audio struct{ Media }

// Kind implements Node.
func (*Audio) Kind() string { return "Audio" }

func (a *Audio) namedProps() []NamedProp { return mediaProps(&a.Media) }

// Video draws a decoded video frame with an optional control bar.
type Video struct {
	Media
	// Fit is how the frame fills the box: cover, contain or fill, like Image.
	Fit  Prop[string]
	Data *img.Data
	// FrameW and FrameH are the decoded frame's size, which is what the atlas
	// packs.
	FrameW, FrameH int
}

// Kind implements Node.
func (*Video) Kind() string { return "Video" }

func (v *Video) namedProps() []NamedProp {
	return append(mediaProps(&v.Media),
		EnumProp(&v.NodeBase, "fit", DirtyPaint, &v.Fit, "cover", "contain", "fill"),
	)
}

// ---------------------------------------------------------------- control

// MediaNode reports the shared playback state of an Audio or Video node.
func MediaNode(n Node) (*Media, bool) {
	switch x := n.(type) {
	case *Audio:
		return &x.Media, true
	case *Video:
		return &x.Media, true
	}
	return nil, false
}

// MediaMethods are the playback methods a design may call on a media element by
// id. `seek` and `volume` take one argument; the rest take none.
var MediaMethods = []string{"play", "pause", "stop", "toggle", "seek", "volume"}

// MediaMethod reports whether name is a playback method, and whether it takes an
// argument.
func MediaMethod(name string) (takesArg, ok bool) {
	for _, m := range MediaMethods {
		if m == name {
			return m == "seek" || m == "volume", true
		}
	}
	return false, false
}

// newMedia is the zero value of a media element: the switches a design may
// write, and the clock set to "nothing played yet".
func newMedia() Media {
	return Media{
		Volume:   newProp(nil, "volume", DirtyPaint, 1.0),
		Controls: newProp(nil, "controls", DirtyLayout, true),
		StartAt:  newProp(nil, "startAt", DirtyPaint, 0.0),
	}
}
