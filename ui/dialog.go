package ui

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/ncruces/zenity"
)

// ErrDialogCanceled is what a dialog returns when the user dismisses it. Test
// with errors.Is so a wrapped error still matches.
var ErrDialogCanceled = zenity.ErrCanceled

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

// location is the start path the dialog library expects: a directory, a file
// name, or both joined.
func (o dialogOptions) location() string {
	if o.dir == "" {
		return o.filename
	}
	return filepath.Join(o.dir, o.filename)
}

// options translates the wrapper's options for the dialog library.
func (o dialogOptions) options() []zenity.Option {
	var out []zenity.Option
	if o.title != "" {
		out = append(out, zenity.Title(o.title))
	}
	if o.dir != "" || o.filename != "" {
		out = append(out, zenity.Filename(o.location()))
	}
	if len(o.filters) > 0 {
		filters := make(zenity.FileFilters, len(o.filters))
		for i, f := range o.filters {
			filters[i] = zenity.FileFilter{Name: f.Name, Patterns: f.Patterns}
		}
		out = append(out, filters)
	}
	if o.ctx != nil {
		out = append(out, zenity.Context(o.ctx))
	}
	return out
}

// DialogsAvailable reports whether the platform has what the dialogs need. It
// is always true on Windows and macOS; on Linux it needs zenity, matedialog or
// qarma on PATH.
func DialogsAvailable() bool { return zenity.IsAvailable() }

var errNoDialogTool = errors.New("ui: no dialog tool on PATH; install zenity (or matedialog/qarma)")

func dialogsReady() error {
	if zenity.IsAvailable() {
		return nil
	}
	return errNoDialogTool
}

// OpenFile shows the platform's open-file dialog and returns the selected path.
// A dismissed dialog returns ErrDialogCanceled.
//
// The call blocks until the dialog closes, so from a UI handler the window does
// not repaint while it is open.
func OpenFile(opts ...DialogOption) (string, error) {
	if err := dialogsReady(); err != nil {
		return "", err
	}
	return zenity.SelectFile(newDialogOptions(opts).options()...)
}

// OpenFiles is OpenFile with multiple selection.
func OpenFiles(opts ...DialogOption) ([]string, error) {
	if err := dialogsReady(); err != nil {
		return nil, err
	}
	return zenity.SelectFileMultiple(newDialogOptions(opts).options()...)
}

// SaveFile shows the platform's save-file dialog and returns the chosen path.
// It asks before overwriting an existing file.
func SaveFile(opts ...DialogOption) (string, error) {
	if err := dialogsReady(); err != nil {
		return "", err
	}
	o := newDialogOptions(opts).options()
	return zenity.SelectFileSave(append(o, zenity.ConfirmOverwrite())...)
}

// OpenFolder shows the platform's folder dialog and returns the chosen directory.
func OpenFolder(opts ...DialogOption) (string, error) {
	if err := dialogsReady(); err != nil {
		return "", err
	}
	o := newDialogOptions(opts).options()
	return zenity.SelectFile(append(o, zenity.Directory())...)
}
