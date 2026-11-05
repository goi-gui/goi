package tree

import "github.com/ksckaan1/goi/internal/img"

// Node is any element in the tree. The interface is intentionally narrow: all
// state lives in NodeBase, and namedProps is the only per-type hook.
type Node interface {
	Base() *NodeBase
	// Kind returns the .goi element name.
	Kind() string
	namedProps() []NamedProp
}

// commonProps returns the property table shared by every element. "id" is
// absent on purpose: it is assigned by the builder and is not a runtime cell.
func commonProps(b *NodeBase) []NamedProp {
	return []NamedProp{
		NumberProp(b, "width", DirtyLayout, &b.width),
		NumberProp(b, "height", DirtyLayout, &b.height),
		NumberProp(b, "minWidth", DirtyLayout, &b.minWidth),
		NumberProp(b, "maxWidth", DirtyLayout, &b.maxWidth),
		NumberProp(b, "minHeight", DirtyLayout, &b.minHeight),
		NumberProp(b, "maxHeight", DirtyLayout, &b.maxHeight),
		NumberProp(b, "margin", DirtyLayout, &b.margin),
		NumberProp(b, "padding", DirtyLayout, &b.padding),
		NumberProp(b, "marginTop", DirtyLayout, &b.marginTop),
		NumberProp(b, "marginRight", DirtyLayout, &b.marginRight),
		NumberProp(b, "marginBottom", DirtyLayout, &b.marginBottom),
		NumberProp(b, "marginLeft", DirtyLayout, &b.marginLeft),
		NumberProp(b, "paddingTop", DirtyLayout, &b.paddingTop),
		NumberProp(b, "paddingRight", DirtyLayout, &b.paddingRight),
		NumberProp(b, "paddingBottom", DirtyLayout, &b.paddingBottom),
		NumberProp(b, "paddingLeft", DirtyLayout, &b.paddingLeft),
		ColorProp(b, "background", DirtyPaint, &b.background),
		ColorProp(b, "borderColor", DirtyPaint, &b.borderColor),
		NumberProp(b, "borderWidth", DirtyLayout, &b.borderW),
		NumberProp(b, "radius", DirtyPaint, &b.radius),
		BoolProp(b, "visible", DirtyLayout, &b.visible),
		BoolProp(b, "template", DirtyLayout, &b.template),
		BoolProp(b, "scroll", DirtyPaint, &b.scroll),
		NumberProp(b, "flex", DirtyLayout, &b.flex),
		EnumProp(b, "alignSelf", DirtyLayout, &b.alignSelf, "start", "center", "end", "stretch"),
	}
}

// Window is the root of a tree. It owns the title but no size of its own; the
// platform window provides the outer size.
type Window struct {
	NodeBase
	Title Prop[string]
	// Theme names a scheme: "light", "dark" or one the app registered.
	Theme Prop[string]
	// TitleBar is "system" (the platform's decorations) or "custom" (none, so
	// the design draws a TitleBar of its own).
	TitleBar Prop[string]
	// DisableResize stops the user from resizing or maximizing the window.
	// Fullscreen is a separate switch and still works.
	DisableResize Prop[bool]
	// Fullscreen fills the display instead of showing a normal window.
	Fullscreen Prop[bool]
}

// Kind implements Node.
func (w *Window) Kind() string { return "Window" }

func (w *Window) namedProps() []NamedProp {
	return append(commonProps(&w.NodeBase),
		StringProp(&w.NodeBase, "title", &w.Title),
		StringProp(&w.NodeBase, "theme", &w.Theme),
		EnumProp(&w.NodeBase, "titleBar", DirtyPaint, &w.TitleBar, "system", "custom"),
		BoolProp(&w.NodeBase, "disableResize", DirtyPaint, &w.DisableResize),
		BoolProp(&w.NodeBase, "fullscreen", DirtyPaint, &w.Fullscreen),
	)
}

// CrossAxisValues are the cross-axis alignments a flow container accepts.
// `stretch` is the default feel of a Column: a child that can fill the width
// does, because most children are containers themselves.
var CrossAxisValues = []string{"start", "center", "end", "stretch"}

// MainAxisValues are the main-axis alignments, the same set Flutter's
// MainAxisAlignment offers: the space handlers only matter when the children do
// not already fill the axis.
var MainAxisValues = []string{"start", "center", "end", "spaceBetween", "spaceAround", "spaceEvenly"}

