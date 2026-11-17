package tree

import "github.com/goi-gui/goi/internal/img"

// Mermaid draws Mermaid source as a diagram. Source is the `data` property; the
// ui layer renders it to a bitmap when the source or the theme changes, and the
// intrinsic size is what layout asks for when the design gives no width.
type Mermaid struct {
	Component
	Source                 Prop[string]
	Data                   *img.Data
	IntrinsicW, IntrinsicH float64
	// RenderedKey and PendingKey are the source and palette a bitmap was made
	// from and the one being rendered, so a frame can tell a stale result from
	// the current design.
	RenderedKey string
	PendingKey  string
	Loading     bool
	// LoadErr is a parse or render failure. The node keeps its box, so a bad
	// source is visible rather than a silent hole.
	LoadErr error
}

// Kind implements Node.
func (*Mermaid) Kind() string { return "Mermaid" }

func (c *Mermaid) namedProps() []NamedProp {
	return append(c.propsExcept("Mermaid", "data"),
		StringProp(&c.NodeBase, "data", &c.Source),
	)
}

// SetSource replaces the diagram. The next visible frame renders it.
func (c *Mermaid) SetSource(s string) { c.Source.Set(s) }
