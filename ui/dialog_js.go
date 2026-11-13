//go:build js

package ui

import "errors"

// ErrDialogCanceled is what a dialog returns when the user dismisses it. The
// browser has no native file dialog to dismiss, but the name exists so an app
// that handles the desktop error still compiles.
var ErrDialogCanceled = errors.New("ui: the browser has no file dialogs")

var errNoBrowserDialog = errors.New("ui: file dialogs are not available in the browser; use an <input type=\"file\">")

// DialogsAvailable reports whether OpenFile and the rest can work here.
func DialogsAvailable() bool { return false }

// OpenFile has no browser equivalent: a page picks a file with an <input>.
func OpenFile(opts ...DialogOption) (string, error) { return "", errNoBrowserDialog }

// OpenFiles is OpenFile with several files.
func OpenFiles(opts ...DialogOption) ([]string, error) { return nil, errNoBrowserDialog }

// SaveFile needs a download, which a design can start itself.
func SaveFile(opts ...DialogOption) (string, error) { return "", errNoBrowserDialog }

// OpenFolder has no browser equivalent.
func OpenFolder(opts ...DialogOption) (string, error) { return "", errNoBrowserDialog }
