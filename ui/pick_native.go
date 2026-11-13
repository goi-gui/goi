//go:build !js

package ui

import (
	"fmt"
	"os"
)

// chooseFiles runs the native file dialog off the UI goroutine and reports the
// result back on it, which is the same shape the browser needs.
func (a *App) chooseFiles(fn func([]string), opts []DialogOption) {
	go func() {
		if err := dialogsReady(); err != nil {
			a.reportError(err)
			return
		}
		paths, err := OpenFiles(opts...)
		if err != nil {
			if err != ErrDialogCanceled {
				a.reportError(err)
			}
			return
		}
		files := make([]string, 0, len(paths))
		for i, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				a.reportError(fmt.Errorf("ui: read %s: %w", p, err))
				continue
			}
			files = append(files, a.putPicked(i, p, b))
		}
		a.Post(func() { fn(files) })
	}()
}
