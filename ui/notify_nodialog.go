//go:build !all && !dialog

package ui

import "errors"

// Notify is absent in a build without the desktop notification library.
func Notify(title, body string) error {
	return errors.New("ui: this build has no desktop notifications; build with -tags dialog")
}
