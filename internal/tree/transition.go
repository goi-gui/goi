package tree

import (
	"time"

	"github.com/goi-gui/goi/internal/csscolor"
	"github.com/goi-gui/goi/internal/syntax"
)

// transitionDef is one property's duration.
type transitionDef struct {
	dur time.Duration
}

// transitionState is the live interpolation for one property. from and to are
// the numeric endpoints; colors use the two uint32 fields. A state starts
// uninitialized so the first value a frame assigns is written directly rather
// than animated from the property's default.
type transitionState struct {
	dur         time.Duration
	elapsed     time.Duration
	from, to    float64
	fromColor   uint32
	toColor     uint32
	fromRot     Rotation
	toRot       Rotation
	fromScale   Scale
	toScale     Scale
	fromShadow  []ShadowLayer
	toShadow    []ShadowLayer
	active      bool
	initialized bool
	color       bool
	rot         bool
	scale       bool
	shadow      bool
}

// setTransitions reads a `transition:` value. Every named property must exist;
// `all` is a wildcard for properties not named explicitly.
func (b *NodeBase) setTransitions(t *syntax.Transition) bool {
	if t == nil || len(t.Entries) == 0 {
		b.transitions = nil
		b.transitionStates = nil
		return true
	}
	b.properties()
	b.transitions = make(map[string]transitionDef, len(t.Entries))
	for _, e := range t.Entries {
		if e.Property != "all" {
			if _, ok := b.byName[e.Property]; !ok {
				return false
			}
		}
		b.transitions[e.Property] = transitionDef{dur: e.Duration}
	}
	b.transitionStates = map[string]*transitionState{}
	for name := range b.transitions {
		if name != "all" {
			b.initTransition(name)
		}
	}
	return true
}

// transitionFor returns the specific transition, then the `all` fallback.
func (b *NodeBase) transitionFor(name string) (transitionDef, bool) {
	if d, ok := b.transitions[name]; ok {
		return d, true
	}
	d, ok := b.transitions["all"]
	return d, ok
}

func (b *NodeBase) initTransition(name string) {
	d, ok := b.transitionFor(name)
	if !ok {
		return
	}
	b.transitionStates[name] = &transitionState{dur: d.dur}
}

// initTransitionCurrent records the property's current value as the start of a
// transition that has not run yet. A conditional fallback calls it when the
// default is already on the cell, so the next arm animates from that default.
func (b *NodeBase) initTransitionCurrent(name string) {
	st, ok := b.transitionStateFor(name)
	if !ok || st.initialized {
		return
	}
	b.properties()
	i, ok := b.byName[name]
	if !ok {
		return
	}
	switch b.props[i].kind {
	case propNumber:
		v := b.props[i].Get().(float64)
		st.from, st.to = v, v
		st.initialized = true
	case propColor:
		v := b.props[i].Get().(uint32)
		st.fromColor, st.toColor = v, v
		st.color = true
		st.initialized = true
	case propRotate:
		v := b.props[i].Get().(Rotation)
		st.fromRot, st.toRot = v, v
		st.rot = true
		st.initialized = true
	case propScale:
		v := b.props[i].Get().(Scale)
		st.fromScale, st.toScale = v, v
		st.scale = true
		st.initialized = true
	case propShadow:
		v := append([]ShadowLayer(nil), b.shadow.layers...)
		st.fromShadow, st.toShadow = v, append([]ShadowLayer(nil), v...)
		st.shadow = true
		st.initialized = true
	}
}

func (b *NodeBase) transitionStateFor(name string) (*transitionState, bool) {
	if b.transitions == nil {
		return nil, false
	}
	if st, ok := b.transitionStates[name]; ok {
		return st, true
	}
	d, ok := b.transitionFor(name)
	if !ok {
		return nil, false
	}
	st := &transitionState{dur: d.dur}
	b.transitionStates[name] = st
	return st, true
}

