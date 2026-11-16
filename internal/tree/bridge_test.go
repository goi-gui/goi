package tree

import (
	"strings"
	"testing"
)

type bridgeRow struct {
	ID    string `goi:"id"`
	Title string `goi:"title"`
}

const bridgeUI = `bridge {
	rows []row
	onOpen func(id string)
	onToggle func(on bool)
	onVolume func(v float64)
	onNote func(text string)
	onIndex func(index float64)
}
type row { id string, title string }
Window {
	Column {
		for _, row := range bridge.rows {
			Text { id: card text: row.title onClick: bridge.onOpen(row.id) }
		}
		Checkbox { id: check label: "on" onToggle: bridge.onToggle(event) }
		Slider { id: volume onChange: bridge.onVolume(event) }
		TextField { id: field onEdit: bridge.onNote(event) onSubmit: bridge.onNote(event) }
		Tabs {
			id: tabs
			onSelect: bridge.onIndex(event)
			Tab { label: "One" selected: true }
			Tab { label: "Two" }
		}
	}
}`

// TestBridgeClickUsesTheRowAtFireTime covers the reason arguments stay syntax:
// `bridge.onOpen(row.id)` must read the row that exists when the click happens,
// not the row that existed when the design was built.
func TestBridgeClickUsesTheRowAtFireTime(t *testing.T) {
	var errs []error
	tr, diag := BuildSource([]byte(bridgeUI), &Hooks{Error: func(err error) { errs = append(errs, err) }})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	var opened []string
	if err := tr.SetHandler("onOpen", func(id string) { opened = append(opened, id) }); err != nil {
		t.Fatal(err)
	}

	var card Node
	tr.OnEach("rows", func(n Node, _ any) { card = n })
	tr.SetData("rows", []bridgeRow{{ID: "a", Title: "A"}})
	if card == nil {
		t.Fatal("no card row")
	}
	card.Base().Click.Emit(ClickEvent{})
	if len(opened) != 1 || opened[0] != "a" {
		t.Fatalf("opened = %v, want [a]", opened)
	}

	tr.SetData("rows", []bridgeRow{{ID: "b", Title: "B"}})
	card.Base().Click.Emit(ClickEvent{})
	if len(opened) != 2 || opened[1] != "b" {
		t.Fatalf("opened = %v, want the new row b", opened)
	}
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
}

