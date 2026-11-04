package main

import (
	"fmt"

	"github.com/ksckaan1/goi/ui"
)

func main() {
	app, err := ui.NewApp()
	if err != nil {
		panic(err)
	}

	win, err := app.LoadWindowFile("examples/search/search.goi", ui.WithHotReload(true))
	if err != nil {
		panic(err)
	}

	win.Bind(func(w *ui.Window) error {
		ui.SetHandler(w, "onSearch", func(term string) {
			fmt.Println("searching:", term)
		})
		return nil
	})

	err = app.Run()
	if err != nil {
		panic(err)
	}
}
