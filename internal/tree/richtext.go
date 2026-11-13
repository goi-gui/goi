package tree

import (
	"fmt"
	"strings"

	"github.com/goi-gui/goi/internal/text"
)

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
	// ColorRole names a theme role for the run, such as "primary" or
	// "tertiary". It wins over Color and follows the scheme, so a role written
	// on a Text child keeps changing colour with the theme.
	ColorRole string
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

// adopt turns the Text children a design declared into the runs they describe
// and detaches them. The children are only a way to author the runs: a span is
// data, not a node, because a run may break across lines.
func (r *RichText) adopt(t *Tree) error {
	if len(r.kids) == 0 {
		return nil
	}
	spans := make([]Span, 0, len(r.kids))
	for _, k := range r.kids {
		tx, ok := k.Node().(*Text)
		if !ok {
			return fmt.Errorf("RichText children must be Text, not %s", k.Node().Kind())
		}
		spans = append(spans, spanOf(tx))
	}
	for _, k := range append([]*NodeBase(nil), r.kids...) {
		t.Remove(k.node)
	}
	r.SetSpans(spans)
	return nil
}

// spanOf maps a Text child's style onto one run: a heavy weight is bold, an
// italic style slants, the decoration's strokes carry over, and a colour
// override does too. The size and family stay the RichText's own.
func spanOf(tx *Text) Span {
	s := Span{Text: tx.Text.Get(), Color: tx.Color.Get()}
	// A role keeps following the scheme after the child is gone; only a theme
	// role can be resolved by the painter, so a custom one falls back.
	if role := tx.colorRoleFor("color"); role != "" && !strings.HasPrefix(role, "custom:") {
		s.ColorRole, s.Color = strings.TrimPrefix(role, "theme:"), 0
	}
	switch w := tx.Weight.Get(); {
	case w >= 600, w == weightBolder:
		s.Bold = true
	}
	if tx.FontStyle.Get() == "italic" {
		s.Italic = true
	}
	for _, tok := range strings.Fields(tx.Decoration.Get()) {
		switch tok {
		case "underline":
			s.Underline = true
		case "line-through":
			s.Strike = true
		}
	}
	return s
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
