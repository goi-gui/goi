package tree

import "github.com/goi-gui/goi/internal/img"

// Mermaid draws Mermaid source as a diagram. The source is Text; the ui layer
// renders it to a bitmap when the text or the theme changes, and the intrinsic
// size is what layout asks for when the design gives no width.
type Mermaid struct {
	Component
	// Src is a .mmd file in the design's container; Text wins when it is not
	// empty.
	Src                    Prop[string]
	Data                   *img.Data
	IntrinsicW, IntrinsicH float64
	// RenderedText/RenderedTheme are what Data was rendered from, so a frame
	// can tell a new diagram or a new scheme from the one already on the GPU.
	RenderedText  string
	RenderedTheme string
	// LoadErr is a parse or render failure. The node keeps its background, so a
	// bad source is visible rather than a silent hole.
	LoadErr error
}

// Kind implements Node.
func (*Mermaid) Kind() string { return "Mermaid" }
func (c *Mermaid) namedProps() []NamedProp {
	return append(c.propsExcept("Mermaid", "src"),
		StringProp(&c.NodeBase, "src", &c.Src))
}

// SetSource replaces the diagram. The next frame renders it.
func (c *Mermaid) SetSource(s string) { c.Text.Set(s) }
