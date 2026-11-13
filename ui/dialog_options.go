// Package-level dialog options. The dialogs themselves are native (dialog.go)
// or absent (dialog_js.go); these names are the same on both.
package ui

import "context"

// FileFilter narrows a file dialog to the patterns it names:
//
//	ui.FileFilter{Name: "Goi designs", Patterns: []string{"*.goi"}}
type FileFilter struct {
	Name     string
	Patterns []string
}

// DialogOption adjusts one call to OpenFile, OpenFiles, SaveFile or OpenFolder.
type DialogOption func(*dialogOptions)

type dialogOptions struct {
	title    string
	dir      string
	filename string
	filters  []FileFilter
	ctx      context.Context
}

// DialogTitle sets the dialog's window title.
func DialogTitle(title string) DialogOption {
	return func(o *dialogOptions) { o.title = title }
}

// DialogDir opens the dialog in dir and, for a save dialog, writes there by
// default.
func DialogDir(dir string) DialogOption {
	return func(o *dialogOptions) { o.dir = dir }
}

// DialogFilename is the name a save dialog starts with, or the file an open
// dialog selects.
func DialogFilename(name string) DialogOption {
	return func(o *dialogOptions) { o.filename = name }
}

// DialogFilters limits the files the dialog shows.
func DialogFilters(filters ...FileFilter) DialogOption {
	return func(o *dialogOptions) { o.filters = filters }
}

// DialogContext dismisses the dialog when ctx is done.
func DialogContext(ctx context.Context) DialogOption {
	return func(o *dialogOptions) { o.ctx = ctx }
}

func newDialogOptions(opts []DialogOption) dialogOptions {
	var o dialogOptions
	for _, fn := range opts {
		fn(&o)
	}
	return o
}
