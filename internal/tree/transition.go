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
	active      bool
	initialized bool
	color       bool
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
	st := &transitionState{dur: d.dur}
	if i, ok := b.byName[name]; ok && b.props[i].Written() {
		switch b.props[i].kind {
		case propNumber:
			st.from, st.to = b.props[i].Get().(float64), b.props[i].Get().(float64)
			st.initialized = true
		case propColor:
			st.fromColor, st.toColor = b.props[i].Get().(uint32), b.props[i].Get().(uint32)
			st.color = true
			st.initialized = true
		}
	}
	b.transitionStates[name] = st
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
	}
	return false
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
	}
	return false
}

func (b *NodeBase) startNumber(name string, st *transitionState, to float64) {
	if !st.initialized {
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
			if st.color {
				b.writeColorRaw(name, mixColor(st.fromColor, st.toColor, e))
			} else {
				b.writeRawNumber(name, st.from+(st.to-st.from)*e)
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

func mixColor(a, b uint32, t float64) uint32 {
	mix := func(x, y uint32) uint32 {
		return uint32(float64(x) + (float64(y)-float64(x))*t + 0.5)
	}
	return mix((a>>24)&0xff, (b>>24)&0xff)<<24 |
		mix((a>>16)&0xff, (b>>16)&0xff)<<16 |
		mix((a>>8)&0xff, (b>>8)&0xff)<<8 |
		mix(a&0xff, b&0xff)
}