// Container holds the properties shared by Column, Row and Stack. It is never
// a node by itself; the three concrete types give each its own Go type so id
// lookups are type-checked.
type Container struct {
	NodeBase
	Spacing Prop[float64]
	// CrossAlign places a child that is smaller than the container across the
	// container's own direction: side to side in a Column, top to bottom in a
	// Row. MainAlign places the whole run along that direction.
	CrossAlign Prop[string]
	MainAlign  Prop[string]
}

func (c *Container) namedProps() []NamedProp {
	return append(commonProps(&c.NodeBase),
		NumberProp(&c.NodeBase, "spacing", DirtyLayout, &c.Spacing),
		NumberProp(&c.NodeBase, "gap", DirtyLayout, &c.Spacing),
		EnumProp(&c.NodeBase, "crossAxisAlign", DirtyLayout, &c.CrossAlign, CrossAxisValues...),
		EnumProp(&c.NodeBase, "mainAxisAlign", DirtyLayout, &c.MainAlign, MainAxisValues...),
	)
}

// Column stacks children vertically.
type Column struct{ Container }

// Kind implements Node.
func (*Column) Kind() string { return "Column" }

// Row places children horizontally.
type Row struct{ Container }

// Kind implements Node.
func (*Row) Kind() string { return "Row" }

// TitleBar behaves like a Row, and a press on it moves the window. It draws
// nothing itself: the custom title bar's contents are its children.
type TitleBar struct{ Container }

// Kind implements Node.
func (*TitleBar) Kind() string { return "TitleBar" }

// DragRegion marks the node as a place where a press moves the window. A
// clickable child such as a window button keeps its own press.
func (*TitleBar) DragRegion() bool { return true }

// Stack overlays children, last on top.
type Stack struct{ Container }

// Kind implements Node.
func (*Stack) Kind() string { return "Stack" }

// ClipRRect lays its children out like a Stack and cuts them to a rounded box,
// which is what a photo in a circle or a scrolling row inside a pill needs. The
// radius comes from the common `radius` property; a background painted on it is
// rounded too.
type ClipRRect struct{ Container }

// Kind implements Node.
func (*ClipRRect) Kind() string { return "ClipRRect" }

// Positioned places itself inside a Stack. left/top are distances from the
// Stack's content edges; right/bottom anchor the opposite edges. A negative
// value means "auto", and an unanchored axis follows the Stack's align.
type Positioned struct {
	NodeBase
	Left   Prop[float64]
	Top    Prop[float64]
	Right  Prop[float64]
	Bottom Prop[float64]
	// TranslateX/Y shift the box after anchoring, which is what an enter/exit
	// animation needs: left/top reserve -1 for "auto", so they cannot go
	// negative.
	TranslateX Prop[float64]
	TranslateY Prop[float64]
}

// Kind implements Node.
func (*Positioned) Kind() string { return "Positioned" }

func (p *Positioned) namedProps() []NamedProp {
	return append(commonProps(&p.NodeBase),
		NumberProp(&p.NodeBase, "left", DirtyLayout, &p.Left),
		NumberProp(&p.NodeBase, "top", DirtyLayout, &p.Top),
		NumberProp(&p.NodeBase, "right", DirtyLayout, &p.Right),
		NumberProp(&p.NodeBase, "bottom", DirtyLayout, &p.Bottom),
		NumberProp(&p.NodeBase, "translateX", DirtyLayout, &p.TranslateX),
		NumberProp(&p.NodeBase, "translateY", DirtyLayout, &p.TranslateY),
	)
}

// Wrap flows children horizontally and continues on a new run when the width
// runs out. spacing is both the gap inside a run and the gap between runs.
type Wrap struct{ Container }

// Kind implements Node.
func (*Wrap) Kind() string { return "Wrap" }

// Grid lays children out in a fixed number of equal-width columns. spacing is
// the gap between columns and between rows.
type Grid struct {
	Container
	Columns Prop[float64]
}

// Kind implements Node.
func (*Grid) Kind() string { return "Grid" }

func (g *Grid) namedProps() []NamedProp {
	return append(g.Container.namedProps(),
		NumberProp(&g.NodeBase, "columns", DirtyLayout, &g.Columns),
	)
}

// Rectangle is a plain colored box.
type Rectangle struct {
	NodeBase
}

// Kind implements Node.
func (r *Rectangle) Kind() string { return "Rectangle" }

func (r *Rectangle) namedProps() []NamedProp { return commonProps(&r.NodeBase) }