// applyTransitionLiteral intercepts a literal property write. It returns false
// for a role, a gradient or a property the transition does not name.
func (b *NodeBase) applyTransitionLiteral(name string, v syntax.Value) bool {
	st, ok := b.transitionStateFor(name)
	if !ok {
		return false
	}
	b.properties()
	i, ok := b.byName[name]
	if !ok {
		return false
	}
	switch b.props[i].kind {
	case propNumber:
		n, ok := v.(*syntax.Number)
		if !ok {
			return false
		}
		b.startNumber(name, st, n.Val)
		return true
	case propColor:
		var rgba uint32
		switch c := v.(type) {
		case *syntax.Color:
			rgba = c.RGBA
		case *syntax.String:
			if csscolor.IsGradient(c.Val) {
				return false
			}
			var ok bool
			rgba, ok = csscolor.Parse(c.Val)
			if !ok {
				return false
			}
		case *syntax.Ident:
			var ok bool
			rgba, ok = csscolor.Parse(c.Val)
			if !ok {
				return false
			}
		default:
			return false
		}
		b.startColor(name, st, rgba, true)
		return true
	case propRotate:
		r, ok := v.(*syntax.Rotate)
		if !ok {
			return false
		}
		b.startRotation(name, st, rotationOf(r))
		return true
	case propScale:
		sc, ok := v.(*syntax.Scale)
		if !ok {
			return false
		}
		b.startScale(name, st, scaleOf(sc))
		return true
	case propShadow:
		sh, ok := v.(*syntax.Shadow)
		if !ok {
			return false
		}
		b.startShadow(name, st, shadowLayers(sh))
		return true
	}
	return false
}

// startGoTransition turns a direct Prop.Set into a transition target. The
// first Go write starts from the value the cell holds now; a write before any
// frame does not animate in from a default.
func (b *NodeBase) startGoTransition(name string, v any) bool {
	st, ok := b.transitionStateFor(name)
	if !ok {
		return false
	}
	b.initTransitionCurrent(name)
	switch x := v.(type) {
	case float64:
		b.startNumber(name, st, x)
		return true
	case uint32:
		b.startColor(name, st, x, true)
		return true
	case Rotation:
		b.startRotation(name, st, x)
		return true
	case Scale:
		b.startScale(name, st, x)
		return true
	case []ShadowLayer:
		b.startShadow(name, st, x)
		return true
	}
	return false
}

func (b *NodeBase) startGoShadow(layers []ShadowLayer) bool {
	st, ok := b.transitionStateFor("shadow")
	if !ok {
		return false
	}
	b.initTransitionCurrent("shadow")
	b.startShadow("shadow", st, layers)
	return true
}

// writeTransitionExpr intercepts a resolved expression value.
func (b *NodeBase) writeTransitionExpr(name string, v any) bool {
	st, ok := b.transitionStateFor(name)
	if !ok {
		return false
	}
	switch x := v.(type) {
	case float64:
		b.startNumber(name, st, x)
		return true
	case uint32:
		b.startColor(name, st, x, true)
		return true
	case Rotation:
		b.startRotation(name, st, x)
		return true
	case Scale:
		b.startScale(name, st, x)
		return true
	case []ShadowLayer:
		b.startShadow(name, st, x)
		return true
	}
	return false
}

func (b *NodeBase) startNumber(name string, st *transitionState, to float64) {
	if !st.initialized {
		if b.tree != nil && b.tree.building {
			b.writeRawNumber(name, to)
			return
		}
		st.initialized = true
		st.from, st.to = to, to
		b.writeRawNumber(name, to)
		return
	}
	if st.active && st.to == to {
		return
	}
	if !st.active && st.to == to {
		return
	}
	from, _ := b.PropValue(name)
	st.from = from.(float64)
	st.to = to
	st.elapsed = 0
	st.active = true
	b.listTransition()
	b.mark(b.PropDirty(name))
}

func (b *NodeBase) startColor(name string, st *transitionState, to uint32, clearRole bool) {
	if clearRole {
		b.clearColorRole(name)
		b.setGradient(name, nil)
	}
	if !st.initialized {
		if b.tree != nil && b.tree.building {
			b.writeColorRaw(name, to)
			return
		}
		st.initialized = true
		st.color = true
		st.fromColor, st.toColor = to, to
		b.writeColorRaw(name, to)
		return
	}
	if st.active && st.toColor == to {
		return
	}
	if !st.active && st.toColor == to {
		return
	}
	cur, _ := b.PropValue(name)
	st.color = true
	st.fromColor = cur.(uint32)
	st.toColor = to
	st.elapsed = 0
	st.active = true
	b.listTransition()
	b.mark(b.PropDirty(name))
}

