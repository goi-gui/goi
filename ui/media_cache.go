//go:build !js

package ui

import "github.com/goi-gui/goi/internal/media"

// mediaCache is the state the ffmpeg side of playback keeps between frames.
type mediaCache struct {
	// info caches the probe of each src, so a reload does not run ffmpeg again.
	info map[string]media.Info
	// probes holds the srcs with a background probe in flight, so two nodes with
	// the same src share one process.
	probes map[string]bool
	// files maps a src inside the design's container to the temp file ffmpeg
	// reads, so one copy serves every player and a reload.
	files map[string]string
}