// TestBridgeEventPayload passes the event's own value through `event`, and a
// missing handler is reported instead of panicking.
func TestBridgeEventPayload(t *testing.T) {
	var errs []error
	tr, diag := BuildSource([]byte(bridgeUI), &Hooks{Error: func(err error) { errs = append(errs, err) }})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	var toggled bool
	var volume float64
	var note string
	var index int
	if err := tr.SetHandler("onToggle", func(on bool) { toggled = on }); err != nil {
		t.Fatal(err)
	}
	if err := tr.SetHandler("onVolume", func(v float64) { volume = v }); err != nil {
		t.Fatal(err)
	}
	if err := tr.SetHandler("onNote", func(text string) { note = text }); err != nil {
		t.Fatal(err)
	}
	if err := tr.SetHandler("onIndex", func(i int) { index = i }); err != nil {
		t.Fatal(err)
	}
	tr.ids["check"].(*Checkbox).Toggle.Emit(true)
	tr.ids["volume"].(*Slider).Change.Emit(0.75)
	tr.ids["field"].(*TextField).Edit.Emit("hi")
	tr.ids["field"].(*TextField).Submit.Emit("done")
	tr.ids["tabs"].(*Tabs).Select.Emit(1)
	if !toggled || volume != 0.75 || note != "done" || index != 1 {
		t.Fatalf("toggled = %v, volume = %v, note = %q, index = %v", toggled, volume, note, index)
	}

	tr2, diag := BuildSource([]byte(bridgeUI), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	tr2.ids["check"].(*Checkbox).Toggle.Emit(true)
	if err := tr2.Err(); err == nil || !strings.Contains(err.Error(), "no handler registered") {
		t.Fatalf("err = %v", err)
	}
}

// TestSetObscureTextDrivesTheField: a reveal button a design can wire on its
// own, with the value it needs to show.
func TestSetObscureTextDrivesTheField(t *testing.T) {
	var errs []error
	tr, diag := BuildSource([]byte(`Window { Column {
		TextField { id: password label: "Password" obscureText: true }
		Button { id: reveal label: "Show" onClick: password.setObscureText(false) }
	} }`), &Hooks{Error: func(err error) { errs = append(errs, err) }})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	password := tr.ids["password"].(*TextField)
	reveal := tr.ids["reveal"].(*Button)
	if !password.Obscure.Get() {
		t.Fatal("obscureText: true was not applied")
	}
	reveal.Base().Click.Emit(ClickEvent{})
	if password.Obscure.Get() {
		t.Fatal("setObscureText(false) kept the value hidden")
	}
	if len(errs) != 0 {
		t.Fatalf("%v", errs)
	}
}

// TestExpandDrivesTheExpandedProp: `id.expand()` flips the state and
// `id.expand(true)` sets it, which is the value a design reads back as
// `id.expanded` and the icon switch follows.
func TestExpandDrivesTheExpandedProp(t *testing.T) {
	var errs []error
	tr, diag := BuildSource([]byte(`Window { Column {
		SplitButton { id: split label: "Split" }
		SplitButton { id: open label: "Open" onClick: open.expand(true) }
		SplitButton { id: close label: "Close" expanded: true onClick: close.expand(false) }
		SplitButton { id: flip label: "Flip" onClick: flip.expand() }
	} }`), &Hooks{Error: func(err error) { errs = append(errs, err) }})
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	split := tr.ids["split"].(*SplitButton)
	open := tr.ids["open"].(*SplitButton)
	closeBtn := tr.ids["close"].(*SplitButton)
	flip := tr.ids["flip"].(*SplitButton)

	open.Base().Click.Emit(ClickEvent{})
	if !open.Expanded.Get() {
		t.Fatal("expand(true) did not open")
	}
	closeBtn.Base().Click.Emit(ClickEvent{})
	if closeBtn.Expanded.Get() {
		t.Fatal("expand(false) did not close")
	}
	flip.Base().Click.Emit(ClickEvent{})
	if !flip.Expanded.Get() {
		t.Fatal("expand() did not toggle a closed button open")
	}
	flip.Base().Click.Emit(ClickEvent{})
	if flip.Expanded.Get() {
		t.Fatal("expand() did not toggle an open button closed")
	}
	if split.Expanded.Get() {
		t.Fatal("a button nobody touched reports expanded")
	}
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
}

// loopChannel is the Go shape the design's `type loopChannel` describes.
type loopChannel struct{ Name string }

// TestLoopIndexIsAValue: `for i, row := range bridge.rows` gives i Go's index,
// so a handler that takes an int gets the row's position, not the word "i".
func TestLoopIndexIsAValue(t *testing.T) {
	tr, diag := BuildSource([]byte(`bridge {
		channels []loopChannel
		onPick   func(index int)
	}
	type loopChannel {
		Name string
	}
	Window {
		Column {
			for i, channel := range bridge.channels {
				Text {
					text:    channel.Name
					onClick: bridge.onPick(i)
				}
			}
		}
	}`), nil)
	if !diag.Empty() {
		t.Fatalf("%v", diag)
	}
	var picked []int
	if err := tr.SetHandler("onPick", func(index int) { picked = append(picked, index) }); err != nil {
		t.Fatal(err)
	}
	var rows []Node
	tr.OnEach("channels", func(n Node, _ any) { rows = append(rows, n) })
	tr.SetData("channels", []loopChannel{{Name: "a"}, {Name: "b"}, {Name: "c"}})
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	for _, row := range rows {
		row.Base().Click.Emit(ClickEvent{})
	}
	if len(picked) != 3 || picked[0] != 0 || picked[1] != 1 || picked[2] != 2 {
		t.Fatalf("picked = %v, want 0, 1, 2", picked)
	}
}
