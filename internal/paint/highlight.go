package paint

import "strings"

// Code spans. The painter colors a span by its kind; a plain span is drawn in
// the normal text color so an unknown language still reads.
type codeKind uint8

const (
	codePlain codeKind = iota
	codeKeyword
	codeString
	codeComment
	codeNumber
)

// codeSpan is a highlighted byte range of the source.
type codeSpan struct {
	Start, End int
	Kind       codeKind
}

// langSyntax is the few facts a tokenizer needs per language. Only the popular
// ones are listed; anything else tokenizes as plain text.
type langSyntax struct {
	line     []string // starts a comment that runs to the end of the line
	block    [][2]string
	quotes   string // characters that open a string
	backtick bool   // ` opens a raw string
	keywords map[string]bool
}

var codeLangs = map[string]langSyntax{
	"go": {
		line:     []string{"//"},
		block:    [][2]string{{"/*", "*/"}},
		quotes:   `"'`,
		backtick: true,
		keywords: words("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var"),
	},
	"goi": {
		line:   []string{"//"},
		block:  [][2]string{{"/*", "*/"}},
		quotes: `"`,
		keywords: words("Window Column Row Stack Grid Wrap Positioned Rectangle Text Button IconButton " +
			"FAB FABMenu Chip Checkbox RadioButton Switch Slider TextField TextArea SearchBar Code " +
			"Card Dialog BottomSheet SideSheet Divider List ListItem Menu MenuItem Snackbar TopAppBar " +
			"NavigationBar NavigationRail NavigationDrawer NavigationItem Tabs Tab LinearProgress " +
			"CircularProgress LoadingIndicator DatePicker TimePicker Carousel Toolbar TitleBar " +
			"true false app"),
	},
	"js": {
		line:     []string{"//"},
		block:    [][2]string{{"/*", "*/"}},
		quotes:   `"'`,
		backtick: true,
		keywords: words("async await break case catch class const continue debugger default delete do else export extends finally for function if import in instanceof let new of return static super switch this throw try typeof var void while yield"),
	},
	"python": {
		line:     []string{"#"},
		quotes:   `"'`,
		keywords: words("and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield None True False self"),
	},
	"sh": {
		line:     []string{"#"},
		quotes:   `"'`,
		keywords: words("if then else elif fi for while do done case esac function return export local echo cd set unset"),
	},
	"json": {
		quotes:   `"`,
		keywords: words("true false null"),
	},
}

// langAlias maps the short names people actually write to a listed language.
var langAlias = map[string]string{
	"golang": "go", "py": "python", "python3": "python", "bash": "sh",
	"shell": "sh", "zsh": "sh", "js": "js", "javascript": "js",
	"typescript": "js", "ts": "js", "goi": "goi",
}

// words turns a space-separated list into a set.
func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// HighlightCode returns the spans that make src readable. Every byte of src is
// covered by exactly one span, in order, so a line can be drawn by walking them.
// It is exported so a test can check the coverage without a renderer.
func HighlightCode(src, lang string) []codeSpan {
	if alias, ok := langAlias[lang]; ok {
		lang = alias
	}
	syn, ok := codeLangs[lang]
	if !ok {
		return []codeSpan{{0, len(src), codePlain}}
	}
	var out []codeSpan
	emit := func(a, b int, k codeKind) {
		if b > a {
			out = append(out, codeSpan{a, b, k})
		}
	}
	plain := 0
	flush := func(at int) {
		emit(plain, at, codePlain)
	}
	for i := 0; i < len(src); {
		// Comment to the end of the line.
		if matchAny(src[i:], syn.line) != 0 {
			end := strings.IndexByte(src[i:], '\n')
			if end < 0 {
				end = len(src) - i
			}
			flush(i)
			emit(i, i+end, codeComment)
			i += end
			plain = i
			continue
		}
		// Block comment.
		if start, end, n := matchBlock(src, i, syn.block); n > 0 {
			flush(i)
			emit(start, end, codeComment)
			i = end
			plain = i
			continue
		}
		c := src[i]
		if strings.IndexByte(syn.quotes, c) >= 0 || (syn.backtick && c == '`') {
			end := scanString(src, i, c)
			flush(i)
			emit(i, end, codeString)
			i = end
			plain = i
			continue
		}
		if c >= '0' && c <= '9' {
			end := scanNumber(src, i)
			flush(i)
			emit(i, end, codeNumber)
			i = end
			plain = i
			continue
		}
		if isIdentStart(c) && (i == 0 || !isIdent(src[i-1])) {
			end := i
			for end < len(src) && isIdent(src[end]) {
				end++
			}
			if syn.keywords[src[i:end]] {
				flush(i)
				emit(i, end, codeKeyword)
				plain = end
			}
			i = end
			continue
		}
		i++
	}
	flush(len(src))
	return out
}

// matchAny reports the longest prefix of any of the markers.
func matchAny(s string, marks []string) int {
	for _, m := range marks {
		if strings.HasPrefix(s, m) {
			return len(m)
		}
	}
	return 0
}

// matchBlock finds a block comment at i and returns its bounds and the length of
// its opening marker (0 when there is none).
func matchBlock(src string, i int, blocks [][2]string) (int, int, int) {
	for _, b := range blocks {
		if !strings.HasPrefix(src[i:], b[0]) {
			continue
		}
		rest := src[i+len(b[0]):]
		idx := strings.Index(rest, b[1])
		if idx < 0 {
			return i, len(src), len(b[0])
		}
		return i, i + len(b[0]) + idx + len(b[1]), len(b[0])
	}
	return 0, 0, 0
}

// scanString returns the end of the string that starts at i. An unterminated
// string runs to the end of its line, which stops a stray quote from painting
// the rest of the file.
func scanString(src string, i int, quote byte) int {
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case quote:
			return j + 1
		case '\n':
			if quote != '`' {
				return j
			}
		}
	}
	return len(src)
}

// scanNumber returns the end of the numeric literal at i. A sign only continues
// a number after an exponent, so "1+2" is two numbers and not one.
func scanNumber(src string, i int) int {
	j := i
	for j < len(src) {
		c := src[j]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F',
			c == '.', c == '_', c == 'x', c == 'X':
			j++
		case c == '+' || c == '-':
			if j == i || !strings.ContainsRune("eE", rune(src[j-1])) {
				return j
			}
			j++
		default:
			return j
		}
	}
	return j
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdent(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9'
}
