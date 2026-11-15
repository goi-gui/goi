//go:build !nonet

package ui

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/goi-gui/goi/internal/img"
)

// maxRemoteImageBytes caps one download. A design may point at anything, and an
// image that decodes to a 20k by 20k bitmap would take the process down.
const maxRemoteImageBytes = 32 << 20

// remoteImageClient is shared by every window: it pools connections, and the
// timeout keeps a hanging host from leaving a placeholder pulsing forever.
var remoteImageClient = &http.Client{Timeout: 20 * time.Second}

// fetchRemoteImage downloads and decodes one URL. Decoding happens here, off the
// UI goroutine: a large JPEG takes long enough to drop a frame.
func fetchRemoteImage(src string) (*img.Data, *img.Anim, error) {
	b, err := fetchRemote(src)
	if err != nil {
		return nil, nil, err
	}
	d, anim, err := img.DecodeAnimated(b)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", src, err)
	}
	if anim == nil {
		d = capDecoded(d)
	}
	return d, anim, nil
}

// fetchRemote downloads one URL and returns its bytes. Image decodes them here
// too; SVG keeps them until the UI goroutine can parse and rasterize.
func fetchRemote(src string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, src, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	req.Header.Set("Accept", "image/*")
	// A tile service can refuse an anonymous client: OpenStreetMap's policy
	// asks for an identifying agent, and it is what turns a 403 into tiles. A
	// browser drops this header and sends its own, which is what its policy
	// expects anyway.
	req.Header.Set("User-Agent", "goi (+https://github.com/goi-gui/goi)")
	resp, err := remoteImageClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", src, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	if len(b) > maxRemoteImageBytes {
		return nil, fmt.Errorf("%s: larger than %d bytes", src, maxRemoteImageBytes)
	}
	return b, nil
}