func (b *NodeBase) startRotation(name string, st *transitionState, to Rotation) {
	if !st.initialized {
		if b.tree != nil && b.tree.building {
			b.writeRawRotation(name, to)
			return
		}
		st.initialized = true
		st.rot = true
		st.fromRot, st.toRot = to, to
		b.writeRawRotation(name, to)
		return
	}
	if st.rot && st.toRot == to {
		return
	}
	cur, _ := b.PropValue(name)
	st.rot = true
	st.fromRot = cur.(Rotation)
	st.toRot = to
	st.elapsed = 0
	st.active = true
	b.listTransition()
	b.mark(b.PropDirty(name))
}

func (b *NodeBase) startScale(name string, st *transitionState, to Scale) {
	if !st.initialized {
		if b.tree != nil && b.tree.building {
			b.writeRawScale(name, to)
			return
		}
		st.initialized = true
		st.scale = true
		st.fromScale, st.toScale = to, to
		b.writeRawScale(name, to)
		return
	}
	if st.scale && st.toScale == to {
		return
	}
	cur, _ := b.PropValue(name)
	st.scale = true
	st.fromScale = cur.(Scale)
	st.toScale = to
	st.elapsed = 0
	st.active = true
	b.listTransition()
	b.mark(b.PropDirty(name))
}

func (b *NodeBase) startShadow(name string, st *transitionState, to []ShadowLayer) {
	for _, l := range to {
		if l.Role != "" {
			b.listShadow()
			break
		}
	}
	if !st.initialized {
		if b.tree != nil && b.tree.building {
			b.writeRawShadow(to)
			return
		}
		st.initialized = true
		st.shadow = true
		st.fromShadow = append([]ShadowLayer(nil), to...)
		st.toShadow = append([]ShadowLayer(nil), to...)
		b.writeRawShadow(to)
		return
	}
	if st.shadow && shadowEqual(st.toShadow, to) {
		return
	}
	st.shadow = true
	st.fromShadow = append([]ShadowLayer(nil), b.shadow.layers...)
	st.toShadow = append([]ShadowLayer(nil), to...)
	st.elapsed = 0
	st.active = true
	b.listTransition()
	b.mark(b.PropDirty(name))
}

func (b *NodeBase) writeRawRotation(name string, r Rotation) {
	b.properties()
	if i, ok := b.byName[name]; ok {
		b.props[i].SetDefault(&syntax.Rotate{Angle: r.Angle * 180 / 3.141592653589793, Axis: axisName(r.Axis), Vector: r.Axis})
	}
}

func (b *NodeBase) writeRawScale(name string, sc Scale) {
	b.properties()
	if i, ok := b.byName[name]; ok {
		b.props[i].SetDefault(&syntax.Scale{X: sc.X, Y: sc.Y, Z: sc.Z})
	}
}

func (b *NodeBase) writeRawShadow(layers []ShadowLayer) {
	b.shadow.set(layers)
	b.shadow.explicit = true
}

func axisName(a [3]float64) string {
	switch {
	case a == [3]float64{1, 0, 0}:
		return "x"
	case a == [3]float64{0, 1, 0}:
		return "y"
	}
	return ""
}

func shadowEqual(a, b []ShadowLayer) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (b *NodeBase) writeRawNumber(name string, v float64) {
	b.properties()
	if i, ok := b.byName[name]; ok {
		b.props[i].SetDefault(&syntax.Number{Val: v})
	}
}

func (b *NodeBase) writeColorRaw(name string, rgba uint32) bool {
	b.properties()
	if i, ok := b.byName[name]; ok {
		b.props[i].SetColor(rgba)
		return true
	}
	return false
}

