package csscolor

import "testing"

// TestParseNamedColors checks the CSS keyword list, including the spellings that
// only differ by a synonym.
func TestParseNamedColors(t *testing.T) {
	cases := map[string]uint32{
		"white":         0xFFFFFFFF,
		"black":         0x000000FF,
		"red":           0xFF0000FF,
		"green":         0x008000FF,
		"lime":          0x00FF00FF,
		"navy":          0x000080FF,
		"rebeccapurple": 0x663399FF,
		"grey":          0x808080FF,
		"gray":          0x808080FF,
		"transparent":   0x00000000,
	}
	for name, want := range cases {
		got, ok := Parse(name)
		if !ok || got != want {
			t.Errorf("%s = %08x ok=%v, want %08x", name, got, ok, want)
		}
	}
	// Case and padding are the design's business, not the parser's.
	if got, ok := Parse("  ReD "); !ok || got != 0xFF0000FF {
		t.Errorf("padded mixed case = %08x ok=%v", got, ok)
	}
	if _, ok := Parse("notacolour"); ok {
		t.Error("an unknown keyword parsed")
	}
}

// TestParseHexForms covers #rgb, #rgba, #rrggbb and #rrggbbaa.
func TestParseHexForms(t *testing.T) {
	cases := map[string]uint32{
		"#fff":      0xFFFFFFFF,
		"#f00":      0xFF0000FF,
		"#38f":      0x3388FFFF,
		"#38f8":     0x3388FF88,
		"#3b82f6":   0x3B82F6FF,
		"#00000000": 0x00000000,
	}
	for in, want := range cases {
		if got, ok := Parse(in); !ok || got != want {
			t.Errorf("%s = %08x ok=%v, want %08x", in, got, ok, want)
		}
	}
}

// TestParseRGBAndHSL checks both function forms, legacy commas and the modern
// space-with-slash spelling.
func TestParseRGBAndHSL(t *testing.T) {
	cases := map[string]uint32{
		"rgb(42, 123, 155)":       0x2A7B9BFF,
		"rgba(42, 123, 155, 1)":   0x2A7B9BFF,
		"rgba(42, 123, 155, 0.5)": 0x2A7B9B80,
		"rgb(42 123 155 / 50%)":   0x2A7B9B80,
		"rgb(100% 0% 0%)":         0xFF0000FF,
		"hsl(0 100% 50%)":         0xFF0000FF,
		"hsl(120, 100%, 25%)":     0x008000FF,
	}
	for in, want := range cases {
		got, ok := Parse(in)
		if !ok || got != want {
			t.Errorf("%s = %08x ok=%v, want %08x", in, got, ok, want)
		}
	}
	if _, ok := Parse("rgb(1,2)"); ok {
		t.Error("a two-channel rgb() parsed")
	}
}

// TestParseGradientCoversCSSForms checks angle and direction keywords, stops
// with and without positions, and both gradient kinds.
func TestParseGradientCoversCSSForms(t *testing.T) {
	g, ok := ParseGradient("linear-gradient(90deg,rgba(42, 123, 155, 1) 0%, rgba(87, 199, 133, 1) 50%, rgba(237, 221, 83, 1) 100%)")
	if !ok || g.Kind != Linear || g.Angle != 90 || len(g.Stops) != 3 {
		t.Fatalf("linear = %+v ok=%v", g, ok)
	}
	if g.Stops[1].At != 0.5 || g.Stops[2].Color != 0xEDDD53FF {
		t.Fatalf("stops = %+v", g.Stops)
	}

	if g, ok := ParseGradient("linear-gradient(to right, red, blue)"); !ok || g.Angle != 90 {
		t.Fatalf("to right = %+v ok=%v", g, ok)
	}
	if g, ok := ParseGradient("linear-gradient(red, yellow, green)"); !ok || g.Angle != 0 || len(g.Stops) != 3 {
		t.Fatalf("default direction = %+v ok=%v", g, ok)
	} else if g.Stops[1].At != 0.5 {
		t.Fatalf("even spacing = %+v", g.Stops)
	}

	g, ok = ParseGradient("radial-gradient(circle, red 0%, blue 100%)")
	if !ok || g.Kind != Radial || len(g.Stops) != 2 {
		t.Fatalf("radial = %+v ok=%v", g, ok)
	}

	if _, ok := ParseGradient("linear-gradient(90deg)"); ok {
		t.Error("a one-stop gradient parsed")
	}
	if _, ok := ParseGradient("linear-gradient(90deg, notacolour, red)"); ok {
		t.Error("a gradient with an unknown colour parsed")
	}
}
