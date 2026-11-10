package tree

import (
	"fmt"
	"math"

	"github.com/goi-gui/goi/internal/i18n"
	"github.com/goi-gui/goi/internal/schema"
	"github.com/goi-gui/goi/internal/syntax"
)

// Tree is a live node tree plus its id index.
type Tree struct {
	Root *Window
	ids  map[string]Node
	// responsive lists the nodes that declared at least one breakpoint map.
	responsive []*NodeBase
	// exprNodes lists the nodes with at least one expression property, which
	// includes a node whose only expression is a conditional modifier.
	exprNodes []*NodeBase
	// colorNodes lists the nodes with at least one role-valued color, and
	// shadowNodes the ones whose shadow names a role.
	colorNodes  []*NodeBase
	shadowNodes []*NodeBase
	// viewW and viewH are the last viewport in logical pixels, so an expression
	// can read window.width and window.height.
	viewW, viewH float64
	// mode is the resolved scheme's light/dark mood, so `app.mode` and
	// `app.dark` follow any registered theme, not only one literally named
	// "dark". modeSet marks that the driver resolved it; before that a tree
	// reads the theme's name.
	mode    Mode
	modeSet bool
	// systemMode is the desktop's preference, or ModeUnknown when the platform
	// does not report one. A design follows it with `theme: app.systemMode`.
	systemMode Mode
	// systemLanguage is the desktop's preferred language code, read once at
	// startup and again on a locale change. `app.systemLanguage` exposes it.
	systemLanguage string
	// i18n is the design's catalog, nil when the app ships no i18n folder. The
	// language pair follows it: an empty code means "use systemLanguage", which
	// is why both defaults need no startup write.
	i18n             *i18n.Bundle
	language         string
	fallbackLanguage string
	// evalSeen is the scratch reference chain of an expression pass, so
	// evaluating one costs no allocation.
	evalSeen []refKey
	// cloning is set while Instantiate builds a copy: ids stay on the nodes so
	// an app can find them, but they are not registered, because a copy would
	// otherwise collide with the template's own ids.
	cloning bool
	// conds holds the arms of every {#if} block, in build order, so the frame
	// loop can toggle their visibility.
	conds []*condBranch
	// vars holds the `data` values an app set with SetData.
	vars map[string]any
	// eachs lists the {#each} blocks, so setting a list rebuilds the blocks
	// that walk it.
	eachs []*eachBlock
	// eachHooks lists the app callbacks that run for every built row.
	eachHooks map[string][]func(Node, any)
	// scope is the loop scope during a build, nil outside an {#each} arm.
	scope *scope
	// dataErr is the first error a data fill hit, reported to the app.
	dataErr error
	// bridge holds the Go handlers a design's bridge block declares.
	bridge *Bridge
	// report receives runtime errors, when the app supplied Hooks.Error.
	report func(error)
}

// ByID returns the node with the given id.
func (t *Tree) ByID(id string) (Node, bool) {
	n, ok := t.ids[id]
	return n, ok
}

// IDs returns the number of indexed ids.
func (t *Tree) IDs() int { return len(t.ids) }

// All returns every node, parents before children.
// All returns every node in the tree, root first. It allocates, so it is for
// tests and one-off work; Walk is the per-frame variant.
func (t *Tree) All() []Node {
	var out []Node
	var walk func(*NodeBase)
	walk = func(b *NodeBase) {
		out = append(out, b.node)
		for _, c := range b.kids {
			walk(c)
		}
	}
	walk(&t.Root.NodeBase)
	return out
}

// Walk calls fn for every node, root first, without building a slice. Use it
// where All would run every frame or every pointer move: a hover update or a
// tick that scans for one kind of node.
// WalkVisible is Walk without the subtrees hidden by `visible: false`: an
// animation inside a hidden page costs nothing, and a pruned tree is the same
// one paint draws.
func (t *Tree) WalkVisible(fn func(Node)) {
	var walk func(*NodeBase)
	walk = func(b *NodeBase) {
		if !b.Visible() {
			return
		}
		fn(b.node)
		for _, c := range b.kids {
			walk(c)
		}
	}
	if t.Root != nil {
		walk(&t.Root.NodeBase)
	}
}

func (t *Tree) Walk(fn func(Node)) {
	var walk func(*NodeBase)
	walk = func(b *NodeBase) {
		fn(b.node)
		for _, c := range b.kids {
			walk(c)
		}
	}
	if t.Root != nil {
		walk(&t.Root.NodeBase)
	}
}

