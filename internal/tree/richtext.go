package tree

import "github.com/goi-gui/goi/internal/text"

// Span is one styled run inside a RichText. A span is data, not a node: a run
// can break across lines, which one node's bounds cannot describe.
type Span struct {
	Text string
	// Bold draws one step heavier than the role, Italic slants the run.
	Bold, Italic bool
	// Underline and Strike draw a line under or through the run.
	Underline, Strike bool
	// Code draws the run as inline code: a tinted background, no markup.
	Code bool
	// Link is the destination a link span points at. A link draws in the
	// theme's primary colour and is underlined. goi has no URL opener of its
	// own, so a design handles taps and calls the platform itself.
	Link string
	// Color overrides the run's colour; 0 keeps the RichText's own.
	Color uint32
}

// RichText is one paragraph whose runs may differ: the inline model behind
// Markdown's paragraphs, and available to a design on its own. It embeds Text
// so the role, size, colour, alignment, wrapping and the common box properties
// mean exactly what they mean for a Text.
//
// Spans are set from Go. A RichText with none draws its `text` as one plain
// run, so the element is still usable straight from a `.goi` file.
type RichText struct {
	Text
	spans   []Span
	version int
}

// Kind implements Node.
func (*RichText) Kind() string { return "RichText" }

// Spans returns the styled runs to draw. A RichText with no spans falls back to
// its text as one plain run.
func (r *RichText) Spans() []Span {
	if len(r.spans) > 0 {
		return r.spans
	}
	if s := r.Text.Text.Get(); s != "" {
		return []Span{{Text: s}}
	}
	return nil
}

// SetSpans replaces the styled runs and asks for a layout: the wraps depend on
// the runs' widths.
func (r *RichText) SetSpans(spans []Span) {
	r.spans = spans
	r.version++
	r.MarkDirty(DirtyLayout)
}

// Version counts span writes, so a paint cache can tell a rewritten paragraph
// from an unchanged one.
func (r *RichText) Version() int { return r.version }

// SpanWeight resolves a run's weight against the role's: a bold run is one step
// heavier. Layout and paint both use it, so the height reserved for a wrapped
// line matches the face it is drawn in.
func (r *RichText) SpanWeight(s Span, role int) int {
	if s.Bold {
		return text.StepWeight(role, true)
	}
	return role
}

// Inlines returns the spans as the text package's run list, each run's index
// standing in as its style, which is what a measure callback resolves.
func (r *RichText) Inlines() []text.Inline {
	spans := r.Spans()
	out := make([]text.Inline, len(spans))
	for i, s := range spans {
		out[i] = text.Inline{Text: s.Text, Style: i}
	}
	return out
}

func (r *RichText) namedProps() []NamedProp {
	t := &r.Text
	return append(commonProps(&r.NodeBase),
		StringProp(&r.NodeBase, "text", &t.Text),
		NumberProp(&r.NodeBase, "fontSize", DirtyLayout, &t.FontSize),
		ColorProp(&r.NodeBase, "color", DirtyPaint, &t.Color),
		EnumProp(&r.NodeBase, "align", DirtyLayout, &t.Align, "start", "center", "end"),
		BoolProp(&r.NodeBase, "wrap", DirtyLayout, &t.Wrap),
		EnumProp(&r.NodeBase, "role", DirtyLayout, &t.Role, textRoles...),
	)
}
