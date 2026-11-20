package csscolor

import "testing"

// TestGradientDPStops: a stop position above 1 is a dp length, while the old
// 0..1 fraction spelling and percentages still work.
func TestGradientDPStops(t *testing.T) {
	g, ok := ParseGradient("linear-gradient(90deg, red 0, blue 100, green 260)")
	if !ok {
		t.Fatal("not parsed")
	}
	if g.Stops[0].At != 0 || g.Stops[1].Len != 100 || g.Stops[2].Len != 260 {
		t.Fatalf("stops before resolve = %+v", g.Stops)
	}
	g.Resolve(260, 100)
	if g.Stops[1].At != 100.0/260 || g.Stops[2].At != 1 {
		t.Fatalf("resolved stops = %+v", g.Stops)
	}

	g, ok = ParseGradient("linear-gradient(90deg, red 0.5, blue 100dp)")
	if !ok || g.Stops[0].At != 0.5 || g.Stops[1].Len != 100 {
		t.Fatalf("fraction and dp stops = %+v", g.Stops)
	}
	if _, ok := ParseGradient("linear-gradient(90deg, red 0, blue 100, green)"); !ok {
		t.Fatal("a missing position should parse")
	}
}

// TestGradientRadialLengths uses the same radius the shader does: half the
// box diagonal.
func TestGradientRadialLengths(t *testing.T) {
	g, ok := ParseGradient("radial-gradient(circle, red 0%, blue 250)")
	if !ok {
		t.Fatal("not parsed")
	}
	g.Resolve(300, 400) // half the diagonal is 250
	if g.Stops[1].At != 1 {
		t.Fatalf("radial stop = %v, want 1", g.Stops[1].At)
	}
}

// TestValueGradientPercents reads the blur gradient from the documentation.
func TestValueGradientPercents(t *testing.T) {
	g, ok := ParseValueGradient("linear-gradient(90deg, 0 0%, 6 60%, 0 100%)")
	if !ok {
		t.Fatal("not parsed")
	}
	if len(g.Stops) != 3 || g.Stops[1].Value != 6 || g.Stops[1].At != 0.6 {
		t.Fatalf("stops = %+v", g.Stops)
	}
	if max := g.Max(); max != 6 {
		t.Fatalf("max = %v, want 6", max)
	}
	if g.Stops[0].At != 0 || g.Stops[2].At != 1 {
		t.Fatalf("positions = %+v", g.Stops)
	}
}

// TestValueGradientDPStops reads the dp form and resolves it against the box.
func TestValueGradientDPStops(t *testing.T) {
	g, ok := ParseValueGradient("linear-gradient(90deg, 0 0, 6 100, 0 260)")
	if !ok {
		t.Fatal("not parsed")
	}
	resolved := g.Resolve(260, 100)
	if resolved[1].At != 100.0/260 || resolved[2].At != 1 {
		t.Fatalf("resolved = %+v", resolved)
	}
	if _, ok := ParseValueGradient("radial-gradient(circle, 0 0%, 6 60%, 0 100%)"); !ok {
		t.Fatal("radial form did not parse")
	}
	if _, ok := ParseValueGradient("linear-gradient(90deg, 0, 6)"); !ok {
		t.Fatal("missing positions should parse")
	}
	if _, ok := ParseValueGradient("linear-gradient(90deg, -1 0%, 6 100%)"); ok {
		t.Fatal("a negative radius parsed")
	}
}
