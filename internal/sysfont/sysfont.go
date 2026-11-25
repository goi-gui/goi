// Package sysfont finds the font families installed on the machine, so a goi
// design can name a system font in `fontFamily` without the app loading it.
//
// It reads only the sfnt tables it needs (name, OS/2, head and fvar presence),
// so indexing a font directory does not decode outlines. The scan is cached for
// the life of the process; a design that names no custom family never triggers
// it.
package sysfont

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
)

// File is one face found in a system font file.
type File struct {
	Path string
	// Index is the face index inside a TrueType/OpenType collection, 0 for a
	// single-font file.
	Index int
	// Family is the typographic family name (name ID 16), or the legacy family
	// name (ID 1) when the font has no typographic one.
	Family string
	// Weight is the OS/2 usWeightClass, or 400 when the font does not say.
	Weight int
	Italic bool
	// Variable is true when the face has an fvar table, so one file covers the
	// weights.
	Variable bool
}

// Index is one scan of a set of font directories, keyed by normalized family.
type Index struct {
	families map[string][]File
}

// Lookup returns the faces of family, matching case and spacing loosely.
func (idx *Index) Lookup(family string) []File {
	if idx == nil {
		return nil
	}
	return idx.families[normalize(family)]
}

var (
	platformOnce  sync.Once
	platformIndex *Index
)

// Fonts returns the installed faces of family. The first call scans the
// platform's font directories; later calls use the same index.
func Fonts(family string) []File {
	platformOnce.Do(func() {
		platformIndex = Scan(Dirs())
	})
	return platformIndex.Lookup(family)
}

// Scan indexes every font under dirs. A missing or unreadable file is skipped;
// a font with no family name is left out.
func Scan(dirs []string) *Index {
	idx := &Index{families: map[string][]File{}}
	seen := map[string]bool{}
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(filename string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !hasFontExt(filename) {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				info, err := os.Stat(filename)
				if err != nil || info.IsDir() {
					return nil
				}
			}
			if seen[filename] {
				return nil
			}
			seen[filename] = true
			files, err := readFont(filename)
			if err != nil {
				return nil
			}
			for _, file := range files {
				key := normalize(file.Family)
				idx.families[key] = append(idx.families[key], file)
			}
			return nil
		})
	}
	for key := range idx.families {
		files := idx.families[key]
		sort.Slice(files, func(i, j int) bool {
			if files[i].Path != files[j].Path {
				return files[i].Path < files[j].Path
			}
			return files[i].Index < files[j].Index
		})
		idx.families[key] = files
	}
	return idx
}

// Dirs returns the font directories of this platform.
func Dirs() []string {
	home, _ := os.UserHomeDir()
	var dirs []string
	switch runtime.GOOS {
	case "windows":
		if dir := os.Getenv("WINDIR"); dir != "" {
			dirs = append(dirs, filepath.Join(dir, "Fonts"))
		}
		if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
			dirs = append(dirs, filepath.Join(dir, "Microsoft", "Windows", "Fonts"))
		}
		if home != "" {
			dirs = append(dirs, filepath.Join(home, "AppData", "Local", "Microsoft", "Windows", "Fonts"))
		}
	case "darwin":
		dirs = append(dirs,
			"/System/Library/Fonts",
			"/System/Library/Fonts/Supplemental",
			"/Library/Fonts",
		)
		if home != "" {
			dirs = append(dirs, filepath.Join(home, "Library", "Fonts"))
		}
	default:
		dirs = append(dirs, "/usr/share/fonts", "/usr/local/share/fonts")
		if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
			dirs = append(dirs, filepath.Join(dir, "fonts"))
		}
		if home != "" {
			dirs = append(dirs,
				filepath.Join(home, ".local", "share", "fonts"),
				filepath.Join(home, ".fonts"),
			)
		}
	}
	out := dirs[:0]
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

func hasFontExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ttf", ".otf", ".ttc", ".otc":
		return true
	}
	return false
}

// normalize makes a family name comparable: case, repeated spaces and
// surrounding whitespace do not matter.
func normalize(family string) string {
	return strings.ToLower(strings.Join(strings.Fields(family), " "))
}

// readFont reads the metadata of every face in one font file.
func readFont(path string) ([]File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	r := &reader{f: f, size: info.Size()}
	head, err := r.at(0, 12)
	if err != nil {
		return nil, err
	}
	switch binary.BigEndian.Uint32(head[0:4]) {
	case 0x74746366: // ttcf
		count := int(binary.BigEndian.Uint32(head[8:12]))
		if count <= 0 || count > 4096 {
			return nil, errBadFont
		}
		offsets, err := r.at(12, count*4)
		if err != nil {
			return nil, err
		}
		var files []File
		for i := 0; i < count; i++ {
			off := int64(binary.BigEndian.Uint32(offsets[i*4 : i*4+4]))
			file, err := readFace(r, off, i)
			if err != nil {
				continue
			}
			file.Path = path
			files = append(files, file)
		}
		return files, nil
	case 0x00010000, 0x4f54544f, 0x74727565, 0x74797031: // 1.0, OTTO, true, typ1
		file, err := readFace(r, 0, 0)
		if err != nil {
			return nil, err
		}
		file.Path = path
		return []File{file}, nil
	}
	return nil, errBadFont
}

var errBadFont = io.ErrUnexpectedEOF

// reader reads ranges of one open font file.
type reader struct {
	f    *os.File
	size int64
}

func (r *reader) at(offset int64, length int) ([]byte, error) {
	if offset < 0 || length < 0 || offset > r.size || int64(length) > r.size-offset {
		return nil, errBadFont
	}
	b := make([]byte, length)
	if _, err := r.f.ReadAt(b, offset); err != nil {
		return nil, err
	}
	return b, nil
}

