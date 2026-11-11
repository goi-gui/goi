package subtitle

import (
	"testing"
	"time"
)

func TestParseBasic(t *testing.T) {
	doc := "\ufeff1\r\n00:00:01,000 --> 00:00:03,500\r\nHello there\r\n\r\n2\r\n00:00:04,000 --> 00:00:06,000\r\nTwo\r\nlines\r\n"
	cues, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 2 {
		t.Fatalf("%d cues", len(cues))
	}
	if cues[0].Start != time.Second || cues[0].End != 3500*time.Millisecond {
		t.Fatalf("first cue = %v-%v", cues[0].Start, cues[0].End)
	}
	if cues[0].Text != "Hello there" {
		t.Fatalf("text = %q", cues[0].Text)
	}
	if cues[1].Text != "Two\nlines" {
		t.Fatalf("multiline text = %q", cues[1].Text)
	}
}

// TestParseIsForgiving covers the damage real files carry: no index, a short
// clock, markup, and a broken cue that must not take the rest down.
func TestParseIsForgiving(t *testing.T) {
	doc := `00:01,500 --> 00:03,000
<i>short form</i>

3
not a clock at all
this cue is broken

4
00:00:05,000 --> 00:00:07,000
{\an8}<b>top</b> and <font color="red">red</font>
`
	cues, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 2 {
		t.Fatalf("%d cues: %+v", len(cues), cues)
	}
	if cues[0].Start != 1500*time.Millisecond || cues[0].Text != "short form" {
		t.Fatalf("first = %+v", cues[0])
	}
	if cues[1].Text != "top and red" {
		t.Fatalf("markup survived: %q", cues[1].Text)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, doc := range []string{"", "just some text", "1\n00:00:02,000 --> 00:00:01,000\nbackwards"} {
		if _, err := Parse([]byte(doc)); err != ErrEmpty {
			t.Errorf("Parse(%q) = %v, want ErrEmpty", doc, err)
		}
	}
}

func TestAt(t *testing.T) {
	cues := []Cue{
		{Start: time.Second, End: 2 * time.Second, Text: "one"},
		{Start: 2 * time.Second, End: 3 * time.Second, Text: "two"},
	}
	cases := map[time.Duration]string{
		0:                       "",
		1500 * time.Millisecond: "one",
		2 * time.Second:         "two",
		3 * time.Second:         "",
		10 * time.Second:        "",
	}
	for at, want := range cases {
		if got := At(cues, at); got != want {
			t.Errorf("At(%v) = %q, want %q", at, got, want)
		}
	}
	if got := At(nil, time.Second); got != "" {
		t.Errorf("At(nil) = %q", got)
	}
}
