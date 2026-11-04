package tree

import (
	"strings"
	"testing"
)

type bridgeRow struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

const bridgeUI = `bridge {
	rows: [row]
	onOpen: func(id: string)
	onToggle: func(on: bool)
	onVolume: func(v: number)
	onNote: func(text: string)
	onIndex: func(index: number)
}
type row { id: string, title: string }
Window {
	Column {
		{#each bridge.rows as row}
			Text { id: card text: row.title onClick: bridge.onOpen(row.id) }
		{/each}
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