// Image draws a decoded bitmap. The ui layer fills Data from Src, because a node
// has no way to read files; an empty Data draws the Background as a placeholder,
// which keeps a missing file visible instead of blank.
type Image struct {
	NodeBase
	Src  Prop[string]
	Fit  Prop[string]
	Data *img.Data
	// Loading is true while the ui layer is fetching a remote src; paint draws a
	// breathing placeholder for it instead of leaving a hole.
	Loading bool
	// Phase is that placeholder's 0..1 opacity, advanced per frame by the ui
	// layer. It is UI state, not a property: a rate belongs to the loop, not to
	// the design file.
	Phase float64
	// LoadErr is the load or decode failure, if any. The node keeps drawing its
	// placeholder, which is why a bad src shows as a coloured box.
	LoadErr error
}

// Kind implements Node.
func (i *Image) Kind() string { return "Image" }

func (i *Image) namedProps() []NamedProp {
	return append(commonProps(&i.NodeBase),
		StringProp(&i.NodeBase, "src", &i.Src),
		EnumProp(&i.NodeBase, "fit", DirtyLayout, &i.Fit, "cover", "contain", "fill"),
	)
}

// SVG draws an SVG file. The ui layer rasterizes it, replacing the file's
// currentColor with Color, so one icon can follow the theme. IntrinsicW/H is
// the viewBox size, which is the element's size when width and height are
// omitted.
type SVG struct {
	NodeBase
	Src   Prop[string]
	Fit   Prop[string]
	Color Prop[uint32]
	Data  *img.Data
	// IntrinsicW/H is the parsed viewBox size in logical pixels.
	IntrinsicW, IntrinsicH float64
	// RenderedSrc and RenderedColor are what Data was rasterized from. The ui
	// layer compares them to re-rasterize when Go changes the colour.
	RenderedSrc   string
	RenderedColor uint32
	// LoadErr is a read, parse or rasterize failure. The node keeps drawing
	// whatever background its box has, so a bad file is visible rather than a
	// silent hole.
	LoadErr error
	// Loading is true while a remote src is on the way; paint draws the same
	// breathing placeholder Image uses. Phase is that placeholder's 0..1
	// opacity, advanced per frame by the ui layer.
	Loading bool
	Phase   float64
}

// Kind implements Node.
func (s *SVG) Kind() string { return "SVG" }

func (s *SVG) namedProps() []NamedProp {
	return append(commonProps(&s.NodeBase),
		StringProp(&s.NodeBase, "src", &s.Src),
		EnumProp(&s.NodeBase, "fit", DirtyLayout, &s.Fit, "cover", "contain", "fill"),
		ColorProp(&s.NodeBase, "color", DirtyPaint, &s.Color),
	)
}

// Text draws a single run of text. FontSize 0 means "use the M3 role in Role";
// Role is one of the type-scale names (bodyMedium is the default).
type Text struct {
	NodeBase
	Text       Prop[string]
	FontSize   Prop[float64]
	Color      Prop[uint32]
	Align      Prop[string]
	Wrap       Prop[bool]
	Role       Prop[string]
	Weight     Prop[float64]
	Selectable Prop[bool]
	// SelA and SelB are the selection in byte offsets; equal values mean none.
	SelA, SelB int
}

// Kind implements Node.
func (t *Text) Kind() string { return "Text" }

func (t *Text) namedProps() []NamedProp {
	return append(commonProps(&t.NodeBase),
		StringProp(&t.NodeBase, "text", &t.Text),
		NumberProp(&t.NodeBase, "fontSize", DirtyLayout, &t.FontSize),
		ColorProp(&t.NodeBase, "color", DirtyPaint, &t.Color),
		EnumProp(&t.NodeBase, "align", DirtyLayout, &t.Align, "start", "center", "end"),
		BoolProp(&t.NodeBase, "wrap", DirtyLayout, &t.Wrap),
		EnumProp(&t.NodeBase, "role", DirtyLayout, &t.Role,
			"displayLarge", "displayMedium", "displaySmall",
			"headlineLarge", "headlineMedium", "headlineSmall",
			"titleLarge", "titleMedium", "titleSmall",
			"bodyLarge", "bodyMedium", "bodySmall",
			"labelLarge", "labelMedium", "labelSmall"),
		NumberProp(&t.NodeBase, "weight", DirtyLayout, &t.Weight),
		BoolProp(&t.NodeBase, "selectable", DirtyPaint, &t.Selectable),
	)
}
