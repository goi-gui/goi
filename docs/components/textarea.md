# TextArea

A multiline text field.

[All components](../components.md)

## In a `.goi` file

```goi
TextArea {
	id: notes
	label: "Notes"
	placeholder: "Enter starts a new line"
	text: "first line\nsecond line"
	width: 360
	height: 140
}
```

## Properties

| Property | Type | Notes |
|---|---|---|
| `label` | string | floats above the value once there is one |
| `text` | string | the value; `\n` separates lines |
| `placeholder` | string | shown while the value is empty |
| `supporting` | string | helper line under the field |
| `variant` | enum | `filled` (default) or `outlined` |
| `disabled` | bool | mutes the field and stops edits |

## Behaviour

Enter inserts a newline instead of submitting; `Submit` fires on Ctrl+Enter you
handle, or when an app reads the value. Home and End move to the start and end
of the current line, Up and Down walk the wrapped lines and keep the column.
Paste keeps newlines; a single-line TextField still strips them.

The field wraps at its own width, so a long line becomes several visual lines
and the caret scrolls vertically to stay visible.

## From Go

```go
area := ui.MustByID[*ui.TextArea](w, "notes")
area.OnEdit(func(s string) {
	fmt.Println("value:", s)
})
area.SetText("loaded from disk")
area.Caret = len(area.Text.Get())
```

Every element also accepts the [common properties](../components.md#common-properties).
