//go:build !js

package ui

import (
	"testing"
	"time"
)

// TestProbeDoesNotBlockTheWindow is the startup guarantee: building a window
// with a player only starts the probe, so the first frame appears while ffmpeg
// is still reading the file.
func TestProbeDoesNotBlockTheWindow(t *testing.T) {
	clip := mediaClip(t)
	a, win := reloadApp(t, `Window { Video { id: v width: 320 height: 180 } }`)
	v := MustByID[*Video](win, "v")
	v.Src.Set(clip)
	a.probeMedia(v, &v.Media)

	// Nothing has drained the posted result yet, so the node is still loading
	// even though ffmpeg may already be done.
	if !v.Loading {
		t.Fatal("the window waited for the probe instead of deferring it")
	}
	if !a.pulsing {
		t.Fatal("the loading placeholder was not started")
	}
	// The placeholder breathes, which is what the loop paints.
	if !a.tickImageLoads(0.2) {
		t.Fatal("the loading media did not ask for a frame")
	}
	if v.Phase <= 0 {
		t.Fatalf("the placeholder phase stayed at %v", v.Phase)
	}

	deadline := time.Now().Add(15 * time.Second)
	for v.Loading && time.Now().Before(deadline) {
		a.drainPosted()
		time.Sleep(5 * time.Millisecond)
	}
	if v.Loading {
		t.Fatal("the probe never finished")
	}
	if v.Duration <= 0 {
		t.Fatalf("the probe result was not applied: %+v", v.Media)
	}
	if v.LoadErr != nil {
		t.Fatalf("probe error: %v", v.LoadErr)
	}
}

// TestPlayWhileLoadingWaitsForTheProbe covers autoplay of a remote stream: the
// design asked to play before the probe landed, and the decoder starts on its
// own once it does.
func TestPlayWhileLoadingWaitsForTheProbe(t *testing.T) {
	clip := mediaClip(t)
	a, win := reloadApp(t, `Window { Video { id: v width: 320 height: 180 autoplay: true } }`)
	v := MustByID[*Video](win, "v")
	v.Src.Set(clip)
	defer a.closeMedia()
	a.probeMedia(v, &v.Media)
	if !v.Loading {
		t.Fatal("the probe did not start")
	}

	a.playMedia(v)
	if !v.Playing {
		t.Fatal("the design's play was dropped while loading")
	}
	if a.playerFor(v) != nil {
		t.Fatal("a decoder started before the file was probed")
	}

	deadline := time.Now().Add(15 * time.Second)
	for (v.Loading || a.playerFor(v) == nil) && time.Now().Before(deadline) {
		a.drainPosted()
		time.Sleep(5 * time.Millisecond)
	}
	if v.Loading {
		t.Fatal("the probe never finished")
	}
	if a.playerFor(v) == nil {
		t.Fatalf("the deferred play never started (err %v)", v.LoadErr)
	}
	if !v.Playing || v.Paused {
		t.Fatalf("state = playing %v paused %v", v.Playing, v.Paused)
	}
}

// TestProbeFailureIsReportedNotFatal checks that a file that cannot be probed
// leaves the window alive and says why.
func TestProbeFailureIsReportedNotFatal(t *testing.T) {
	a, win := reloadApp(t, `Window { Video { id: v width: 320 height: 180 } }`)
	v := MustByID[*Video](win, "v")
	v.Src.Set(t.TempDir() + "/missing.mp4")
	a.probeMedia(v, &v.Media)
	deadline := time.Now().Add(15 * time.Second)
	for v.Loading && time.Now().Before(deadline) {
		a.drainPosted()
		time.Sleep(5 * time.Millisecond)
	}
	if v.Loading {
		t.Fatal("a missing file left the player loading forever")
	}
	if v.LoadErr == nil {
		t.Fatal("a missing file did not report an error")
	}
}
