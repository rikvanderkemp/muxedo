package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type statusSegment struct {
	Text string
	FG   lipgloss.Color
	BG   lipgloss.Color
}

type paneViewport struct {
	Lines             []string
	PlainLines        []string
	SelectedRow       int
	MarkedRow         int
	SelectionActive   bool
	SelectionStartRow int
	SelectionStartCol int
	SelectionEndRow   int
	SelectionEndCol   int
}

// renderStatusLine renders a single full-width status row.
func renderStatusLine(theme Theme, width int, segments []statusSegment) string {
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

	statusBarStyle := lipgloss.NewStyle().
		Foreground(theme.color(theme.StatusBarFG)).
		Background(theme.color(theme.StatusBarBG)).
		Padding(0, 1)

	return statusBarStyle.Width(width).MaxWidth(width).Render(content)
}

// insertMode is read only when active is true: green border/title in insert, orange in normal.
func renderPane(theme Theme, title string, output string, width, height int, active bool, stopped bool, insertMode bool, scrollMode bool, selectMode bool, viewport *paneViewport, timer string) string {
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
		} else if selectMode {
			displayTitle += " · SELECT"
		} else if scrollMode {
			displayTitle += " · SCROLL"
		} else {
			displayTitle += " · NORMAL"
		}
	}

	var titleRenderer lipgloss.Style
	var borderRenderer lipgloss.Style
	var titleSeparatorRenderer lipgloss.Style
	switch {
	case stopped:
		titleRenderer = lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.color(theme.StoppedTitleFG)).
			Background(theme.color(theme.StoppedTitleBG)).
			Padding(0, 1)
		borderRenderer = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(theme.color(theme.StoppedBorder))
		titleSeparatorRenderer = lipgloss.NewStyle().Foreground(theme.color(theme.StoppedTitleBG))
	case active:
		if insertMode {
			titleRenderer = lipgloss.NewStyle().
				Bold(true).
				Foreground(theme.color(theme.ActiveInsertTitleFG)).
				Background(theme.color(theme.ActiveInsertTitleBG)).
				Padding(0, 1)
			borderRenderer = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(theme.color(theme.ActiveInsertBorder))
			titleSeparatorRenderer = lipgloss.NewStyle().Foreground(theme.color(theme.ActiveInsertTitleBG))
		} else {
			titleRenderer = lipgloss.NewStyle().
				Bold(true).
				Foreground(theme.color(theme.ActiveNormalTitleFG)).
				Background(theme.color(theme.ActiveNormalTitleBG)).
				Padding(0, 1)
			borderRenderer = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(theme.color(theme.ActiveNormalBorder))
			titleSeparatorRenderer = lipgloss.NewStyle().Foreground(theme.color(theme.ActiveNormalTitleBG))
		}
	default:
		titleRenderer = lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.color(theme.InactiveTitleFG)).
			Background(theme.color(theme.InactiveTitleBG)).
			Padding(0, 1)
		borderRenderer = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(theme.color(theme.InactiveBorder))
		titleSeparatorRenderer = lipgloss.NewStyle().Foreground(theme.color(theme.InactiveTitleBG))
	}

	titleChip := titleRenderer.Render(displayTitle)
	titleBar := padOrTruncate(titleChip+titleSeparatorRenderer.Render(""), innerW)
	contentH := innerH - 1
	if contentH < 0 {
		contentH = 0
	}

	reloadHint := stopped && active
	outLines := contentH
	useDedicatedFooterRow := contentH > 1
	if useDedicatedFooterRow {
		outLines--
	}

	lines := fitLines(output, outLines, innerW)
	if viewport != nil {
		lines = fitViewportLines(theme, viewport, outLines, innerW)
	}
	if contentH == 1 {
		if viewport != nil {
			lines = fitViewportLines(theme, viewport, 1, innerW)
		} else {
			lines = fitLines(output, 1, innerW)
		}
	}

	if contentH == 1 && len(lines) > 0 {
		lines[len(lines)-1] = renderPaneFooter(theme, lines[len(lines)-1], innerW, reloadHint, timer)
	}

	content := strings.Join(lines, "\n")
	if useDedicatedFooterRow {
		content += "\n" + renderPaneFooter(theme, "", innerW, reloadHint, timer)
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

func renderEmptyPane(theme Theme, width, height int) string {
	innerW := width - 2
	innerH := height - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	box := lipgloss.NewStyle().
		Width(innerW).
		Height(innerH).
		MaxWidth(width).
		MaxHeight(height).
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.color(theme.EmptyBorder)).
		Render("")
	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, box)
}

