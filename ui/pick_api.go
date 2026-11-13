package ui

// PickFiles opens the platform's file picker and hands fn the chosen files as
// srcs: `goi://picked/N/name`, which every reader understands, so an Image, a
// Video or a preview can take one directly.
//
// It does not block. A browser can only show a picker from a live user gesture,
// so waiting for the dialog would stall the frame that opened it; on the desktop
// the same shape keeps one API for both.
//
//	app.PickFiles(func(files []string) {
//		if len(files) > 0 {
//			preview.Src.Set(files[0])
//		}
//	})
func (a *App) PickFiles(fn func([]string), opts ...DialogOption) {
	if a == nil || fn == nil {
		return
	}
	a.chooseFiles(fn, opts)
}
