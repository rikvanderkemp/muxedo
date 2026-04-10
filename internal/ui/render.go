package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type statusSegment struct {
	Text string
	FG   lipgloss.Color
	BG   lipgloss.Color
}

var (
	// Inactive, running: muted blue so it reads as "alive but unfocused".
	inactiveBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("67"))

	inactiveTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("252")).
				Background(lipgloss.Color("60")).
				Padding(0, 1)

	// Active pane: orange = normal mode, green = insert (keys go to PTY).
	activeNormalBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("208"))

	activeInsertBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("46"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)

	activeNormalTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("230")).
				Background(lipgloss.Color("208")).
				Padding(0, 1)

	activeInsertTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("230")).
				Background(lipgloss.Color("34")).
				Padding(0, 1)

	// Stopped process: neutral grey (focused or not).
	stoppedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("240"))

	stoppedTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("245")).
				Background(lipgloss.Color("238")).
				Padding(0, 1)

	overlayStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("238")).
			Padding(0, 1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)
)

// renderStatusLine renders a single full-width status row.
func renderStatusLine(width int, segments []statusSegment) string {
	if width < 1 {
		return ""
	}
	inner := width - 2 // padding 0,1 on left and right
	if inner < 1 {
		inner = 1
	}

	var b strings.Builder
	for i, seg := range segments {
		segmentText := lipgloss.NewStyle().
			Foreground(seg.FG).
			Background(seg.BG).
			Render(" " + seg.Text + " ")
		b.WriteString(segmentText)
		if i+1 < len(segments) {
			next := segments[i+1]
			b.WriteString(lipgloss.NewStyle().
				Foreground(seg.BG).
				Background(next.BG).
				Render(""))
		} else {
			b.WriteString(lipgloss.NewStyle().
				Foreground(seg.BG).
				Render(""))
		}
	}

	content := b.String()
	if ansi.StringWidth(content) > inner {
		content = ansi.Truncate(content, inner, "")
	}
	if w := ansi.StringWidth(content); w < inner {
		content += strings.Repeat(" ", inner-w)
	}

	return statusBarStyle.Width(width).MaxWidth(width).Render(content)
}

// insertMode is read only when active is true: green border/title in insert, orange in normal.
func renderPane(title string, output string, width, height int, active bool, stopped bool, insertMode bool) string {
	innerW := width - 2
	innerH := height - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}

	displayTitle := title
	if stopped {
		displayTitle = title + " — Process stopped"
	}
	if active {
		if insertMode {
			displayTitle += " · INSERT"
		} else {
			displayTitle += " · NORMAL"
		}
	}

	var titleRenderer lipgloss.Style
	var borderRenderer lipgloss.Style
	var titleSeparatorRenderer lipgloss.Style
	switch {
	case stopped:
		titleRenderer = stoppedTitleStyle
		borderRenderer = stoppedBorderStyle
		titleSeparatorRenderer = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	case active:
		if insertMode {
			titleRenderer = activeInsertTitleStyle
			borderRenderer = activeInsertBorderStyle
			titleSeparatorRenderer = lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
		} else {
			titleRenderer = activeNormalTitleStyle
			borderRenderer = activeNormalBorderStyle
			titleSeparatorRenderer = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
		}
	default:
		titleRenderer = inactiveTitleStyle
		borderRenderer = inactiveBorderStyle
		titleSeparatorRenderer = lipgloss.NewStyle().Foreground(lipgloss.Color("60"))
	}

	titleChip := titleRenderer.Render(displayTitle)
	titleBar := padOrTruncate(titleChip+titleSeparatorRenderer.Render(""), innerW)
	contentH := innerH - 1
	if contentH < 0 {
		contentH = 0
	}

	reloadHint := stopped && active
	outLines := contentH
	if reloadHint && outLines > 0 {
		outLines--
	}

	lines := fitLines(output, outLines, innerW)
	content := strings.Join(lines, "\n")
	if reloadHint {
		if outLines > 0 {
			hint := overlayStyle.Render("Press R to reload")
			hintRow := lipgloss.Place(innerW, 1, lipgloss.Center, lipgloss.Center, hint)
			content = content + "\n" + hintRow
		} else {
			hint := overlayStyle.Render("Press R to reload")
			content = lipgloss.Place(innerW, contentH, lipgloss.Center, lipgloss.Center, hint)
		}
	}

	inner := titleBar + "\n" + content

	box := borderRenderer.
		Width(innerW).
		Height(innerH).
		MaxWidth(width).
		MaxHeight(height).
		Render(inner)

	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, box)
}

func renderEmptyPane(width, height int) string {
	innerW := width - 2
	innerH := height - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	box := inactiveBorderStyle.
		Width(innerW).
		Height(innerH).
		MaxWidth(width).
		MaxHeight(height).
		BorderForeground(lipgloss.Color("236")).
		Render("")
	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, box)
}

// fitLines takes the VT screen dump and returns exactly `n` lines,
// each truncated/padded to `maxWidth`.
func fitLines(raw string, n, maxWidth int) []string {
	all := strings.Split(raw, "\n")

	// Take last n lines (tail)
	if len(all) > n {
		all = all[len(all)-n:]
	}

	lines := make([]string, n)
	for i := 0; i < n; i++ {
		if i < len(all) {
			lines[i] = padOrTruncate(all[i], maxWidth)
		} else {
			lines[i] = strings.Repeat(" ", maxWidth)
		}
	}

	return lines
}

func padOrTruncate(line string, targetWidth int) string {
	w := ansi.StringWidth(line)
	if w > targetWidth {
		return ansi.Truncate(line, targetWidth, "")
	}
	if w < targetWidth {
		return line + strings.Repeat(" ", targetWidth-w)
	}
	return line
}
