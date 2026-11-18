//go:build !js && !all && !dialog

package ui

import "errors"

// ErrDialogCanceled is what a dialog returns when the user dismisses it.
var ErrDialogCanceled = errors.New("ui: this build has no desktop dialogs; build with -tags dialog")

var errNoDialog = errors.New("ui: this build has no desktop dialogs; build with -tags dialog")

// dialogsReady is the internal gate the file picker uses.
func dialogsReady() error { return errNoDialog }

// DialogsAvailable reports whether OpenFile and the rest can work here.
func DialogsAvailable() bool { return false }

// OpenFile is absent in this build.
func OpenFile(opts ...DialogOption) (string, error) { return "", errNoDialog }

// OpenFiles is absent in this build.
func OpenFiles(opts ...DialogOption) ([]string, error) { return nil, errNoDialog }

// SaveFile is absent in this build.
func SaveFile(opts ...DialogOption) (string, error) { return "", errNoDialog }

// OpenFolder is absent in this build.
func OpenFolder(opts ...DialogOption) (string, error) { return "", errNoDialog }
