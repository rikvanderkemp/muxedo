package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"muxedo/internal/process"
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
	row := renderPaneFooter(DefaultTheme(), "", 30, false, "1m30s")
	if ansi.StringWidth(row) != 30 {
		t.Fatalf("expected footer width 30, got %d", ansi.StringWidth(row))
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), "1m30s") {
		t.Fatalf("expected timer at right edge, got %q", row)
	}
}

func TestRenderPaneFooterShowsReloadHintAndTimer(t *testing.T) {
	row := renderPaneFooter(DefaultTheme(), "", 40, true, "2h5m")
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
	row := renderPaneFooter(DefaultTheme(), "", 4, false, "1m30s")
	if ansi.StringWidth(row) != 4 {
		t.Fatalf("expected footer width 4, got %d", ansi.StringWidth(row))
	}
}

func TestRenderPaneShortBodyKeepsOutputVisible(t *testing.T) {
	pane := renderPane(DefaultTheme(), "demo", "hello world", 20, 4, false, false, false, false, nil, "1s")
	if !strings.Contains(pane, "hello") {
		t.Fatalf("expected short pane to keep output visible, got %q", pane)
	}
	if !strings.Contains(pane, "1s") {
		t.Fatalf("expected short pane to show timer, got %q", pane)
	}
}

func TestFitLinesTrimsBlankTerminalTail(t *testing.T) {
	raw := strings.Join([]string{
		"Insert mode demo: type something and press Enter.",
		"",
		"",
		"",
		"",
	}, "\n")

	lines := fitLines(raw, 3, 50)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "Insert mode demo") {
		t.Fatalf("expected prompt text to stay visible, got %q", joined)
	}
}

func TestRenderStatusLineUsesThemeColors(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(oldProfile)
	})

	theme := DefaultTheme()
	theme.StatusBarFG = "#112233"
	theme.StatusBarBG = "#445566"

	row := renderStatusLine(theme, 24, []statusSegment{
		{Text: "demo", FG: theme.color("#abcdef"), BG: theme.color("#123456")},
	})

	if !strings.Contains(row, "38;2;17;34;51") {
		t.Fatalf("expected status bar fg truecolor escape in %q", row)
	}
	if !strings.Contains(row, "48;2;68;85;102") {
		t.Fatalf("expected status bar bg truecolor escape in %q", row)
	}
}

func TestViewMaximizedShowsOnlyActivePane(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("alpha", "echo alpha", "", "."),
		process.New("beta", "echo beta", "", "."),
	})
	model.width = 80
	model.height = 20
	model.activePanel = 0
	model.maximizedPanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	view := model.View()
	if !strings.Contains(view, "alpha") {
		t.Fatalf("expected maximized view to include active panel, got %q", view)
	}
	if strings.Contains(view, "beta") {
		t.Fatalf("expected maximized view to hide inactive panel, got %q", view)
	}
}

func TestRenderPaneShowsScrollModeTitle(t *testing.T) {
	pane := renderPane(DefaultTheme(), "demo", "hello", 20, 6, true, false, false, true, nil, "1s")
	if !strings.Contains(pane, "SCROLL") {
		t.Fatalf("expected scroll mode title, got %q", pane)
	}
}

func TestFitViewportLinesHighlightsSelectedAndMarkedRows(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(oldProfile)
	})

	lines := fitViewportLines(DefaultTheme(), &paneViewport{
		Lines:       []string{"one", "two", "three"},
		SelectedRow: 1,
		MarkedRow:   2,
	}, 3, 10)

	if !strings.Contains(lines[1], "\x1b[") {
		t.Fatalf("expected selected row to be styled, got %q", lines[1])
	}
	if !strings.Contains(lines[2], "\x1b[") {
		t.Fatalf("expected marked row to be styled, got %q", lines[2])
	}
}
