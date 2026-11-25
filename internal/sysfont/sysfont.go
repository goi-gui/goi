// Package sysfont finds the font families installed on the machine, so a goi
// design can name a system font in `fontFamily` without the app loading it.
//
// It reads only the sfnt tables it needs (name, OS/2, head and fvar presence),
// so indexing a font directory does not decode outlines. TrueType collections,
// WOFF files and old Mac resource-fork fonts (.dfont) are supported. The scan is
// cached for the life of the process; a design that names no custom family never
// triggers it. A family the platform's font matcher knows under another name is
// resolved through `fc-match` on Linux and the FontSubstitutes key on Windows;
// the generic CSS families map to the platform's usual face.
package sysfont

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/go-text/typesetting/font"
)

// File is one face found in a system font file.
type File struct {
	Path string
	// Index is the face index inside a TrueType/OpenType collection or the
	// sfnt resource inside a .dfont, 0 otherwise.
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

	mu      sync.Mutex
	aliases map[string][]File
}

// Lookup returns the faces of family, matching case and spacing loosely.
func (idx *Index) Lookup(family string) []File {
	if idx == nil {
		return nil
	}
	return idx.families[normalize(family)]
}

// Resolve returns the faces for a family the index does not carry, using the
// platform's font matching (fontconfig on Linux, the generic family map on
// macOS and Windows). A result is cached, including a miss.
func (idx *Index) Resolve(family string) []File {
	if idx == nil {
		return nil
	}
	key := normalize(family)
	idx.mu.Lock()
	files, ok := idx.aliases[key]
	idx.mu.Unlock()
	if ok {
		return files
	}
	files = idx.resolvePlatform(family)
	idx.mu.Lock()
	if idx.aliases == nil {
		idx.aliases = map[string][]File{}
	}
	idx.aliases[key] = files
	idx.mu.Unlock()
	return files
}

func (idx *Index) resolvePlatform(family string) []File {
	if runtime.GOOS == "linux" {
		if canonical, ok := fcMatchFamily(family); ok {
			if files := idx.Lookup(canonical); len(files) > 0 {
				return files
			}
		}
		if path, ok := fcMatchFile(family); ok {
			if files, err := readFont(path); err == nil {
				return files
			}
		}
	}
	if runtime.GOOS == "windows" {
		if substitute, ok := windowsSubstitute(family); ok {
			if files := idx.Lookup(substitute); len(files) > 0 {
				return files
			}
		}
	}
	if name := genericFamily(runtime.GOOS, family); name != "" {
		return idx.Lookup(name)
	}
	return nil
}

var (
	platformOnce  sync.Once
	platformIndex *Index
)

// Fonts returns the installed faces of family. The first call scans the
// platform's font directories; later calls use the same index. A family that is
// not installed by that name is resolved through the platform's alias rules.
func Fonts(family string) []File {
	platformOnce.Do(func() {
		platformIndex = Scan(Dirs())
	})
	if files := platformIndex.Lookup(family); len(files) > 0 {
		return files
	}
	return platformIndex.Resolve(family)
}

// FaceData is the sfnt data of one face and the index to pass to font.ParseTTC.
type FaceData struct {
	Data  []byte
	Index int
}

