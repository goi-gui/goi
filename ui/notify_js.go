//go:build js

package ui

import "errors"

// Notify has no browser implementation; a page uses its own notifications.
func Notify(title, body string) error {
	return errors.New("ui: a browser has no desktop notifications")
}