// readFace reads the tables of the face at offset.
func readFace(r *reader, offset int64, index int) (File, error) {
	header, err := r.at(offset, 12)
	if err != nil {
		return File{}, err
	}
	numTables := int(binary.BigEndian.Uint16(header[4:6]))
	if numTables <= 0 || numTables > 4096 {
		return File{}, errBadFont
	}
	dir, err := r.at(offset+12, numTables*16)
	if err != nil {
		return File{}, err
	}
	var name, os2, head *[2]int64
	variable := false
	for i := 0; i < numTables; i++ {
		record := dir[i*16 : i*16+16]
		tag := string(record[0:4])
		table := [2]int64{
			int64(binary.BigEndian.Uint32(record[8:12])),
			int64(binary.BigEndian.Uint32(record[12:16])),
		}
		switch tag {
		case "name":
			name = &table
		case "OS/2":
			os2 = &table
		case "head":
			head = &table
		case "fvar":
			variable = true
		}
	}
	if name == nil {
		return File{}, errBadFont
	}
	nameData, err := r.at(name[0], int(min(name[1], 1<<20)))
	if err != nil {
		return File{}, err
	}
	family, style := parseNames(nameData)
	if family == "" {
		return File{}, errBadFont
	}
	weight, italic := 400, false
	if os2 != nil {
		os2Data, err := r.at(os2[0], int(min(os2[1], 128)))
		if err == nil && len(os2Data) >= 64 {
			if w := int(binary.BigEndian.Uint16(os2Data[4:6])); w > 0 && w <= 1000 {
				weight = w
			}
			selection := binary.BigEndian.Uint16(os2Data[62:64])
			italic = selection&(1<<0) != 0 || selection&(1<<9) != 0
		}
	}
	if head != nil && !italic {
		headData, err := r.at(head[0], int(min(head[1], 54)))
		if err == nil && len(headData) >= 46 {
			italic = binary.BigEndian.Uint16(headData[44:46])&(1<<1) != 0
		}
	}
	if !italic {
		italic = strings.Contains(strings.ToLower(style), "italic") ||
			strings.Contains(strings.ToLower(style), "oblique")
	}
	if weight == 400 && strings.Contains(strings.ToLower(style), "bold") {
		weight = 700
	}
	return File{Index: index, Family: family, Weight: weight, Italic: italic, Variable: variable}, nil
}

type nameCandidate struct {
	text  string
	score int
}

// parseNames reads the family and style from a name table. A typographic name
// (ID 16/17) wins over the legacy family/subfamily pair (ID 1/2), and an
// English Windows record wins over the other encodings.
func parseNames(data []byte) (family, style string) {
	if len(data) < 6 {
		return "", ""
	}
	format := binary.BigEndian.Uint16(data[0:2])
	if format != 0 && format != 1 {
		return "", ""
	}
	count := int(binary.BigEndian.Uint16(data[2:4]))
	stringOffset := int(binary.BigEndian.Uint16(data[4:6]))
	if count <= 0 || 6+count*12 > len(data) {
		return "", ""
	}
	var typo, legacy, typoStyle, legacyStyle nameCandidate
	for i := 0; i < count; i++ {
		record := data[6+i*12 : 6+i*12+12]
		platform := binary.BigEndian.Uint16(record[0:2])
		language := binary.BigEndian.Uint16(record[4:6])
		nameID := binary.BigEndian.Uint16(record[6:8])
		length := int(binary.BigEndian.Uint16(record[8:10]))
		start := stringOffset + int(binary.BigEndian.Uint16(record[10:12]))
		if start < 0 || start > len(data) || length < 0 || start+length > len(data) {
			continue
		}
		text := decodeName(data[start:start+length], platform)
		if text == "" {
			continue
		}
		score := nameScore(platform, language)
		switch nameID {
		case 16:
			typo = better(typo, nameCandidate{text, score})
		case 1:
			legacy = better(legacy, nameCandidate{text, score})
		case 17:
			typoStyle = better(typoStyle, nameCandidate{text, score})
		case 2:
			legacyStyle = better(legacyStyle, nameCandidate{text, score})
		}
	}
	if typo.text != "" {
		family = typo.text
	} else {
		family = legacy.text
	}
	if typoStyle.text != "" {
		style = typoStyle.text
	} else {
		style = legacyStyle.text
	}
	return family, style
}

func better(current, candidate nameCandidate) nameCandidate {
	if candidate.text == "" {
		return current
	}
	if current.text == "" || candidate.score > current.score {
		return candidate
	}
	return current
}

// nameScore prefers Windows English names, then Windows and Unicode names,
// then the rest.
func nameScore(platform, language uint16) int {
	switch {
	case platform == 3 && language == 0x409:
		return 30
	case platform == 3:
		return 20
	case platform == 0:
		return 20
	case platform == 1 && language == 0:
		return 10
	case platform == 1:
		return 5
	}
	return 0
}

// decodeName decodes a name record. Windows and Unicode names are UTF-16BE;
// Macintosh names are treated as bytes, which covers the ASCII family names the
// platform uses.
func decodeName(b []byte, platform uint16) string {
	if platform == 0 || platform == 3 {
		if len(b)%2 != 0 {
			b = b[:len(b)-1]
		}
		units := make([]uint16, len(b)/2)
		for i := range units {
			units[i] = binary.BigEndian.Uint16(b[i*2 : i*2+2])
		}
		return strings.TrimSpace(strings.Trim(string(utf16.Decode(units)), "\x00"))
	}
	return strings.TrimSpace(strings.Trim(string(b), "\x00"))
}