// Build instantiates a validated file. The schema defaults are applied first,
// then the literals from the file. hooks may be nil for headless tests.
func Build(f *syntax.File, hooks *Hooks) (*Tree, error) {
	if f == nil || f.Root == nil {
		return nil, fmt.Errorf("tree: no root element")
	}
	t := &Tree{ids: map[string]Node{}, bridge: &Bridge{}}
	if hooks != nil {
		t.report = hooks.Error
	}
	// A bridge variable starts at its type's zero value, so a design that reads
	// one before Go sets it still has a value.
	t.vars = map[string]any{}
	for _, dv := range f.Vars {
		if v, ok := zeroRuntime(dv.Type); ok {
			t.vars[dv.Name] = v
		}
	}
	root, err := t.build(f.Root, nil, hooks)
	if err != nil {
		return nil, err
	}
	w, ok := root.(*Window)
	if !ok {
		return nil, fmt.Errorf("tree: root must be Window, got %s", root.Kind())
	}
	t.Root = w
	// Blocks show their first matching arm from the start, so a headless build
	// and the first frame agree before any expression has run.
	t.applyConds()
	t.clearDirty()
	return t, nil
}

// clearDirty marks every node clean, so a freshly built tree does not schedule
// a frame before anything changed.
func (t *Tree) clearDirty() {
	for _, n := range t.All() {
		n.Base().ClearDirty()
	}
}

// BuildSource parses, validates and instantiates in one go.
func BuildSource(src []byte, hooks *Hooks) (*Tree, syntax.Diagnostics) {
	f, diag := syntax.Parse(src)
	if !diag.Empty() {
		return nil, diag
	}
	if _, diag := schema.Validate(f); !diag.Empty() {
		return nil, diag
	}
	t, err := Build(f, hooks)
	if err != nil {
		return nil, syntax.Diagnostics{{Message: err.Error()}}
	}
	return t, nil
}

func (t *Tree) build(el *syntax.Element, parent *NodeBase, hooks *Hooks) (Node, error) {
	def, ok := schema.Lookup(el.Name)
	if !ok {
		return nil, fmt.Errorf("unknown element %q", el.Name)
	}
	n := newNode(el.Name)
	if n == nil {
		return nil, fmt.Errorf("unknown element %q", el.Name)
	}
	b := n.Base()
	b.node = n
	b.parent = parent
	b.tree = t
	b.scope = t.scope
	b.AST = el
	b.properties() // wires every Prop cell to b before any value is written
	applyDefaults(b, def)
	b.hooks = hooks
	if parent != nil {
		parent.kids = append(parent.kids, b)
	}

	for _, p := range el.Props() {
		if p.Name == "id" {
			id, _ := literalText(p.Value)
			b.id = id
			if id != "" && !t.cloning {
				if _, dup := t.ids[id]; dup {
					return nil, fmt.Errorf("duplicate id %q", id)
				}
				t.ids[id] = n
			}
			continue
		}
		if _, ok := schema.Event(p.Name); ok {
			if err := t.bindEvent(b, p.Name, p.Value); err != nil {
				return nil, fmt.Errorf("%s: %w", el.Name, err)
			}
			continue
		}
		if m, ok := p.Value.(*syntax.Map); ok {
			if !b.setResponsive(p.Name, m) {
				return nil, fmt.Errorf("%s.%s: invalid responsive value", el.Name, p.Name)
			}
			continue
		}
		if m, ok := p.Value.(*syntax.Modifier); ok {
			if !b.setModifier(p.Name, m) {
				return nil, fmt.Errorf("%s.%s: invalid %s value", el.Name, p.Name, m.Form)
			}
			continue
		}
		if !b.applyValue(p.Name, p.Value) {
			return nil, fmt.Errorf("%s.%s: value not valid for the property type", el.Name, p.Name)
		}
	}
	// A template is hidden before its children are built, so it never joins
	// layout. The flag stays on the node for ui.Fill to find.
	if b.template.Get() {
		b.SetVisible(false)
	}
	if len(b.responsive) > 0 {
		t.responsive = append(t.responsive, b)
	}

	if err := t.buildMembers(el.Members, b, hooks, nil); err != nil {
		return nil, err
	}
	return n, nil
}

