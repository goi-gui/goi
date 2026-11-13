package tree

import (
	"image"
	"time"

	"github.com/goi-gui/goi/internal/img"
	"github.com/goi-gui/goi/internal/schema"
	"github.com/goi-gui/goi/internal/svg"
	"github.com/goi-gui/goi/internal/text"
)

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
		SizeProp(b),
		NumberProp(b, "minWidth", DirtyLayout, &b.minWidth),
		NumberProp(b, "maxWidth", DirtyLayout, &b.maxWidth),
		NumberProp(b, "minHeight", DirtyLayout, &b.minHeight),
		NumberProp(b, "maxHeight", DirtyLayout, &b.maxHeight),
		NumberProp(b, "margin", DirtyLayout, &b.margin),
		NumberProp(b, "padding", DirtyLayout, &b.padding),
		NumberProp(b, "paddingX", DirtyLayout, &b.paddingX),
		NumberProp(b, "paddingY", DirtyLayout, &b.paddingY),
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
		NumberProp(b, "borderTopWidth", DirtyLayout, &b.borderTopW),
		NumberProp(b, "borderRightWidth", DirtyLayout, &b.borderRightW),
		NumberProp(b, "borderBottomWidth", DirtyLayout, &b.borderBottomW),
		NumberProp(b, "borderLeftWidth", DirtyLayout, &b.borderLeftW),
		ColorProp(b, "borderTopColor", DirtyPaint, &b.borderTopColor),
		ColorProp(b, "borderRightColor", DirtyPaint, &b.borderRightColor),
		ColorProp(b, "borderBottomColor", DirtyPaint, &b.borderBottomColor),
		ColorProp(b, "borderLeftColor", DirtyPaint, &b.borderLeftColor),
		ColorProp(b, "borderTopColor", DirtyPaint, &b.borderTopColor),
		ColorProp(b, "borderRightColor", DirtyPaint, &b.borderRightColor),
		ColorProp(b, "borderBottomColor", DirtyPaint, &b.borderBottomColor),
		ColorProp(b, "borderLeftColor", DirtyPaint, &b.borderLeftColor),
		NumberProp(b, "borderTopWidth", DirtyLayout, &b.borderTopW),
		NumberProp(b, "borderRightWidth", DirtyLayout, &b.borderRightW),
		NumberProp(b, "borderBottomWidth", DirtyLayout, &b.borderBottomW),
		NumberProp(b, "borderLeftWidth", DirtyLayout, &b.borderLeftW),
		NumberProp(b, "radius", DirtyPaint, &b.radius),
		NumberProp(b, "radiusTop", DirtyPaint, &b.radiusTop),
		NumberProp(b, "radiusRight", DirtyPaint, &b.radiusRight),
		NumberProp(b, "radiusBottom", DirtyPaint, &b.radiusBottom),
		NumberProp(b, "radiusLeft", DirtyPaint, &b.radiusLeft),
		NumberProp(b, "radiusTopLeft", DirtyPaint, &b.radiusTopLeft),
		NumberProp(b, "radiusTopRight", DirtyPaint, &b.radiusTopRight),
		NumberProp(b, "radiusBottomRight", DirtyPaint, &b.radiusBottomRight),
		NumberProp(b, "radiusBottomLeft", DirtyPaint, &b.radiusBottomLeft),
		StringProp(b, "fontFamily", &b.fontFamily),
		NamedShadow(b, "shadow", &b.shadow),
		BoolProp(b, "visible", DirtyLayout, &b.visible),
		BoolProp(b, "template", DirtyLayout, &b.template),
		BoolProp(b, "scroll", DirtyPaint, &b.scroll),
		BoolProp(b, "hideScrollbar", DirtyPaint, &b.hideScrollbar),
		BoolProp(b, "autoHideScrollbar", DirtyPaint, &b.autoHideScrollbar),
		NumberProp(b, "scrollbarWidth", DirtyPaint, &b.scrollbarWidth),
		ColorProp(b, "scrollbarColor", DirtyPaint, &b.scrollbarColor),
		BoolProp(b, "scrollShadow", DirtyPaint, &b.scrollShadow),
		ColorProp(b, "scrollShadowColor", DirtyPaint, &b.scrollShadowColor),
		NumberProp(b, "opacity", DirtyPaint, &b.opacity),
		NumberProp(b, "backdropBlur", DirtyPaint, &b.backdropBlur),
		NumberProp(b, "zIndex", DirtyPaint, &b.zIndex),
		EnumProp(b, "cursor", DirtyPaint, &b.cursor, schema.CursorValues...),
		NumberProp(b, "flex", DirtyLayout, &b.flex),
		EnumProp(b, "alignSelf", DirtyLayout, &b.alignSelf, "start", "center", "end", "stretch"),
		RotateProp(b, "rotate", DirtyPaint, &b.rotate),
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
	// Resizable is the same switch read the other way round, and is the one a
	// design or Go is expected to write.
	DisableResize Prop[bool]
	// Resizable lets the user resize the window; `disableResize: true` is its
	// old spelling and still works.
	Resizable Prop[bool]
	// Minimizable and Maximizable gate the framework's own minimize and maximize
	// requests. A design that draws its own title bar uses them to disable its
	// buttons.
	Minimizable Prop[bool]
	Maximizable Prop[bool]
	// Minimized and Maximized are requests: writing one asks the native window to
	// move to that state. What the window actually is lives in the platform
	// report below, which is what `window.minimized` and the Go getters read.
	Minimized Prop[bool]
	Maximized Prop[bool]

	// platformMin/Max/Full are the state the platform last reported, and
	// platformKnown says whether it has reported yet. They are deliberately
	// separate from the request props: echoing into the request would undo a
	// maximize a compositor applies one frame later.
	platformMin, platformMax, platformFull bool
	platformKnown                          bool
	// Fullscreen fills the display instead of showing a normal window.
	Fullscreen Prop[bool]
}

