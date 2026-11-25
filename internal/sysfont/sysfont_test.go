package sysfont

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

// nameTable builds a minimal name table from records.
func nameTable(t *testing.T, records []struct {
	platform uint16
	language uint16
	id       uint16
	text     string
}) []byte {
	t.Helper()
	var strings []byte
	type rec struct{ platform, language, id, offset, length uint16 }
	entries := make([]rec, len(records))
	for i, r := range records {
		var raw []byte
		if r.platform == 0 || r.platform == 3 {
			for _, u := range utf16.Encode([]rune(r.text)) {
				raw = binary.BigEndian.AppendUint16(raw, u)
			}
		} else {
			raw = []byte(r.text)
		}
		entries[i] = rec{r.platform, r.language, r.id, uint16(len(strings)), uint16(len(raw))}
		strings = append(strings, raw...)
	}
	out := make([]byte, 6+len(entries)*12, 6+len(entries)*12+len(strings))
	binary.BigEndian.PutUint16(out[0:2], 0)
	binary.BigEndian.PutUint16(out[2:4], uint16(len(entries)))
	binary.BigEndian.PutUint16(out[4:6], uint16(len(out)))
	for i, e := range entries {
		at := 6 + i*12
		binary.BigEndian.PutUint16(out[at:at+2], e.platform)
		binary.BigEndian.PutUint16(out[at+2:at+4], 1)
		binary.BigEndian.PutUint16(out[at+4:at+6], e.language)
		binary.BigEndian.PutUint16(out[at+6:at+8], e.id)
		binary.BigEndian.PutUint16(out[at+8:at+10], e.length)
		binary.BigEndian.PutUint16(out[at+10:at+12], e.offset)
	}
	return append(out, strings...)
}

func TestParseNamesPrefersTypographicEnglish(t *testing.T) {
	data := nameTable(t, []struct {
		platform uint16
		language uint16
		id       uint16
		text     string
	}{
		{1, 0, 1, "Legacy Family"},
		{1, 0, 2, "Bold"},
		{3, 0x409, 16, "Typographic Family"},
		{3, 0x409, 17, "Bold Italic"},
	})
	family, style := parseNames(data)
	if family != "Typographic Family" || style != "Bold Italic" {
		t.Fatalf("family, style = %q, %q", family, style)
	}
}

func TestParseNamesFallsBackToLegacy(t *testing.T) {
	data := nameTable(t, []struct {
		platform uint16
		language uint16
		id       uint16
		text     string
	}{{1, 0, 1, "Legacy Family"}, {1, 0, 2, "Regular"}})
	family, style := parseNames(data)
	if family != "Legacy Family" || style != "Regular" {
		t.Fatalf("family, style = %q, %q", family, style)
	}
}

func TestReadRealFont(t *testing.T) {
	path := filepath.Join("..", "text", "fonts", "roboto-mono.ttf")
	files, err := readFont(path)
	if err != nil {
		t.Skipf("the test font is unavailable: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("faces = %d, want 1", len(files))
	}
	file := files[0]
	if file.Family == "" || file.Weight != 400 || file.Index != 0 || file.Italic {
		t.Fatalf("face = %+v", file)
	}
}

// ttc builds a collection containing count copies of the single font in raw.
func ttc(t *testing.T, raw []byte, count int) []byte {
	t.Helper()
	numTables := int(binary.BigEndian.Uint16(raw[4:6]))
	start := 12 + count*4
	out := make([]byte, start+len(raw))
	copy(out[:4], "ttcf")
	binary.BigEndian.PutUint32(out[4:8], 0x00010000)
	binary.BigEndian.PutUint32(out[8:12], uint32(count))
	for i := 0; i < count; i++ {
		binary.BigEndian.PutUint32(out[12+i*4:16+i*4], uint32(start))
	}
	copy(out[start:], raw)
	// A TTC's table offsets are absolute from the start of the file.
	for i := 0; i < numTables; i++ {
		at := start + 12 + i*16 + 8
		offset := binary.BigEndian.Uint32(out[at : at+4])
		binary.BigEndian.PutUint32(out[at:at+4], offset+uint32(start))
	}
	return out
}

func TestReadTTCFaces(t *testing.T) {
	path := filepath.Join("..", "text", "fonts", "roboto-mono.ttf")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("the test font is unavailable: %v", err)
	}
	dir := t.TempDir()
	collection := filepath.Join(dir, "test.ttc")
	if err := os.WriteFile(collection, ttc(t, raw, 2), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := readFont(collection)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Index != 0 || files[1].Index != 1 {
		t.Fatalf("faces = %+v", files)
	}
	if files[0].Family != files[1].Family || files[0].Family == "" {
		t.Fatalf("families = %q, %q", files[0].Family, files[1].Family)
	}
}

func TestScanAndLookup(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "text", "fonts", "roboto-mono.ttf"))
	if err != nil {
		t.Skipf("the test font is unavailable: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "custom.TTF"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	idx := Scan([]string{dir})
	for _, name := range []string{"Roboto Mono", "roboto mono", "  ROBOTO   MONO  "} {
		if files := idx.Lookup(name); len(files) != 1 {
			t.Fatalf("Lookup(%q) = %+v", name, files)
		}
	}
	if files := idx.Lookup("No Such Family"); files != nil {
		t.Fatalf("Lookup of a missing family = %+v", files)
	}
}

func TestScanSkipsBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.ttf"), []byte("not a font"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := Scan([]string{dir})
	if len(idx.families) != 0 {
		t.Fatalf("broken file was indexed: %+v", idx.families)
	}
}