// buildMembers builds one member list under parent. A block's arms are built as
// ordinary children of the same parent, tagged with the arm that controls them.
func (t *Tree) buildMembers(members []syntax.Member, parent *NodeBase, hooks *Hooks, arm *condBranch) error {
	for _, m := range members {
		switch x := m.(type) {
		case *syntax.Element:
			n, err := t.build(x, parent, hooks)
			if err != nil {
				return err
			}
			if arm != nil {
				arm.nodes = append(arm.nodes, n.Base())
				arm.own = append(arm.own, n.Base().Visible())
			}
		case *syntax.Block:
			if x.Kind == syntax.BlockFor {
				if err := t.buildFor(x, parent, hooks); err != nil {
					return err
				}
				continue
			}
			if err := t.buildBlock(x, parent, hooks, arm); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyDefaults seeds every property of the node from the schema.
func applyDefaults(b *NodeBase, def *schema.Element) {
	for _, pd := range def.Attrs {
		if pd.Name == "id" || pd.Default == nil {
			continue
		}
		b.SetPropLiteral(pd.Name, defaultValueLiteral(pd.Default))
	}
	b.ClearDirty() // defaults are not a user-visible change
}

// literalText returns the text of a String or Ident literal.
func literalText(v syntax.Value) (string, bool) {
	switch x := v.(type) {
	case *syntax.String:
		return x.Val, true
	case *syntax.Ident:
		return x.Val, true
	}
	return "", false
}

// defaultValueLiteral converts a schema default into a syntax literal so the
// node property setters stay the single write path.
func defaultValueLiteral(v any) syntax.Value {
	switch x := v.(type) {
	case string:
		return &syntax.Ident{Val: x}
	case float64:
		return &syntax.Number{Val: x}
	case bool:
		return &syntax.Bool{Val: x}
	case uint32:
		return &syntax.Color{RGBA: x}
	}
	return &syntax.Ident{}
}

func newNode(name string) Node {
	b := func() NodeBase {
		return NodeBase{
			visible:           newProp(nil, "visible", DirtyLayout, true),
			template:          newProp(nil, "template", DirtyLayout, false),
			margin:            newProp(nil, "margin", DirtyLayout, 0.0),
			padding:           newProp(nil, "padding", DirtyLayout, 0.0),
			marginTop:         newProp(nil, "marginTop", DirtyLayout, -1.0),
			marginRight:       newProp(nil, "marginRight", DirtyLayout, -1.0),
			marginBottom:      newProp(nil, "marginBottom", DirtyLayout, -1.0),
			marginLeft:        newProp(nil, "marginLeft", DirtyLayout, -1.0),
			paddingTop:        newProp(nil, "paddingTop", DirtyLayout, -1.0),
			paddingRight:      newProp(nil, "paddingRight", DirtyLayout, -1.0),
			paddingBottom:     newProp(nil, "paddingBottom", DirtyLayout, -1.0),
			paddingLeft:       newProp(nil, "paddingLeft", DirtyLayout, -1.0),
			borderW:           newProp(nil, "borderWidth", DirtyLayout, 0.0),
			flex:              newProp(nil, "flex", DirtyLayout, 0.0),
			alignSelf:         newProp(nil, "alignSelf", DirtyLayout, ""),
			scroll:            newProp(nil, "scroll", DirtyPaint, false),
			hideScrollbar:     newProp(nil, "hideScrollbar", DirtyPaint, false),
			autoHideScrollbar: newProp(nil, "autoHideScrollbar", DirtyPaint, true),
			scrollbarWidth:    newProp(nil, "scrollbarWidth", DirtyPaint, ScrollbarThickness),
			scrollbarColor:    newProp(nil, "scrollbarColor", DirtyPaint, uint32(0)),
			scrollShadow:      newProp(nil, "scrollShadow", DirtyPaint, false),
			scrollShadowColor: newProp(nil, "scrollShadowColor", DirtyPaint, uint32(0)),
			opacity:           newProp(nil, "opacity", DirtyPaint, 1.0),
			zIndex:            newProp(nil, "zIndex", DirtyPaint, 0.0),
			cursor:            newProp(nil, "cursor", DirtyPaint, ""),
			width:             newProp(nil, "width", DirtyLayout, -1.0),
			height:            newProp(nil, "height", DirtyLayout, -1.0),
			minWidth:          newProp(nil, "minWidth", DirtyLayout, 0.0),
			maxWidth:          newProp(nil, "maxWidth", DirtyLayout, -1.0),
			minHeight:         newProp(nil, "minHeight", DirtyLayout, 0.0),
			maxHeight:         newProp(nil, "maxHeight", DirtyLayout, -1.0),
			background:        newProp(nil, "background", DirtyPaint, uint32(0)),
			borderColor:       newProp(nil, "borderColor", DirtyPaint, uint32(0)),
			radius:            newProp(nil, "radius", DirtyPaint, 0.0),
		}
	}
	switch name {
	case "Window":
		return &Window{
			NodeBase:      b(),
			Title:         newProp(nil, "title", DirtyPaint, ""),
			Theme:         newProp(nil, "theme", DirtyPaint, "light"),
			TitleBar:      newProp(nil, "titleBar", DirtyPaint, "system"),
			DisableResize: newProp(nil, "disableResize", DirtyPaint, false),
			Resizable:     newProp(nil, "resizable", DirtyPaint, true),
			Minimizable:   newProp(nil, "minimizable", DirtyPaint, true),
			Maximizable:   newProp(nil, "maximizable", DirtyPaint, true),
			Minimized:     newProp(nil, "minimized", DirtyPaint, false),
			Maximized:     newProp(nil, "maximized", DirtyPaint, false),
			Fullscreen:    newProp(nil, "fullscreen", DirtyPaint, false),
		}
	case "Column", "Row", "Stack", "ClipRRect", "Wrap", "Grid", "TitleBar":
		c := Container{
			NodeBase:   b(),
			Spacing:    newProp(nil, "spacing", DirtyLayout, 0.0),
			CrossAlign: newProp(nil, "crossAxisAlign", DirtyLayout, "start"),
			MainAlign:  newProp(nil, "mainAxisAlign", DirtyLayout, "start"),
		}
		switch name {
		case "Column":
			return &Column{Container: c}
		case "ClipRRect":
			return &ClipRRect{Container: c}
		case "Row":
			return &Row{Container: c}
		case "TitleBar":
			return &TitleBar{Container: c}
		case "Wrap":
			return &Wrap{Container: c}
		case "Grid":
			return &Grid{Container: c, Columns: newProp(nil, "columns", DirtyLayout, 2.0)}
		default:
			return &Stack{Container: c}
		}
	case "Rectangle":
		return &Rectangle{NodeBase: b()}
	case "Image":
		return &Image{
			NodeBase: b(),
			Src:      newProp(nil, "src", DirtyLayout, ""),
			Fit:      newProp(nil, "fit", DirtyLayout, "cover"),
			Paused:   newProp(nil, "paused", DirtyPaint, false),
			Loop:     newProp(nil, "loop", DirtyPaint, true),
		}
	case "SVG":
		return &SVG{
			NodeBase: b(),
			Src:      newProp(nil, "src", DirtyLayout, ""),
			Fit:      newProp(nil, "fit", DirtyLayout, "contain"),
			Color:    newProp(nil, "color", DirtyPaint, uint32(0)),
			Paused:   newProp(nil, "paused", DirtyPaint, false),
			Loop:     newProp(nil, "loop", DirtyPaint, true),
		}
	case "Positioned":
		return &Positioned{
			NodeBase:   b(),
			Left:       newProp(nil, "left", DirtyLayout, -1.0),
			Top:        newProp(nil, "top", DirtyLayout, -1.0),
			Right:      newProp(nil, "right", DirtyLayout, -1.0),
			Bottom:     newProp(nil, "bottom", DirtyLayout, -1.0),
			TranslateX: newProp(nil, "translateX", DirtyLayout, 0.0),
			TranslateY: newProp(nil, "translateY", DirtyLayout, 0.0),
		}
	case "Text":
		return &Text{
			NodeBase:            b(),
			Text:                newProp(nil, "text", DirtyLayout, ""),
			FontSize:            newProp(nil, "fontSize", DirtyLayout, 16.0),
			Color:               newProp(nil, "color", DirtyPaint, uint32(0x000000FF)),
			Align:               newProp(nil, "align", DirtyLayout, "start"),
			Wrap:                newProp(nil, "wrap", DirtyLayout, false),
			Role:                newProp(nil, "role", DirtyLayout, "bodyMedium"),
			Weight:              newProp(nil, "weight", DirtyLayout, 0.0),
			FontStyle:           newProp(nil, "fontStyle", DirtyPaint, "normal"),
			Decoration:          newProp(nil, "decoration", DirtyPaint, "none"),
			DecorationStyle:     newProp(nil, "decorationStyle", DirtyPaint, "solid"),
			DecorationThickness: newProp(nil, "decorationThickness", DirtyPaint, 0.0),
			DecorationColor:     newProp(nil, "decorationColor", DirtyPaint, uint32(0)),
			Selectable:          newProp(nil, "selectable", DirtyPaint, false),
		}
	}
	if c := newM3Component(name); c != nil {
		return c
	}
	return nil
}

// newM3Component constructs any Material 3 component by element name.
func newM3Component(name string) Node {
	switch name {
	case "Button":
		return &Button{Component: newComponent()}
	case "IconButton":
		return &IconButton{Component: newComponent()}
	case "FAB":
		return &FAB{Component: newComponent()}
	case "FABMenu":
		return &FABMenu{Component: newComponent()}
	case "ButtonGroup":
		return &ButtonGroup{Component: newComponent()}
	case "SplitButton":
		return &SplitButton{Component: newComponent()}
	case "SegmentedButton":
		return &SegmentedButton{Component: newComponent()}
	case "Chip":
		return &Chip{Component: newComponent()}
	case "Badge":
		return &Badge{Component: newComponent()}
	case "Checkbox":
		return &Checkbox{Component: newComponent()}
	case "RadioButton":
		return &RadioButton{Component: newComponent()}
	case "Switch":
		return &Switch{Component: newComponent()}
	case "Slider":
		return &Slider{
			Component: newComponent(),
			End:       newProp(nil, "end", DirtyPaint, 0.0),
			Center:    newProp(nil, "center", DirtyPaint, math.NaN()),
		}
	case "TextField":
		ti := &TextField{Component: newComponent()}
		ti.Max.Set(1)
		return ti
	case "SearchBar":
		return &SearchBar{Component: newComponent()}
	case "Tooltip":
		return &Tooltip{Component: newComponent()}
	case "Card":
		return &Card{Component: newComponent()}
	case "Dialog":
		return &Dialog{Component: newComponent()}
	case "BottomSheet":
		return &BottomSheet{Component: newComponent()}
	case "SideSheet":
		return &SideSheet{Component: newComponent()}
	case "Divider":
		return &Divider{Component: newComponent()}
	case "List":
		return &List{Component: newComponent()}
	case "ListItem":
		return &ListItem{Component: newComponent()}
	case "Table":
		return &Table{
			Component:     newComponent(),
			Striped:       newProp(nil, "striped", DirtyPaint, false),
			ShowHeader:    newProp(nil, "showHeader", DirtyLayout, true),
			RowHeight:     newProp(nil, "rowHeight", DirtyLayout, DefaultRowHeight),
			HeaderHeight:  newProp(nil, "headerHeight", DirtyLayout, DefaultHeaderHeight),
			SortColumn:    newProp(nil, "sortColumn", DirtyLayout, -1.0),
			SortAscending: newProp(nil, "sortAscending", DirtyLayout, true),
			Selectable:    newProp(nil, "selectable", DirtyPaint, "none"),
			SelectedRow:   newProp(nil, "selectedRow", DirtyPaint, -1.0),
		}
	case "TableColumn":
		return &TableColumn{
			Component: newComponent(),
			Key:       newProp(nil, "key", DirtyPaint, ""),
			Align:     newProp(nil, "align", DirtyPaint, ""),
			Sortable:  newProp(nil, "sortable", DirtyPaint, false),
			Numeric:   newProp(nil, "numeric", DirtyPaint, false),
		}
	case "TableRow":
		return &TableRow{Component: newComponent()}
	case "TableCell":
		return &TableCell{
			Component: newComponent(),
			Value:     newProp(nil, "value", DirtyLayout, ""),
			Align:     newProp(nil, "align", DirtyPaint, ""),
		}
	case "Menu":
		return &Menu{Component: newComponent()}
	case "MenuItem":
		return &MenuItem{Component: newComponent()}
	case "Snackbar":
		return &Snackbar{Component: newComponent()}
	case "TopAppBar":
		return &TopAppBar{Component: newComponent()}
	case "NavigationBar":
		return &NavigationBar{Component: newComponent()}
	case "NavigationRail":
		return &NavigationRail{Component: newComponent()}
	case "NavigationDrawer":
		return &NavigationDrawer{Component: newComponent()}
	case "NavigationItem":
		return &NavigationItem{Component: newComponent()}
	case "Tabs":
		return &Tabs{Component: newComponent()}
	case "Tab":
		return &Tab{Component: newComponent()}
	case "LinearProgress":
		return &LinearProgress{Component: newComponent()}
	case "CircularProgress":
		return &CircularProgress{Component: newComponent()}
	case "LoadingIndicator":
		return &LoadingIndicator{Component: newComponent()}
	case "DatePicker":
		return &DatePicker{Component: newComponent()}
	case "TimePicker":
		return &TimePicker{Component: newComponent()}
	case "Carousel":
		return &Carousel{Component: newComponent()}
	case "Toolbar":
		return &Toolbar{Component: newComponent()}
	case "TextArea":
		return &TextArea{Component: newComponent()}
	case "Code":
		return &Code{Component: newComponent()}
	}
	return nil
}