func (b *NodeBase) listTransition() {
	if b.transitionListed || b.tree == nil {
		return
	}
	b.transitionListed = true
	b.tree.transitionNodes = append(b.tree.transitionNodes, b)
}

// transitionColor animates a resolved role colour. It keeps the role spec, so a
// theme change can retarget it after the animation ends.
func (b *NodeBase) transitionColor(name string, rgba uint32) bool {
	st, ok := b.transitionStateFor(name)
	if !ok {
		return false
	}
	b.properties()
	i, ok := b.byName[name]
	if !ok || b.props[i].kind != propColor {
		return false
	}
	b.startColor(name, st, rgba, false)
	return true
}

// AdvanceTransitions moves every live transition one frame forward and reports
// whether one is still running. dt is in seconds.
func (t *Tree) AdvanceTransitions(dt float64) bool {
	if dt <= 0 || len(t.transitionNodes) == 0 {
		return false
	}
	elapsed := time.Duration(dt * float64(time.Second))
	changed := false
	for _, b := range t.transitionNodes {
		for name, st := range b.transitionStates {
			if !st.active {
				continue
			}
			st.elapsed += elapsed
			p := 1.0
			if st.dur > 0 {
				p = float64(st.elapsed) / float64(st.dur)
			}
			if p > 1 {
				p = 1
			}
			e := 1 - (1-p)*(1-p)*(1-p)
			switch {
			case st.color:
				b.writeColorRaw(name, mixColor(st.fromColor, st.toColor, e))
			case st.rot:
				b.writeRawRotation(name, mixRotation(st.fromRot, st.toRot, e))
			case st.scale:
				b.writeRawScale(name, mixScale(st.fromScale, st.toScale, e))
			case st.shadow:
				b.writeRawShadow(mixShadow(st.fromShadow, st.toShadow, e))
			default:
				b.writeRawNumber(name, st.from+(st.to-st.from)*e)
			}
			if p >= 1 {
				switch {
				case st.rot:
					b.writeRawRotation(name, st.toRot)
				case st.scale:
					b.writeRawScale(name, st.toScale)
				case st.shadow:
					b.writeRawShadow(st.toShadow)
				}
			}
			b.mark(b.PropDirty(name))
			changed = true
			if p >= 1 {
				st.active = false
				st.elapsed = 0
			}
		}
	}
	return changed
}

func mixRotation(a, b Rotation, t float64) Rotation {
	return Rotation{
		Angle: a.Angle + (b.Angle-a.Angle)*t,
		Axis: [3]float64{
			a.Axis[0] + (b.Axis[0]-a.Axis[0])*t,
			a.Axis[1] + (b.Axis[1]-a.Axis[1])*t,
			a.Axis[2] + (b.Axis[2]-a.Axis[2])*t,
		},
	}
}

func mixScale(a, b Scale, t float64) Scale {
	return Scale{
		X:   a.X + (b.X-a.X)*t,
		Y:   a.Y + (b.Y-a.Y)*t,
		Z:   a.Z + (b.Z-a.Z)*t,
		Set: true,
	}
}

func mixShadow(a, b []ShadowLayer, t float64) []ShadowLayer {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	out := make([]ShadowLayer, n)
	for i := range out {
		var x, y ShadowLayer
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		out[i] = ShadowLayer{
			X:      x.X + (y.X-x.X)*t,
			Y:      x.Y + (y.Y-x.Y)*t,
			Blur:   x.Blur + (y.Blur-x.Blur)*t,
			Spread: x.Spread + (y.Spread-x.Spread)*t,
			Color:  mixColor(x.Color, y.Color, t),
			Inset:  y.Inset,
			Role:   y.Role,
		}
	}
	return out
}

func mixColor(a, b uint32, t float64) uint32 {
	mix := func(x, y uint32) uint32 {
		return uint32(float64(x) + (float64(y)-float64(x))*t + 0.5)
	}
	return mix((a>>24)&0xff, (b>>24)&0xff)<<24 |
		mix((a>>16)&0xff, (b>>16)&0xff)<<16 |
		mix((a>>8)&0xff, (b>>8)&0xff)<<8 |
		mix(a&0xff, b&0xff)
}
