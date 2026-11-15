//go:build nomarkdown

package tree

// Markdown renders a document as child elements. A `nomarkdown` build drops
// goldmark: the element still exists, but it draws nothing and adds no children.
type Markdown struct {
	Component
	// rendered is the source the children were built from.
	rendered string
}

// Kind implements Node.
func (*Markdown) Kind() string { return "Markdown" }

func (c *Markdown) namedProps() []NamedProp { return c.props("Markdown") }

// Source is the markdown the element draws.
func (c *Markdown) Source() string { return c.Text.Get() }

// SetSource replaces the markdown.
func (c *Markdown) SetSource(s string) { c.Text.Set(s) }

// Rebuild does nothing in this build.
func (c *Markdown) Rebuild() bool { return false }