// Kind implements Node.
func (w *Window) Kind() string { return "Window" }

// SetPlatformState records the state the platform actually reported, so a design
// reading `window.minimized` sees the truth, and reports whether either value
// changed. It does not mark the node dirty: the caller decides whether a repaint
// is owed.
func (w *Window) SetPlatformState(minimized, maximized, fullscreen bool) bool {
	changed := !w.platformKnown || w.platformMin != minimized ||
		w.platformMax != maximized || w.platformFull != fullscreen
	w.platformMin, w.platformMax, w.platformFull = minimized, maximized, fullscreen
	w.platformKnown = true
	return changed
}

// PlatformState returns the minimized, maximized and fullscreen state the
// platform last reported, plus whether it has reported at all. Before the first
// report the request props are the best answer, which is what a headless window
// has.
func (w *Window) PlatformState() (minimized, maximized, fullscreen, known bool) {
	return w.platformMin, w.platformMax, w.platformFull, w.platformKnown
}

func (w *Window) namedProps() []NamedProp {
	return append(
		commonProps(&w.NodeBase),
		StringProp(&w.NodeBase, "title", &w.Title),
		StringProp(&w.NodeBase, "theme", &w.Theme),
		EnumProp(&w.NodeBase, "titleBar", DirtyPaint, &w.TitleBar, "system", "custom"),
		BoolProp(&w.NodeBase, "disableResize", DirtyPaint, &w.DisableResize),
		BoolProp(&w.NodeBase, "resizable", DirtyPaint, &w.Resizable),
		BoolProp(&w.NodeBase, "minimizable", DirtyPaint, &w.Minimizable),
		BoolProp(&w.NodeBase, "maximizable", DirtyPaint, &w.Maximizable),
		BoolProp(&w.NodeBase, "minimized", DirtyPaint, &w.Minimized),
		BoolProp(&w.NodeBase, "maximized", DirtyPaint, &w.Maximized),
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
	return append(
		commonProps(&c.NodeBase),
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
	return append(
		commonProps(&p.NodeBase),
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
	return append(
		g.Container.namedProps(),
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
	// RenderedSrc is the src the current Data came from, so the ui layer can
	// tell a src an expression just changed from one it already loaded.
	RenderedSrc string
	// Anim is the decoded animation behind src, nil for a still picture. Frame
	// and AnimTime are the player's state: the ui layer advances them and swaps
	// Data.Pix before repainting, so the design file never mentions them.
	Anim     *img.Anim
	Frame    int
	AnimTime time.Duration
	// AnimDone is set when a finite animation played its passes; the last
	// frame then stays on screen.
	AnimDone bool
	// Paused stops the animation clock; it stays where it is until set back.
	Paused Prop[bool]
	// Loop keeps an animation running after its first pass; false plays it
	// once and holds the last frame. True by default.
	Loop Prop[bool]
}

// Kind implements Node.
func (i *Image) Kind() string { return "Image" }

// SetImage shows an image that already exists in Go: a chart, a screenshot, a
// picture decoded from somewhere the framework does not read. It replaces
// whatever `src` loaded, and needs no file at all.
//
// Call it from Bind, which runs again after every hot reload; the bitmap itself
// is not carried across a reload.
func (i *Image) SetImage(src image.Image) {
	if src == nil {
		i.Data = nil
	} else {
		i.Data = img.FromImage(src)
	}
	i.Anim = nil
	i.Frame, i.AnimTime, i.AnimDone = 0, 0, false
	i.Paused.Set(false)
	i.LoadErr = nil
	i.Loading = false
	i.Base().MarkDirty(DirtyPaint)
}

func (i *Image) namedProps() []NamedProp {
	return append(
		commonProps(&i.NodeBase),
		StringProp(&i.NodeBase, "src", &i.Src),
		EnumProp(&i.NodeBase, "fit", DirtyLayout, &i.Fit, "cover", "contain", "fill"),
		BoolProp(&i.NodeBase, "paused", DirtyPaint, &i.Paused),
		BoolProp(&i.NodeBase, "loop", DirtyPaint, &i.Loop),
	)
}

// Play resumes an animation from where it is. After Stop or a finished
// one-shot it replays from the first frame.
func (i *Image) Play() {
	if i.AnimDone {
		i.rewind()
	}
	i.Paused.Set(false)
	i.AnimDone = false
	i.Base().MarkDirty(DirtyPaint)
}

// rewind puts the first frame back on screen.
func (i *Image) rewind() {
	i.Frame, i.AnimTime = 0, 0
	if i.Anim != nil && len(i.Anim.Frames) > 0 && i.Data != nil {
		i.Data.Pix = i.Anim.Frames[0].Pix
		i.Data.Gen++
	}
}

// Pause holds the current frame.
func (i *Image) Pause() {
	i.Paused.Set(true)
	i.Base().MarkDirty(DirtyPaint)
}

// Stop rewinds to the first frame and pauses there.
func (i *Image) Stop() {
	i.Paused.Set(true)
	i.AnimDone = false
	i.rewind()
	i.Base().MarkDirty(DirtyPaint)
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
	// Inline is SVG source set from Go with SetDataString/SetDataBytes. While it
	// is set it replaces `src`. InlineGen counts the sets and RenderedGen is the
	// one the ui layer last rasterized, so a frame can tell a new document from
	// the one already on the GPU.
	Inline      []byte
	InlineGen   int
	RenderedGen int
	// Source is the parsed document when it animates, so a frame can be
	// rasterized again without parsing the file; AnimDur is its length.
	Source *svg.SVG
	// AnimDur is the file's SMIL animation length, zero for a still document.
	// AnimTime and Frame are the player's state, advanced by the ui layer.
	AnimDur  time.Duration
	AnimTime time.Duration
	Frame    int
	// AnimDone is set when a finite animation played its passes.
	AnimDone bool
	// Paused stops the SMIL clock; the frame it stopped on stays on screen.
	Paused Prop[bool]
	// Loop keeps a SMIL animation running after its first pass; false plays it
	// once and holds the last frame. True by default.
	Loop Prop[bool]
}

// Kind implements Node.
func (s *SVG) Kind() string { return "SVG" }

// SetDataString draws an SVG document held in Go as a string, instead of
// loading `src`. `color` still replaces the document's currentColor.
//
// Call it from Bind; the document is not carried across a hot reload.
func (s *SVG) SetDataString(v string) { s.SetDataBytes([]byte(v)) }

// Play resumes a SMIL animation from where it is. After Stop or a finished
// one-shot it replays from the first frame.
func (s *SVG) Play() {
	if s.AnimDone {
		s.AnimTime, s.Frame = 0, 0
	}
	s.Paused.Set(false)
	s.AnimDone = false
	s.Base().MarkDirty(DirtyPaint)
}

// Pause holds the current SMIL frame.
func (s *SVG) Pause() {
	s.Paused.Set(true)
	s.Base().MarkDirty(DirtyPaint)
}

// Stop rewinds a SMIL animation to the start and pauses there; the ui layer
// draws the first frame on the next pass.
func (s *SVG) Stop() {
	s.Paused.Set(true)
	s.AnimTime, s.Frame, s.AnimDone = 0, 0, false
	s.Base().MarkDirty(DirtyPaint)
}

// SetDataBytes is SetDataString for bytes an icon was embedded as.
func (s *SVG) SetDataBytes(b []byte) {
	s.Inline = b
	s.InlineGen++
	s.AnimTime, s.Frame, s.AnimDone = 0, 0, false
	s.Source = nil
	s.LoadErr = nil
	s.Loading = false
	// The new document may have a different viewBox, so the element's size can
	// change; the rasterizer fills IntrinsicW/H on the next frame.
	s.Base().MarkDirty(DirtyLayout)
}

func (s *SVG) namedProps() []NamedProp {
	return append(
		commonProps(&s.NodeBase),
		BoolProp(&s.NodeBase, "paused", DirtyPaint, &s.Paused),
		BoolProp(&s.NodeBase, "loop", DirtyPaint, &s.Loop),
		StringProp(&s.NodeBase, "src", &s.Src),
		EnumProp(&s.NodeBase, "fit", DirtyLayout, &s.Fit, "cover", "contain", "fill"),
		ColorProp(&s.NodeBase, "color", DirtyPaint, &s.Color),
	)
}

// textRoles are the type-scale names a Text or a RichText may name as its role.
var textRoles = []string{
	"displayLarge", "displayMedium", "displaySmall",
	"headlineLarge", "headlineMedium", "headlineSmall",
	"titleLarge", "titleMedium", "titleSmall",
	"bodyLarge", "bodyMedium", "bodySmall",
	"labelLarge", "labelMedium", "labelSmall",
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
	FontStyle  Prop[string]
	Decoration Prop[string]
	// DecorationStyle, DecorationThickness and DecorationColor apply to every
	// line `decoration` names.
	DecorationStyle     Prop[string]
	DecorationThickness Prop[float64]
	DecorationColor     Prop[uint32]
	Selectable          Prop[bool]
	// Link is the URL, deep link or path this text points at. A text with one is
	// a link: it draws in the primary colour, and a click opens it unless a
	// LinkClick handler says otherwise. Inside a RichText it is the run's link.
	Link Prop[string]
	// LinkClick is the handler slot for a clicked link. With a handler goi does
	// not open anything, so an app can route `goi://settings` itself.
	LinkClick Event[string]
	// SelA and SelB are the selection in byte offsets; equal values mean none.
	SelA, SelB int
}

// OnLink subscribes to a clicked link, which is the text's own or the run of a
// RichText under the pointer. Without a handler goi hands the target to the
// desktop: the user's browser for a URL or a deep link, the file manager for a
// path that exists.
func (t *Text) OnLink(fn func(link string)) (off func()) { return t.LinkClick.On(fn) }

// Relative font weights, the values `fontWeight: bolder` and `lighter` keep
// until Text.WeightFor resolves them against the role's weight.
const (
	weightBolder  = -1
	weightLighter = -2
)

// WeightFor resolves weight/fontWeight against the role's weight. bolder and
// lighter are relative, so they need the role; layout and paint both call this
// so a line is measured with the face it is drawn in.
func (t *Text) WeightFor(role int) int {
	switch w := t.Weight.Get(); {
	case w == weightBolder:
		return text.StepWeight(role, true)
	case w == weightLighter:
		return text.StepWeight(role, false)
	case w > 0:
		return int(w)
	}
	return role
}

// Kind implements Node.
func (t *Text) Kind() string { return "Text" }

func (t *Text) namedProps() []NamedProp {
	return append(
		commonProps(&t.NodeBase),
		StringProp(&t.NodeBase, "text", &t.Text),
		NumberProp(&t.NodeBase, "fontSize", DirtyLayout, &t.FontSize),
		ColorProp(&t.NodeBase, "color", DirtyPaint, &t.Color),
		EnumProp(&t.NodeBase, "align", DirtyLayout, &t.Align, "start", "center", "end"),
		BoolProp(&t.NodeBase, "wrap", DirtyLayout, &t.Wrap),
		EnumProp(&t.NodeBase, "role", DirtyLayout, &t.Role, textRoles...),
		NumberProp(&t.NodeBase, "weight", DirtyLayout, &t.Weight),
		// fontWeight is the CSS spelling: the parser turns its keywords into the
		// same number, so both names share one cell and the last write wins.
		NumberProp(&t.NodeBase, "fontWeight", DirtyLayout, &t.Weight),
		EnumProp(&t.NodeBase, "fontStyle", DirtyPaint, &t.FontStyle, "normal", "italic"),
		StringProp(&t.NodeBase, "decoration", &t.Decoration),
		EnumProp(&t.NodeBase, "decorationStyle", DirtyPaint, &t.DecorationStyle,
			"solid", "wavy", "dotted", "double", "dashed"),
		NumberProp(&t.NodeBase, "decorationThickness", DirtyPaint, &t.DecorationThickness),
		ColorProp(&t.NodeBase, "decorationColor", DirtyPaint, &t.DecorationColor),
		BoolProp(&t.NodeBase, "selectable", DirtyPaint, &t.Selectable),
		StringProp(&t.NodeBase, "link", &t.Link),
	)
}

// Transport runs one of the playback methods an event can call by id, so a
// design can write `onClick: spin.pause()` without a bridge handler.
func (i *Image) Transport(method string) bool {
	switch method {
	case "play":
		i.Play()
	case "pause":
		i.Pause()
	case "stop":
		i.Stop()
	default:
		return false
	}
	return true
}

// Transport runs one of the playback methods an event can call by id.
func (s *SVG) Transport(method string) bool {
	switch method {
	case "play":
		s.Play()
	case "pause":
		s.Pause()
	case "stop":
		s.Stop()
	default:
		return false
	}
	return true
}