func renderPaneFooter(theme Theme, base string, width int, reloadHint bool, timer string) string {
	if width < 1 {
		return ""
	}
	base = padOrTruncate(base, width)

	overlayStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.color(theme.OverlayFG)).
		Background(theme.color(theme.OverlayBG)).
		Padding(0, 1)

	left := ""
	if reloadHint {
		left = overlayStyle.Render("Press R to reload")
	}

	right := ""
	if timer != "" {
		right = overlayStyle.Render(timer)
	}

	leftWidth := ansi.StringWidth(left)
	rightWidth := ansi.StringWidth(right)

	switch {
	case left == "" && right == "":
		return base
	case left == "":
		if rightWidth >= width {
			return ansi.Truncate(right, width, "")
		}
		prefixWidth := width - rightWidth
		prefix := ansi.Truncate(base, prefixWidth, "")
		prefix = padOrTruncate(prefix, prefixWidth)
		return prefix + right
	case right == "":
		left = padOrTruncate(left, width)
		leftWidth = ansi.StringWidth(left)
		if leftWidth < width {
			return left + ansi.Truncate(base, width-leftWidth, "")
		}
		return left
	}

	if leftWidth+1+rightWidth <= width {
		return left + strings.Repeat(" ", width-leftWidth-rightWidth) + right
	}

	leftBudget := width - rightWidth - 1
	if leftBudget < 0 {
		leftBudget = 0
	}
	left = ansi.Truncate(left, leftBudget, "")
	leftWidth = ansi.StringWidth(left)
	if leftWidth+rightWidth < width {
		return left + strings.Repeat(" ", width-leftWidth-rightWidth) + right
	}
	if rightWidth > width {
		return ansi.Truncate(right, width, "")
	}
	return left + right
}

func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}

	totalSeconds := int(d / time.Second)
	days := totalSeconds / (24 * 60 * 60)
	totalSeconds %= 24 * 60 * 60
	hours := totalSeconds / (60 * 60)
	totalSeconds %= 60 * 60
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60

	switch {
	case days > 0:
		if hours > 0 {
			return formatUnit(days, "d") + formatUnit(hours, "h")
		}
		return formatUnit(days, "d")
	case hours > 0:
		if minutes > 0 {
			return formatUnit(hours, "h") + formatUnit(minutes, "m")
		}
		return formatUnit(hours, "h")
	case minutes > 0:
		if seconds > 0 {
			return formatUnit(minutes, "m") + formatUnit(seconds, "s")
		}
		return formatUnit(minutes, "m")
	default:
		return formatUnit(seconds, "s")
	}
}

func formatUnit(value int, suffix string) string {
	return strconv.Itoa(value) + suffix
}

// fitLines takes the VT screen dump and returns exactly `n` lines,
// each truncated/padded to `maxWidth`.
func fitLines(raw string, n, maxWidth int) []string {
	all := strings.Split(raw, "\n")
	all = trimViewportTail(all)

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

func trimViewportTail(lines []string) []string {
	lastNonEmpty := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(ansi.Strip(lines[i])) != "" {
			lastNonEmpty = i
			break
		}
	}
	if lastNonEmpty < 0 {
		return lines
	}

	// Keep one trailing blank row after the last visible content so prompts
	// that end with a newline still leave space for the current input line.
	keep := lastNonEmpty + 2
	if keep > len(lines) {
		keep = len(lines)
	}
	return lines[:keep]
}

