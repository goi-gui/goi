package tree

import (
	"fmt"

	"github.com/ksckaan1/goi/internal/schema"
	"github.com/ksckaan1/goi/internal/syntax"
)

// Tree is a live node tree plus its id index.
type Tree struct {
	Root *Window
	ids  map[string]Node
	// responsive lists the nodes that declared at least one breakpoint map.
	responsive []*NodeBase
	// exprNodes lists the nodes with at least one expression property.
	exprNodes []*NodeBase
	// colorNodes lists the nodes with at least one role-valued color.
	colorNodes []*NodeBase
	// viewW and viewH are the last viewport in logical pixels, so an expression
	// can read window.width and window.height.
	viewW, viewH float64
	// dark is the resolved scheme's mood, so `app.dark` is true for any
	// registered dark theme, not only one literally named "dark". darkSet marks
	// that the driver resolved it; before that a tree reads the name.
	dark    bool
	darkSet bool
	// cloning is set while Instantiate builds a copy: ids stay on the nodes so
	// an app can find them, but they are not registered, because a copy would
	// otherwise collide with the template's own ids.
	cloning bool
}

// ByID returns the node with the given id.
func (t *Tree) ByID(id string) (Node, bool) {
	n, ok := t.ids[id]
	return n, ok
}

// IDs returns the number of indexed ids.
func (t *Tree) IDs() int { return len(t.ids) }

// All returns every node, parents before children.
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

// Build instantiates a validated file. The schema defaults are applied first,
// then the literals from the file. hooks may be nil for headless tests.
func Build(f *syntax.File, hooks *Hooks) (*Tree, error) {
	if f == nil || f.Root == nil {
		return nil, fmt.Errorf("tree: no root element")
	}
	t := &Tree{ids: map[string]Node{}}
	root, err := t.build(f.Root, nil, hooks)
	if err != nil {
		return nil, err
	}
	w, ok := root.(*Window)
	if !ok {
		return nil, fmt.Errorf("tree: root must be Window, got %s", root.Kind())
	}
	t.Root = w
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
		if m, ok := p.Value.(*syntax.Map); ok {
			if !b.setResponsive(p.Name, m) {
				return nil, fmt.Errorf("%s.%s: invalid responsive value", el.Name, p.Name)
			}
			continue
		}
		if !b.applyValue(p.Name, p.Value) {
			return nil, fmt.Errorf("%s.%s: value not valid for the property type", el.Name, p.Name)
		}
	}
	if len(b.responsive) > 0 {
		t.responsive = append(t.responsive, b)
	}

	for _, c := range el.Children() {
		if _, err := t.build(c, b, hooks); err != nil {
			return nil, err
		}
	}
	return n, nil
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
			visible:       newProp(nil, "visible", DirtyLayout, true),
			margin:        newProp(nil, "margin", DirtyLayout, 0.0),
			padding:       newProp(nil, "padding", DirtyLayout, 0.0),
			marginTop:     newProp(nil, "marginTop", DirtyLayout, -1.0),
			marginRight:   newProp(nil, "marginRight", DirtyLayout, -1.0),
			marginBottom:  newProp(nil, "marginBottom", DirtyLayout, -1.0),
			marginLeft:    newProp(nil, "marginLeft", DirtyLayout, -1.0),
			paddingTop:    newProp(nil, "paddingTop", DirtyLayout, -1.0),
			paddingRight:  newProp(nil, "paddingRight", DirtyLayout, -1.0),
			paddingBottom: newProp(nil, "paddingBottom", DirtyLayout, -1.0),
			paddingLeft:   newProp(nil, "paddingLeft", DirtyLayout, -1.0),
			borderW:       newProp(nil, "borderWidth", DirtyLayout, 0.0),
			flex:          newProp(nil, "flex", DirtyLayout, 0.0),
			alignSelf:     newProp(nil, "alignSelf", DirtyLayout, ""),
			scroll:        newProp(nil, "scroll", DirtyPaint, false),
			width:         newProp(nil, "width", DirtyLayout, -1.0),
			height:        newProp(nil, "height", DirtyLayout, -1.0),
			minWidth:      newProp(nil, "minWidth", DirtyLayout, 0.0),
			maxWidth:      newProp(nil, "maxWidth", DirtyLayout, -1.0),
			background:    newProp(nil, "background", DirtyPaint, uint32(0)),
			borderColor:   newProp(nil, "borderColor", DirtyPaint, uint32(0)),
			radius:        newProp(nil, "radius", DirtyPaint, 0.0),
		}
	}
	switch name {
	case "Window":
		return &Window{
			NodeBase: b(),
			Title:    newProp(nil, "title", DirtyPaint, ""),
			Theme:    newProp(nil, "theme", DirtyPaint, "light"),
		}
	case "Column", "Row", "Stack", "Wrap", "Grid":
		c := Container{
			NodeBase: b(),
			Spacing:  newProp(nil, "spacing", DirtyLayout, 0.0),
			Align:    newProp(nil, "align", DirtyLayout, "start"),
		}
		switch name {
		case "Column":
			return &Column{Container: c}
		case "Row":
			return &Row{Container: c}
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
			NodeBase:   b(),
			Text:       newProp(nil, "text", DirtyLayout, ""),
			FontSize:   newProp(nil, "fontSize", DirtyLayout, 16.0),
			Color:      newProp(nil, "color", DirtyPaint, uint32(0x000000FF)),
			Align:      newProp(nil, "align", DirtyLayout, "start"),
			Wrap:       newProp(nil, "wrap", DirtyLayout, false),
			Role:       newProp(nil, "role", DirtyLayout, "bodyMedium"),
			Weight:     newProp(nil, "weight", DirtyLayout, 0.0),
			Selectable: newProp(nil, "selectable", DirtyPaint, false),
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
		return &Slider{Component: newComponent()}
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
	}
	return nil
}
