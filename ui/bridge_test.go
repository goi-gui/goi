package ui

import (
	"strings"
	"testing"
)

type bridgeRow struct {
	ID string `json:"id"`
}

const bridgeUI = `bridge {
	rows: [row]
	onOpen: func(id: string)
}
type row { id: string }
Window {
	Column {
		{#each bridge.rows as row}
			Button { id: open label: "open" onClick: bridge.onOpen(row.id) }
		{/each}
	}
}`

// TestSetHandlerRunsOnClick is the whole bridge round trip: the design declares
// the handler and the call, Go supplies the body.
func TestSetHandlerRunsOnClick(t *testing.T) {
	_, w := reloadApp(t, bridgeUI)
	var btn *Button
	OnEach(w, "rows", func(b *Button, _ bridgeRow) { btn = b })
	SetData(w, "rows", []bridgeRow{{ID: "42"}})
	var got string
	if err := SetHandler(w, "onOpen", func(id string) { got = id }); err != nil {
		t.Fatal(err)
	}
	btn.Click.Emit(ClickEvent{})
	if got != "42" {
		t.Fatalf("handler got %q, want 42", got)
	}
}

func TestSetHandlerMissingIsReported(t *testing.T) {
	_, w := reloadApp(t, bridgeUI)
	var btn *Button
	OnEach(w, "rows", func(b *Button, _ bridgeRow) { btn = b })
	SetData(w, "rows", []bridgeRow{{ID: "42"}})
	btn.Click.Emit(ClickEvent{})
	if err := w.tree.Err(); err == nil || !strings.Contains(err.Error(), "no handler registered") {
		t.Fatalf("err = %v", err)
	}
}
