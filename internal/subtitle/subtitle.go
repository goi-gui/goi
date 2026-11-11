// Package subtitle parses SRT caption files.
//
// SRT is what every subtitle editor writes and what most films ship beside
// them, so it is the one format the framework reads. The parser is deliberately
// forgiving: a missing index, a CRLF file, a stray blank line or a cue with an
// obviously broken clock is skipped instead of failing the whole file, because a
// film with one damaged cue should still be watchable.
package subtitle

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Cue is one caption: when it appears, when it goes, and what it says.
type Cue struct {
	Start, End time.Duration
	Text       string
}

// ErrEmpty is returned when a document holds no cue at all, which is what a
// wrong file (an image, an empty download) looks like.
var ErrEmpty = errors.New("subtitle: no cue found")

// Parse reads an SRT document.
func Parse(b []byte) ([]Cue, error) {
	text := strings.TrimPrefix(string(b), "\ufeff")
	// A file may use either line ending, and a stray CR would otherwise land in
	// the caption text.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	var cues []Cue
	for _, block := range strings.Split(text, "\n\n") {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) < 2 {
			continue
		}
		// The clock line may be first (no index) or second (with one).
		at := 0
		if !strings.Contains(lines[0], "-->") {
			at = 1
		}
		start, end, ok := parseClock(lines[at])
		if !ok || end <= start {
			continue
		}
		body := clean(strings.Join(lines[at+1:], "\n"))
		if body == "" {
			continue
		}
		cues = append(cues, Cue{Start: start, End: end, Text: body})
	}
	if len(cues) == 0 {
		return nil, ErrEmpty
	}
	sort.Slice(cues, func(i, j int) bool { return cues[i].Start < cues[j].Start })
	return cues, nil
}

// At returns the caption playing at d, or an empty string.
func At(cues []Cue, d time.Duration) string {
	// A binary search keeps a two hour film's thousands of cues cheap on every
	// frame, which is the one place this runs.
	i := sort.Search(len(cues), func(i int) bool { return cues[i].End > d })
	if i < len(cues) && cues[i].Start <= d {
		return cues[i].Text
	}
	return ""
}

// parseClock reads a line like `00:01:02,500 --> 00:01:05,000`. A short form
// (`01:02,500`) is common in hand-edited files and is accepted too.
func parseClock(line string) (time.Duration, time.Duration, bool) {
	parts := strings.Split(line, "-->")
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, ok := parseTime(parts[0])
	if !ok {
		return 0, 0, false
	}
	end, ok := parseTime(parts[1])
	if !ok {
		return 0, 0, false
	}
	return start, end, true
}

func parseTime(s string) (time.Duration, bool) {
	// Anything after the clock (a position hint) is not part of it.
	s = strings.TrimSpace(strings.Fields(strings.TrimSpace(s) + " ")[0])
	s = strings.Replace(s, ",", ".", 1)
	fields := strings.Split(s, ":")
	if len(fields) > 3 {
		return 0, false
	}
	total := 0.0
	for _, f := range fields {
		n, err := strconv.ParseFloat(f, 64)
		if err != nil || n < 0 {
			return 0, false
		}
		total = total*60 + n
	}
	return time.Duration(total * float64(time.Second)), true
}

// clean strips the markup an SRT body may carry: `<i>`, `<b>`, `<font …>` and
// the ASS override codes a converter leaves behind (`{\an8}`). The renderer has
// no rich text on this path, and showing the tags would be worse than losing the
// italics.
func clean(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			if j := strings.IndexByte(s[i:], '>'); j >= 0 {
				i += j
				continue
			}
		case '{':
			if j := strings.IndexByte(s[i:], '}'); j >= 0 {
				i += j
				continue
			}
		}
		out.WriteByte(s[i])
	}
	return strings.TrimSpace(out.String())
}
