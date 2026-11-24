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
		// OnError may touch nodes, so it has to run on the UI goroutine.
		fail := func(err error) { a.Post(func() { a.reportError(err) }) }
		if err := dialogsReady(); err != nil {
			fail(err)
			return
		}
		paths, err := OpenFiles(opts...)
		if err != nil {
			if err != ErrDialogCanceled {
				fail(err)
			}
			return
		}
		files := make([]string, 0, len(paths))
		for i, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				fail(fmt.Errorf("ui: read %s: %w", p, err))
				continue
			}
			files = append(files, a.putPicked(i, p, b))
		}
		a.Post(func() { fn(files) })
	}()
}
