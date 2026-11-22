//go:build js

package ui

import (
	"encoding/json"
	"syscall/js"
)

// hmrMessage is what goi serve sends over the event stream. The server's copy
// is in cmd/goi/serve.go; keep the two shapes in step.
type hmrMessage struct {
	Type    string            `json:"type"`
	Files   map[string]string `json:"files"`
	Message string            `json:"message"`
}

// hmrFuncs keeps the JS callbacks reachable: syscall/js releases a Func the Go
// side drops, while the EventSource still holds the listener.
var hmrFuncs []js.Func

// startWebReload follows the development server when the served page carries its
// marker. A normal static page has no marker, so a production build never opens
// a connection.
func (a *App) startWebReload() {
	if a == nil || !a.opts.HotReload {
		return
	}
	global := js.Global()
	url := global.Get("__GOI_HMR__")
	if url.Type() != js.TypeString || url.String() == "" {
		return
	}
	ctor := global.Get("EventSource")
	if ctor.Type() != js.TypeFunction {
		return
	}
	// The served page and goi serve look for this: a build from before the
	// feature never sets it, which is why the page can say what is missing.
	global.Set("__GOI_HMR_READY__", true)
	stream := ctor.New(url.String())
	onMessage := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		data := args[0].Get("data")
		if data.Type() != js.TypeString {
			return nil
		}
		var msg hmrMessage
		if err := json.Unmarshal([]byte(data.String()), &msg); err != nil {
			return nil
		}
		switch msg.Type {
		case "assets":
			files := make(map[string][]byte, len(msg.Files))
			for name, body := range msg.Files {
				files[name] = []byte(body)
			}
			a.Post(func() { a.applyLiveAssets(files) })
		case "reload":
			global.Get("location").Call("reload")
		case "error":
			global.Get("console").Call("error", "goi: "+msg.Message)
		}
		return nil
	})
	stream.Call("addEventListener", "message", onMessage)
	hmrFuncs = append(hmrFuncs, onMessage)
}
