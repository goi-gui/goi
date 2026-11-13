//go:build js

package ui

import (
	"errors"
	"strings"
	"syscall/js"
)

// OpenLink opens a link in a new tab. A page has no filesystem behind it, so
// only a link with a scheme — a URL or a registered deep link — can be followed.
func OpenLink(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return errors.New("ui: OpenLink: empty target")
	}
	// A scheme is a colon before the first slash: `https://x`, `mailto:a@b`.
	// Anything else would be a path, which a page cannot open.
	head, _, _ := strings.Cut(target, "/")
	if !strings.Contains(head, ":") {
		return errors.New("ui: OpenLink: a page cannot open a path")
	}
	js.Global().Call("open", target, "_blank")
	return nil
}
