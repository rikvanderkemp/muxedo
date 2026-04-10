package ui

import (
	"strings"
	"testing"
	"time"

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

func TestFormatElapsed(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{name: "zero", in: 0, want: "0s"},
		{name: "seconds", in: time.Second, want: "1s"},
		{name: "under minute", in: 59 * time.Second, want: "59s"},
		{name: "one minute", in: time.Minute, want: "1m"},
		{name: "minute and seconds", in: time.Minute + 30*time.Second, want: "1m30s"},
		{name: "hours", in: 2 * time.Hour, want: "2h"},
		{name: "hours and minutes", in: 2*time.Hour + 5*time.Minute, want: "2h5m"},
		{name: "days", in: 24 * time.Hour, want: "1d"},
		{name: "days and hours", in: 26 * time.Hour, want: "1d2h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatElapsed(tt.in); got != tt.want {
				t.Fatalf("formatElapsed(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRenderPaneFooterPlacesTimerOnRight(t *testing.T) {
	row := renderPaneFooter("", 30, false, "1m30s")
	if ansi.StringWidth(row) != 30 {
		t.Fatalf("expected footer width 30, got %d", ansi.StringWidth(row))
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), "1m30s") {
		t.Fatalf("expected timer at right edge, got %q", row)
	}
}

func TestRenderPaneFooterShowsReloadHintAndTimer(t *testing.T) {
	row := renderPaneFooter("", 40, true, "2h5m")
	if !strings.Contains(row, "Press R to reload") {
		t.Fatalf("expected reload hint in footer, got %q", row)
	}
	if !strings.Contains(row, "2h5m") {
		t.Fatalf("expected timer in footer, got %q", row)
	}
	if ansi.StringWidth(row) != 40 {
		t.Fatalf("expected footer width 40, got %d", ansi.StringWidth(row))
	}
}

func TestRenderPaneFooterTruncatesTimerToWidth(t *testing.T) {
	row := renderPaneFooter("", 4, false, "1m30s")
	if ansi.StringWidth(row) != 4 {
		t.Fatalf("expected footer width 4, got %d", ansi.StringWidth(row))
	}
}

func TestRenderPaneShortBodyKeepsOutputVisible(t *testing.T) {
	pane := renderPane("demo", "hello world", 20, 4, false, false, false, "1s")
	if !strings.Contains(pane, "hello") {
		t.Fatalf("expected short pane to keep output visible, got %q", pane)
	}
	if !strings.Contains(pane, "1s") {
		t.Fatalf("expected short pane to show timer, got %q", pane)
	}
}
