package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPadOrTruncateASCII(t *testing.T) {
	line := strings.Repeat("a", 50)
	got := padOrTruncate(line, 30)
	if ansi.StringWidth(got) != 30 {
		t.Fatalf("want display width 30, got %d (%q)", ansi.StringWidth(got), got)
	}
	if got != strings.Repeat("a", 30) {
		t.Fatalf("unexpected truncation: %q", got)
	}
}

func TestPadOrTruncatePadsByDisplayWidth(t *testing.T) {
	got := padOrTruncate("ab", 5)
	if ansi.StringWidth(got) != 5 {
		t.Fatalf("want display width 5, got %d", ansi.StringWidth(got))
	}
	if got != "ab   " {
		t.Fatalf("got %q", got)
	}
}

func TestPadOrTruncateWideRunes(t *testing.T) {
	// Hiragana is typically 2 cells; 3 runes, 4 cells wide.
	line := "a\u3042b"
	if ansi.StringWidth(line) != 4 {
		t.Fatalf("precondition: line should be 4 cells wide, got %d", ansi.StringWidth(line))
	}
	got := padOrTruncate(line, 3)
	if ansi.StringWidth(got) != 3 {
		t.Fatalf("want display width 3, got %d (%q)", ansi.StringWidth(got), got)
	}
}
