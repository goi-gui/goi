package tree

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// InputInsert applies a TextField's `type` and `mask` to typed text: it
// replaces text[a:b] with what the field accepts of insert, and returns the
// caret after the last accepted rune. It lives in tree so the rules are
// testable without a window.
func InputInsert(kind, mask, text string, a, b int, insert string) (string, int) {
	if mask == "" {
		kept := filterInput(kind, insert)
		if kept == "" {
			return text, a
		}
		return text[:a] + kept + text[b:], a + len(kept)
	}
	fill, slots := maskSlots(kind, mask, text)
	at := slotsBefore(slots, a)
	end := at
	if b > a {
		end = sort.Search(len(slots), func(i int) bool { return slots[i].start >= b })
		if end < at {
			end = at
		}
	}
	classes := slotClasses(mask)
	var accepted []rune
	slot := at
	for _, r := range insert {
		if slot >= len(classes) {
			break
		}
		if fieldAllows(kind, r) && slotAllows(classes[slot], r) {
			accepted = append(accepted, r)
			slot++
		}
	}
	if len(accepted) == 0 {
		if end == at {
			return text, a
		}
		return buildMasked(mask, splice(fill, at, end, nil), at-1)
	}
	return buildMasked(mask, splice(fill, at, end, accepted), at+len(accepted)-1)
}

// InputDelete removes the rune before or after the caret. A mask's literals
// are skipped, so backspace always removes an entered character.
func InputDelete(kind, mask, text string, caret int, backward bool) (string, int) {
	if mask == "" {
		if backward {
			if caret <= 0 || caret > len(text) {
				return text, caret
			}
			p := prevRuneIndex(text, caret)
			return text[:p] + text[caret:], p
		}
		if caret < 0 || caret >= len(text) {
			return text, caret
		}
		n := nextRuneIndex(text, caret)
		return text[:caret] + text[n:], caret
	}
	fill, slots := maskSlots(kind, mask, text)
	idx := slotsBefore(slots, caret)
	if backward {
		idx--
	}
	if idx < 0 || idx >= len(fill) {
		return text, caret
	}
	return buildMasked(mask, splice(fill, idx, idx+1, nil), idx-1)
}

// InputRemove removes a selected range, dropping only the entered characters it
// covers and keeping the mask's literals.
func InputRemove(kind, mask, text string, a, b int) (string, int) {
	if mask == "" {
		return text[:a] + text[b:], a
	}
	fill, slots := maskSlots(kind, mask, text)
	from := slotsBefore(slots, a)
	to := sort.Search(len(slots), func(i int) bool { return slots[i].start >= b })
	if to < from {
		to = from
	}
	if from == to {
		return text, a
	}
	return buildMasked(mask, splice(fill, from, to, nil), from-1)
}

// filterInput keeps only the runes a field's type accepts.
func filterInput(kind, s string) string {
	var b strings.Builder
	for _, r := range s {
		if fieldAllows(kind, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// fieldAllows is a TextField's `type`: the three named classes, or the
// characters written as the value itself.
func fieldAllows(kind string, r rune) bool {
	switch kind {
	case "", "text":
		return true
	case "numeric":
		return unicode.IsDigit(r)
	case "alpha":
		return unicode.IsLetter(r)
	case "alphanumeric":
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	}
	return strings.ContainsRune(kind, r)
}

// maskTokens reads a mask: # is a digit, @ a letter, * either, and every other
// rune is literal text.
type maskToken struct {
	lit  rune
	slot byte
}

func maskTokens(mask string) []maskToken {
	out := make([]maskToken, 0, len(mask))
	for _, r := range mask {
		switch r {
		case '#', '@', '*':
			out = append(out, maskToken{slot: byte(r)})
		default:
			out = append(out, maskToken{lit: r})
		}
	}
	return out
}

func slotClasses(mask string) []byte {
	var out []byte
	for _, tok := range maskTokens(mask) {
		if tok.slot != 0 {
			out = append(out, tok.slot)
		}
	}
	return out
}

func slotAllows(slot byte, r rune) bool {
	switch slot {
	case '#':
		return unicode.IsDigit(r)
	case '@':
		return unicode.IsLetter(r)
	case '*':
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	}
	return true
}

// inputSlot is where one entered rune sits in the formatted text.
type inputSlot struct {
	start, end int
}

// maskSlots walks a value through its mask and returns the entered runes with
// their byte ranges in text. A raw value (no literals yet) is accepted too: the
// literals are then skipped and the slots take the next rune.
func maskSlots(kind, mask, text string) (fill []rune, slots []inputSlot) {
	ti := 0
	for _, tok := range maskTokens(mask) {
		if tok.slot == 0 {
			if strings.HasPrefix(text[ti:], string(tok.lit)) {
				ti += utf8.RuneLen(tok.lit)
			}
			continue
		}
		if ti >= len(text) {
			break
		}
		r, size := utf8.DecodeRuneInString(text[ti:])
		if !fieldAllows(kind, r) || !slotAllows(tok.slot, r) {
			break
		}
		slots = append(slots, inputSlot{ti, ti + size})
		fill = append(fill, r)
		ti += size
	}
	return fill, slots
}

// slotsBefore counts the entered runes that end at or before at, which is the
// slot index an edit at that byte position starts from.
func slotsBefore(slots []inputSlot, at int) int {
	return sort.Search(len(slots), func(i int) bool { return slots[i].end > at })
}

// splice replaces fill[from:to] with add, copying so the caller's slice stays
// intact.
func splice(fill []rune, from, to int, add []rune) []rune {
	out := make([]rune, 0, len(fill)-(to-from)+len(add))
	out = append(out, fill[:from]...)
	out = append(out, add...)
	return append(out, fill[to:]...)
}

// buildMasked writes the mask with the entered runes in place. Literals appear
// only up to the last entered rune, so an empty field stays empty and a partial
// value has no dangling punctuation. caretSlot is the filled rune the caret
// follows; -1 puts it at the start.
func buildMasked(mask string, fill []rune, caretSlot int) (string, int) {
	var b strings.Builder
	caret, fi := 0, 0
	for _, tok := range maskTokens(mask) {
		if tok.slot == 0 {
			if fi < len(fill) {
				b.WriteRune(tok.lit)
			}
			continue
		}
		if fi >= len(fill) {
			break
		}
		b.WriteRune(fill[fi])
		if fi == caretSlot {
			caret = b.Len()
		}
		fi++
	}
	return b.String(), caret
}

// prevRuneIndex and nextRuneIndex step a byte index over one rune, for the
// unmasked case.
func prevRuneIndex(s string, i int) int {
	if i <= 0 {
		return 0
	}
	_, size := utf8.DecodeLastRuneInString(s[:i])
	return i - size
}

func nextRuneIndex(s string, i int) int {
	if i >= len(s) {
		return len(s)
	}
	_, size := utf8.DecodeRuneInString(s[i:])
	return i + size
}
