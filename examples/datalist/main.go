// Command datalist shows a list a design draws from data the app holds.
//
// The design declares the shape in a `data` block; Go fills it with ui.SetData.
// Run with: go run -ldflags=-linkmode=internal ./examples/datalist
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/ksckaan1/goi/ui"
)

type ticket struct {
	Title     string `json:"title"`
	Requester string `json:"requester"`
	Unread    bool   `json:"unread"`
}

// tickets lives outside Bind: Bind runs again after a hot reload, so the list
// the app holds is state, and Bind only re-applies it.
var tickets = []ticket{
	{Title: "VPN will not connect", Requester: "Ada", Unread: true},
	{Title: "Printer queue is stuck", Requester: "Grace", Unread: false},
	{Title: "Screen flickers on resume", Requester: "Alan", Unread: true},
}

func main() {
	app, err := ui.NewApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	win, err := app.LoadWindow(ui.DirFS("examples/datalist"), "main.goi",
		ui.WithHotReload(true), ui.WithSize(520, 560))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// fetch stands in for an API call. It loads for a moment, then sets the
	// list; the design rebuilds its rows by itself.
	fetch := func() {
		ui.SetData(win, "loading", true)
		go func() {
			time.Sleep(400 * time.Millisecond)
			// The tree belongs to the UI goroutine, so the result is posted.
			app.Post(func() {
				ui.SetData(win, "loading", false)
				ui.SetData(win, "tickets", tickets)
				ui.SetData(win, "count", fmt.Sprintf("%d tickets", len(tickets)))
			})
		}()
	}

	err = win.Bind(func(w *ui.Window) error {
		if _, err := ui.ByID[*ui.Button](w, "reload"); err == nil {
			ui.MustByID[*ui.Button](w, "reload").OnClick(func(ui.ClickEvent) { fetch() })
		}
		// Every rebuilt row is handed to this hook, so the click reads the item
		// the row was built from.
		ui.OnEach[*ui.ListItem](w, "tickets", func(row *ui.ListItem, it ticket) {
			row.OnClick(func(ui.ClickEvent) {
				fmt.Println("open:", it.Title)
			})
		})
		// Bind runs again on every reload; re-applying is cheap and idempotent.
		ui.SetData(w, "loading", false)
		ui.SetData(w, "tickets", tickets)
		ui.SetData(w, "count", fmt.Sprintf("%d tickets", len(tickets)))
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fetch()
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