// ReadFaces returns the sfnt data of each file in order. The files of one path
// share a single read, so a collection is not loaded once per face. A .dfont
// resource is extracted (index 0); a collection keeps its face index. A face
// that cannot be read or extracted is left with nil Data.
func ReadFaces(files []File) []FaceData {
	out := make([]FaceData, len(files))
	type read struct {
		data []byte
		err  error
	}
	cache := map[string]read{}
	for i, file := range files {
		raw, ok := cache[file.Path]
		if !ok {
			raw.data, raw.err = os.ReadFile(file.Path)
			cache[file.Path] = raw
		}
		if raw.err != nil {
			continue
		}
		data, index := raw.data, file.Index
		if resources, ok := dfontResources(data); ok {
			if index < 0 || index >= len(resources) {
				continue
			}
			data, index = resources[index], 0
		}
		out[i] = FaceData{Data: data, Index: index}
	}
	return out
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
	case ".ttf", ".otf", ".ttc", ".otc", ".woff", ".dfont":
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
	r := &reader{at: f, size: info.Size()}
	head, err := r.read(0, 12)
	if err != nil {
		return nil, err
	}
	tag := binary.BigEndian.Uint32(head[0:4])
	switch tag {
	case 0x74746366: // ttcf
		count := int(binary.BigEndian.Uint32(head[8:12]))
		if count <= 0 || count > 4096 {
			return nil, errBadFont
		}
		offsets, err := r.read(12, count*4)
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
	case 0x774f4646: // wOFF
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		parsed, err := font.ParseTTF(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		desc := parsed.Describe()
		family := strings.TrimSpace(desc.Family)
		if family == "" {
			return nil, errBadFont
		}
		weight := int(desc.Aspect.Weight + 0.5)
		if weight <= 0 {
			weight = 400
		}
		return []File{{
			Path:   path,
			Family: family,
			Weight: weight,
			Italic: desc.Aspect.Style == font.StyleItalic,
		}}, nil
	case 0x00010000, 0x4f54544f, 0x74727565, 0x74797031: // 1.0, OTTO, true, typ1
		file, err := readFace(r, 0, 0)
		if err != nil {
			return nil, err
		}
		file.Path = path
		return []File{file}, nil
	}
	// A .dfont is a resource fork, not an sfnt file.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	resources, ok := dfontResources(data)
	if !ok {
		return nil, errBadFont
	}
	var files []File
	for i, resource := range resources {
		file, err := readFaceBytes(resource, i)
		if err != nil {
			continue
		}
		file.Path = path
		files = append(files, file)
	}
	return files, nil
}

var errBadFont = io.ErrUnexpectedEOF

// reader reads ranges of one open font file or of an in-memory face.
type reader struct {
	at   io.ReaderAt
	size int64
}

func (r *reader) read(offset int64, length int) ([]byte, error) {
	if offset < 0 || length < 0 || offset > r.size || int64(length) > r.size-offset {
		return nil, errBadFont
	}
	b := make([]byte, length)
	if _, err := r.at.ReadAt(b, offset); err != nil {
		return nil, err
	}
	return b, nil
}

// readFaceBytes reads a single sfnt face held in memory.
func readFaceBytes(data []byte, index int) (File, error) {
	return readFace(&reader{at: bytes.NewReader(data), size: int64(len(data))}, 0, index)
}

// readFace reads the tables of the face at offset.
func readFace(r *reader, offset int64, index int) (File, error) {
	header, err := r.read(offset, 12)
	if err != nil {
		return File{}, err
	}
	numTables := int(binary.BigEndian.Uint16(header[4:6]))
	if numTables <= 0 || numTables > 4096 {
		return File{}, errBadFont
	}
	dir, err := r.read(offset+12, numTables*16)
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
	nameData, err := r.read(name[0], int(min(name[1], 1<<20)))
	if err != nil {
		return File{}, err
	}
	family, style := parseNames(nameData)
	if family == "" {
		return File{}, errBadFont
	}
	weight, italic := 400, false
	if os2 != nil {
		os2Data, err := r.read(os2[0], int(min(os2[1], 128)))
		if err == nil && len(os2Data) >= 64 {
			if w := int(binary.BigEndian.Uint16(os2Data[4:6])); w > 0 && w <= 1000 {
				weight = w
			}
			selection := binary.BigEndian.Uint16(os2Data[62:64])
			italic = selection&(1<<0) != 0 || selection&(1<<9) != 0
		}
	}
	if head != nil && !italic {
		headData, err := r.read(head[0], int(min(head[1], 54)))
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

// dfontResources returns the sfnt resources of a Mac resource-fork font.
func dfontResources(data []byte) ([][]byte, bool) {
	if len(data) < 16 {
		return nil, false
	}
	dataOffset := int(binary.BigEndian.Uint32(data[0:4]))
	mapOffset := int(binary.BigEndian.Uint32(data[4:8]))
	dataLength := int(binary.BigEndian.Uint32(data[8:12]))
	mapLength := int(binary.BigEndian.Uint32(data[12:16]))
	if dataOffset < 16 || mapOffset < 16 || dataLength < 0 || mapLength < 28 ||
		dataOffset+dataLength > len(data) || mapOffset+mapLength > len(data) {
		return nil, false
	}
	mapData := data[mapOffset : mapOffset+mapLength]
	typeListOffset := int(binary.BigEndian.Uint16(mapData[24:26]))
	typeList := mapOffset + typeListOffset
	if typeList+2 > mapOffset+mapLength {
		return nil, false
	}
	count := int(binary.BigEndian.Uint16(data[typeList:typeList+2])) + 1
	at := typeList + 2
	var out [][]byte
	for i := 0; i < count; i++ {
		if at+8 > mapOffset+mapLength {
			return nil, false
		}
		kind := string(data[at : at+4])
		resources := int(binary.BigEndian.Uint16(data[at+4:at+6])) + 1
		refList := typeList + int(binary.BigEndian.Uint16(data[at+6:at+8]))
		at += 8
		if kind != "sfnt" {
			continue
		}
		for j := 0; j < resources; j++ {
			if refList+12 > mapOffset+mapLength {
				return nil, false
			}
			ref := data[refList : refList+12]
			offset := int(ref[5])<<16 | int(ref[6])<<8 | int(ref[7])
			start := dataOffset + offset
			if start+4 > len(data) {
				return nil, false
			}
			length := int(binary.BigEndian.Uint32(data[start : start+4]))
			if length < 0 || start+4+length > len(data) {
				return nil, false
			}
			out = append(out, data[start+4:start+4+length])
			refList += 12
		}
	}
	return out, len(out) > 0
}

// fcMatchFamily asks fontconfig for the canonical family of a name, which is
// how aliases and generic names reach an installed family.
func fcMatchFamily(family string) (string, bool) {
	out, err := exec.Command("fc-match", "-f", "%{family}\n", family).Output()
	if err != nil {
		return "", false
	}
	line := strings.SplitN(string(out), "\n", 2)[0]
	if i := strings.IndexByte(line, ','); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	return line, line != ""
}

func fcMatchFile(family string) (string, bool) {
	out, err := exec.Command("fc-match", "-f", "%{file}", family).Output()
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(string(out))
	return line, line != ""
}

// windowsSubstitute is the DirectWrite rule Windows uses for a missing family
// (Helvetica is drawn as Arial, for example): the FontSubstitutes key maps the
// name to an installed family.
func windowsSubstitute(family string) (string, bool) {
	for _, key := range []string{
		`HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion\FontSubstitutes`,
		`HKCU\SOFTWARE\Microsoft\Windows NT\CurrentVersion\FontSubstitutes`,
	} {
		out, err := exec.Command("reg", "query", key, "/v", family).Output()
		if err != nil {
			continue
		}
		if value, ok := regString(out); ok {
			return value, true
		}
	}
	return "", false
}

// regString reads the data column of a `reg query` value line.
func regString(out []byte) (string, bool) {
	for _, line := range strings.Split(string(out), "\n") {
		i := strings.Index(line, "REG_SZ")
		if i < 0 {
			continue
		}
		if value := strings.TrimSpace(line[i+len("REG_SZ"):]); value != "" {
			return value, true
		}
	}
	return "", false
}

// genericFamily is the usual face for a CSS generic family on a platform whose
// font matcher has no generic aliases of its own (and the Linux fallback when
// fontconfig is unavailable).
func genericFamily(goos, family string) string {
	key := normalize(family)
	switch goos {
	case "linux":
		switch key {
		case "sans-serif", "sans", "system-ui", "ui":
			return "DejaVu Sans"
		case "serif":
			return "DejaVu Serif"
		case "monospace", "mono", "code":
			return "DejaVu Sans Mono"
		}
	case "darwin":
		switch key {
		case "sans-serif", "sans", "system-ui", "ui":
			return "Helvetica"
		case "serif":
			return "Times New Roman"
		case "monospace", "mono", "code":
			return "Menlo"
		case "cursive":
			return "Apple Chancery"
		case "fantasy":
			return "Papyrus"
		}
	case "windows":
		switch key {
		case "sans-serif", "sans", "system-ui", "ui":
			return "Segoe UI"
		case "serif":
			return "Times New Roman"
		case "monospace", "mono", "code":
			return "Consolas"
		case "cursive":
			return "Comic Sans MS"
		case "fantasy":
			return "Impact"
		}
	}
	return ""
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