func fitViewportLines(theme Theme, viewport *paneViewport, n, maxWidth int) []string {
	lines := make([]string, n)
	for i := 0; i < n; i++ {
		line := strings.Repeat(" ", maxWidth)
		if i < len(viewport.Lines) {
			line = padOrTruncate(viewport.Lines[i], maxWidth)
		}
		plainLine := ansi.Strip(line)
		if i < len(viewport.PlainLines) {
			plainLine = padOrTruncate(viewport.PlainLines[i], maxWidth)
		}

		if viewport.SelectionActive {
			if startCol, endCol, ok := selectionColumnsForRow(viewport, i, maxWidth); ok {
				lines[i] = renderSelectedColumns(theme, plainLine, startCol, endCol)
				continue
			}
		}

		switch {
		case i == viewport.SelectedRow && i == viewport.MarkedRow:
			line = lipgloss.NewStyle().
				Bold(true).
				Foreground(theme.color(theme.OverlayFG)).
				Background(theme.color(theme.ActiveNormalTitleBG)).
				Render(line)
		case i == viewport.MarkedRow:
			line = lipgloss.NewStyle().
				Bold(true).
				Foreground(theme.color(theme.OverlayFG)).
				Background(theme.color(theme.ActiveInsertTitleBG)).
				Render(line)
		case i == viewport.SelectedRow:
			line = lipgloss.NewStyle().
				Foreground(theme.color(theme.StatusBarFG)).
				Background(theme.color(theme.StatusActivePanelBG)).
				Render(line)
		}

		lines[i] = line
	}
	return lines
}

func selectionColumnsForRow(viewport *paneViewport, row, maxWidth int) (int, int, bool) {
	if !viewport.SelectionActive {
		return 0, 0, false
	}
	startRow, startCol, endRow, endCol := normalizeSelection(
		viewport.SelectionStartRow,
		viewport.SelectionStartCol,
		viewport.SelectionEndRow,
		viewport.SelectionEndCol,
	)
	if row < startRow || row > endRow {
		return 0, 0, false
	}

	lineStart := 0
	lineEnd := maxWidth
	if row == startRow {
		lineStart = startCol
	}
	if row == endRow {
		lineEnd = endCol + 1
	}
	if lineStart < 0 {
		lineStart = 0
	}
	if lineEnd > maxWidth {
		lineEnd = maxWidth
	}
	if lineStart >= lineEnd {
		return 0, 0, false
	}
	return lineStart, lineEnd, true
}

func renderSelectedColumns(theme Theme, line string, startCol, endCol int) string {
	startCol = max(0, startCol)
	endCol = max(startCol, endCol)
	before := sliceByCells(line, 0, startCol)
	selected := sliceByCells(line, startCol, endCol)
	after := sliceByCells(line, endCol, ansi.StringWidth(line))
	if selected == "" {
		return line
	}
	selected = lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.color(theme.OverlayFG)).
		Background(theme.color(theme.StatusActivePanelBG)).
		Render(selected)
	return before + selected + after
}

func normalizeSelection(startRow, startCol, endRow, endCol int) (int, int, int, int) {
	if startRow > endRow || (startRow == endRow && startCol > endCol) {
		return endRow, endCol, startRow, startCol
	}
	return startRow, startCol, endRow, endCol
}

func sliceByCells(line string, startCol, endCol int) string {
	if endCol <= startCol {
		return ""
	}
	var out strings.Builder
	col := 0
	for _, r := range line {
		width := ansi.StringWidth(string(r))
		if width <= 0 {
			width = 1
		}
		nextCol := col + width
		if nextCol > startCol && col < endCol {
			out.WriteRune(r)
		}
		col = nextCol
		if col >= endCol {
			break
		}
	}
	return out.String()
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
