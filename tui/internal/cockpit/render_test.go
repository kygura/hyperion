package cockpit

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestFnum(t *testing.T) {
	cases := []struct {
		v    float64
		dec  int
		want string
	}{
		{3412.4, 1, "3,412.4"},
		{67231, 0, "67,231"},
		{0.8412, 4, "0.8412"},
		{1234567.89, 2, "1,234,567.89"},
	}
	for _, c := range cases {
		if got := fnum(c.v, c.dec); got != c.want {
			t.Errorf("fnum(%v, %d) = %q, want %q", c.v, c.dec, got, c.want)
		}
	}
}

func TestPriceDec(t *testing.T) {
	cases := []struct {
		v    float64
		want int
	}{{0.5, 4}, {42, 2}, {3412, 1}, {67231, 0}}
	for _, c := range cases {
		if got := priceDec(c.v); got != c.want {
			t.Errorf("priceDec(%v) = %d, want %d", c.v, got, c.want)
		}
	}
}

func TestSpreadWidth(t *testing.T) {
	s := spread("left", "right", 40)
	if w := lipgloss.Width(s); w != 40 {
		t.Errorf("spread width = %d, want 40", w)
	}
}

func TestPad(t *testing.T) {
	if got := padR("ab", 5); got != "ab   " {
		t.Errorf("padR = %q", got)
	}
	if got := padL("ab", 5); got != "   ab" {
		t.Errorf("padL = %q", got)
	}
}

func TestBar(t *testing.T) {
	b := bar(0.5, 10)
	if w := lipgloss.Width(b); w != 10 {
		t.Errorf("bar width = %d, want 10", w)
	}
	if b2 := bar(1.5, 10); lipgloss.Width(b2) != 10 { // ratio clamped
		t.Errorf("bar clamp failed")
	}
}

func TestBoxGeometry(t *testing.T) {
	out := box("TITLE", "right", []string{"one", "two"}, 40, 8)
	rows := strings.Split(out, "\n")
	if len(rows) != 8 {
		t.Fatalf("box height = %d rows, want 8", len(rows))
	}
	for i, r := range rows {
		if w := lipgloss.Width(r); w != 40 {
			t.Errorf("row %d width = %d, want 40", i, w)
		}
	}
	if !strings.Contains(out, "TITLE") {
		t.Error("box missing title")
	}
}

func TestCvdStr(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{0, "+0"},
		{42, "+42"},
		{-42, "-42"},
		{4200, "+4.2K"},
		{-4200, "-4.2K"},
		{4.2e6, "+4.2M"},
		{-4.2e6, "-4.2M"},
		{4.2e9, "+4.2B"},
		{-4.2e9, "-4.2B"},
	}
	for _, c := range cases {
		if got := cvdStr(c.v); got != c.want {
			t.Errorf("cvdStr(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestTruncTail(t *testing.T) {
	if got := truncTail("hello world", 6); lipgloss.Width(got) > 6 {
		t.Errorf("truncTail too wide: %q", got)
	}
}

// TestWordWrapMultibyte is the fix's proof: before the fix, wordWrap sliced
// on byte offsets (len(s), s[:width]), so multibyte UTF-8 either overflowed
// the intended column or split a rune's bytes in half. Each line here must
// stay within the display-cell budget, and no line may end up empty or with
// a broken rune.
func TestWordWrapMultibyte(t *testing.T) {
	s := "比特币 以太坊 比特币 以太坊 比特币"
	out := wordWrap(s, 10)
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 10 {
			t.Errorf("line %q has display width %d, want <= 10", line, w)
		}
	}
	if strings.Join(strings.Fields(out), " ") != strings.Join(strings.Fields(s), " ") {
		t.Errorf("wordWrap lost or mangled content: got %q from %q", out, s)
	}
}

func TestWordWrapASCII(t *testing.T) {
	out := wordWrap("the quick brown fox jumps", 10)
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 10 {
			t.Errorf("line %q has display width %d, want <= 10", line, w)
		}
	}
}
