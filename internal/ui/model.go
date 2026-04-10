package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"muxedo/internal/layout"
	"muxedo/internal/process"
)

type tickMsg time.Time

type exitProgressMsg struct {
	panelIdx int
	status   string
}

func killPanelCmd(idx int, p *process.Panel) tea.Cmd {
	return func() tea.Msg {
		p.RunCmdKill()
		p.Stop()
		return exitProgressMsg{
			panelIdx: idx,
			status:   fmt.Sprintf("exiting panel %s.... exiting completed...", p.Name),
		}
	}
}

type Model struct {
	panels           []*process.Panel
	theme            Theme
	width            int
	height           int
	grid             layout.Grid
	activePanel      int
	maximizedPanel   int
	panelInsertMode  bool // when a panel is focused: false = normal (vim-like), true = keys go to PTY
	panelScrollMode  bool
	prevPanelRunning []bool // per-panel running state last tick; detects run→stop while focused
	scrollOffsets    []int
	scrollSelections []int
	scrollMarks      []uint64
	sendInput        func(*process.Panel, []byte) error
	panelRunning     func(*process.Panel) bool
	restartPanel     func(*process.Panel) error
	historyLines     func(*process.Panel) []process.HistoryLine
	exiting          bool
	exitStatuses     []string
	exitCompleted    int
}

func NewModel(panels []*process.Panel, themes ...Theme) Model {
	theme := DefaultTheme()
	if len(themes) > 0 {
		theme = themes[0]
	}

	return Model{
		panels:         panels,
		theme:          theme,
		grid:           layout.Compute(len(panels)),
		activePanel:    -1,
		maximizedPanel: -1,
		sendInput: func(p *process.Panel, input []byte) error {
			return p.WriteInput(input)
		},
		panelRunning: func(p *process.Panel) bool {
			return p.Running()
		},
		restartPanel: func(p *process.Panel) error {
			return p.Restart()
		},
		historyLines: func(p *process.Panel) []process.HistoryLine {
			return p.History()
		},
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tick(), tea.ClearScreen)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.exiting {
		switch msg := msg.(type) {
		case exitProgressMsg:
			m.exitStatuses[msg.panelIdx] = msg.status
			m.exitCompleted++
			if m.exitCompleted == len(m.panels) {
				return m, tea.Quit
			}
			return m, nil
		}
		// Ignore other messages while exiting
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyEsc {
			if m.activePanel >= 0 {
				if m.panelScrollMode {
					m.panelScrollMode = false
					return m, nil
				}
				if m.panelInsertMode {
					m.panelInsertMode = false
					return m, nil
				}
				m.maximizedPanel = -1
				m.activePanel = -1
				m.panelInsertMode = false
				m.panelScrollMode = false
				m.resizePanels()
				return m, nil
			}
			return m, nil
		}

		if m.activePanel < 0 &&
			msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) == 1 {
			if idx, ok := panelIndexFromDigit(msg.Runes[0], len(m.panels)); ok {
				m.applyPanelFocus(idx)
				return m, nil
			}
		}

		if m.activePanel >= 0 && m.activePanel < len(m.panels) {
			m.ensureScrollState()
			p := m.panels[m.activePanel]

			if !m.panelInsertMode && !m.panelScrollMode &&
				msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) == 1 {
				r := msg.Runes[0]
				if next, ok := neighborPanelIndex(m.grid, len(m.panels), m.activePanel, r); ok {
					m.applyPanelFocus(next)
					return m, nil
				}
				if idx, ok := panelIndexFromDigit(r, len(m.panels)); ok {
					m.applyPanelFocus(idx)
					return m, nil
				}
			}

			running := m.panelRunning(p)
			if !running {
				if m.panelInsertMode {
					return m, nil
				}
				if m.panelScrollMode {
					m.handleScrollKey(msg)
					return m, nil
				}
				if msg.Type == tea.KeyRunes && (string(msg.Runes) == "r" || string(msg.Runes) == "R") {
					_ = m.restartPanel(p)
					m.resizePanels()
					return m, nil
				}
				if msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) == 1 {
					switch msg.Runes[0] {
					case 'i', 'I':
						m.panelInsertMode = true
					case 'm', 'M':
						m.toggleMaximized()
					case 'z', 'Z':
						m.enterScrollMode()
					}
				}
				return m, nil
			}

			if m.panelScrollMode {
				m.handleScrollKey(msg)
				return m, nil
			}

			if m.panelInsertMode {
				if input := keyMsgToBytes(msg); len(input) > 0 {
					_ = m.sendInput(p, input)
				}
				return m, nil
			}

			if msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) == 1 {
				switch msg.Runes[0] {
				case 'i', 'I':
					m.panelInsertMode = true
					return m, nil
				case 'm', 'M':
					m.toggleMaximized()
					return m, nil
				case 'z', 'Z':
					m.enterScrollMode()
					return m, nil
				case 'r', 'R':
					_ = m.restartPanel(p)
					m.resizePanels()
					return m, nil
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			m.exiting = true
			m.exitStatuses = make([]string, len(m.panels))
			var cmds []tea.Cmd
			for i, p := range m.panels {
				m.exitStatuses[i] = fmt.Sprintf("exiting panel %s....", p.Name)
				cmds = append(cmds, killPanelCmd(i, p))
			}
			return m, tea.Batch(cmds...)
		}

	case tea.MouseMsg:
		if m.activePanel >= 0 && m.panelScrollMode && msg.Action == tea.MouseActionPress {
			if idx, ok := m.panelIndexAt(msg.X, msg.Y); ok && idx == m.activePanel {
				switch msg.Button {
				case tea.MouseButtonWheelUp:
					m.scrollViewportBy(-3)
					return m, nil
				case tea.MouseButtonWheelDown:
					m.scrollViewportBy(3)
					return m, nil
				}
			}
		}
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if idx, ok := m.panelIndexAt(msg.X, msg.Y); ok {
				m.activePanel = idx
				m.panelInsertMode = false
				m.panelScrollMode = false
				if m.maximizedPanel >= 0 {
					m.maximizedPanel = idx
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizePanels()
		return m, tea.ClearScreen

	case tickMsg:
		n := len(m.panels)
		if len(m.prevPanelRunning) != n {
			m.prevPanelRunning = make([]bool, n)
			for i := range m.panels {
				m.prevPanelRunning[i] = m.panelRunning(m.panels[i])
			}
		} else {
			for i := range m.panels {
				now := m.panelRunning(m.panels[i])
				if i == m.activePanel && m.prevPanelRunning[i] && !now {
					m.maximizedPanel = -1
					m.activePanel = -1
					m.panelInsertMode = false
					m.panelScrollMode = false
					m.resizePanels()
				}
				m.prevPanelRunning[i] = now
			}
		}
		return m, tick()
	}

	return m, nil
}

func (m Model) gridHeight() int {
	if m.height > 1 {
		return m.height - 1
	}
	return m.height
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Starting muxedo..."
	}

	gh := m.gridHeight()
	if idx, ok := m.visibleMaximizedPanel(); ok {
		p := m.panels[idx]
		out := p.DisplayForView()
		stopped := !m.panelRunning(p)
		body := renderPane(
			m.theme,
			formatPanelTitle(idx, p.Name),
			out,
			m.width,
			gh,
			idx == m.activePanel,
			stopped,
			m.panelInsertMode,
			m.panelScrollMode,
			m.viewportForPanel(idx, gh),
			formatElapsed(p.Elapsed()),
		)
		if m.height > 1 {
			body = lipgloss.JoinVertical(lipgloss.Left, body, m.renderStatusLine())
		}
		return lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, m.wrapExiting(body))
	}

	cell := layout.CellSizes(m.width, gh, m.grid.Rows, m.grid.Cols)

	var gridRows []string
	idx := 0
	for r := 0; r < m.grid.Rows; r++ {
		var cols []string
		for c := 0; c < m.grid.Cols; c++ {
			if idx < len(m.panels) {
				p := m.panels[idx]
				out := p.DisplayForView()
				stopped := !m.panelRunning(p)
				pane := renderPane(
					m.theme,
					formatPanelTitle(idx, p.Name),
					out,
					cell.Width,
					cell.Height,
					idx == m.activePanel,
					stopped,
					m.panelInsertMode,
					m.panelScrollMode && idx == m.activePanel,
					m.viewportForPanel(idx, cell.Height),
					formatElapsed(p.Elapsed()),
				)
				cols = append(cols, pane)
			} else {
				empty := renderEmptyPane(m.theme, cell.Width, cell.Height)
				cols = append(cols, empty)
			}
			idx++
		}
		row := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
		gridRows = append(gridRows, row)
	}

	body := strings.Join(gridRows, "\n")
	if m.height > 1 {
		body = lipgloss.JoinVertical(lipgloss.Left, body, m.renderStatusLine())
	}

	return lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, m.wrapExiting(body))
}

func (m Model) wrapExiting(body string) string {
	if !m.exiting {
		return body
	}
	dialogBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 3).
		Align(lipgloss.Center).
		Render(strings.Join(m.exitStatuses, "\n"))

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialogBox)
}

func (m Model) statusModeLabel() string {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return "NONE"
	}
	if m.panelInsertMode {
		return "INSERT"
	}
	if m.panelScrollMode {
		return "SCROLL"
	}
	return "NORMAL"
}

func (m Model) statusHint() string {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return "No active: 1–9 focus · NORMAL: hjkl panes · Ctrl-C quit"
	}

	p := m.panels[m.activePanel]
	parts := []string{}
	maximizeAction := "M maximize"
	escapeAction := "Esc blur"
	if m.maximizedPanel == m.activePanel {
		maximizeAction = "M restore"
		escapeAction = "Esc restore+blur"
	}
	if !m.panelRunning(p) {
		if m.panelInsertMode {
			parts = append(parts, "STOPPED-INSERT: Esc normal")
		} else if m.panelScrollMode {
			parts = append(parts, "SCROLL: PgUp/PgDn wheel", "J/K move", "M mark", "Esc normal")
		} else {
			parts = append(parts, "STOPPED-NORMAL: I insert", "Z scroll", "1–9 hjkl panes", maximizeAction, "R reload", escapeAction)
		}
	} else if m.panelInsertMode {
		parts = append(parts, "INSERT: Esc normal")
	} else if m.panelScrollMode {
		parts = append(parts, "SCROLL: PgUp/PgDn wheel", "J/K move", "M mark", "G live", "Esc normal")
	} else {
		parts = append(parts, "NORMAL: I insert", "Z scroll", "1–9 hjkl panes", maximizeAction, "R reload", escapeAction)
	}

	parts = append(parts, "README")
	return strings.Join(parts, " · ")
}

func (m *Model) resizePanels() {
	if idx, ok := m.visibleMaximizedPanel(); ok {
		innerW, innerH := m.maximizedPanelInnerSize()
		if innerW < 1 {
			innerW = 1
		}
		if innerH < 1 {
			innerH = 1
		}
		m.panels[idx].Resize(innerW, innerH)
		return
	}

	innerW, innerH := m.gridPanelInnerSize()
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	for _, p := range m.panels {
		p.Resize(innerW, innerH)
	}
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) panelIndexAt(x, y int) (int, bool) {
	if m.width <= 0 || m.height <= 0 || x < 0 || y < 0 || x >= m.width || y >= m.height {
		return -1, false
	}

	gh := m.gridHeight()
	if m.height > 1 && y >= gh {
		return -1, false
	}
	if idx, ok := m.visibleMaximizedPanel(); ok {
		return idx, true
	}

	cell := layout.CellSizes(m.width, gh, m.grid.Rows, m.grid.Cols)
	if cell.Width <= 0 || cell.Height <= 0 {
		return -1, false
	}

	col := x / cell.Width
	row := y / cell.Height
	if col < 0 || col >= m.grid.Cols || row < 0 || row >= m.grid.Rows {
		return -1, false
	}

	idx := row*m.grid.Cols + col
	if idx >= len(m.panels) {
		return -1, false
	}

	return idx, true
}

func (m *Model) toggleMaximized() {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	if m.maximizedPanel == m.activePanel {
		m.maximizedPanel = -1
	} else {
		m.maximizedPanel = m.activePanel
	}
	m.resizePanels()
}

func (m Model) visibleMaximizedPanel() (int, bool) {
	if m.maximizedPanel < 0 || m.maximizedPanel >= len(m.panels) {
		return -1, false
	}
	if m.activePanel != m.maximizedPanel {
		return -1, false
	}
	return m.maximizedPanel, true
}

func (m *Model) ensureScrollState() {
	n := len(m.panels)
	if len(m.scrollOffsets) != n {
		m.scrollOffsets = resizeIntSlice(m.scrollOffsets, n, 0)
	}
	if len(m.scrollSelections) != n {
		m.scrollSelections = resizeIntSlice(m.scrollSelections, n, -1)
	}
	if len(m.scrollMarks) != n {
		m.scrollMarks = resizeUint64Slice(m.scrollMarks, n)
	}
}

func resizeIntSlice(prev []int, n int, fill int) []int {
	out := make([]int, n)
	copy(out, prev)
	for i := len(prev); i < n; i++ {
		out[i] = fill
	}
	return out
}

func resizeUint64Slice(prev []uint64, n int) []uint64 {
	out := make([]uint64, n)
	copy(out, prev)
	return out
}

func (m *Model) enterScrollMode() {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	m.ensureScrollState()
	m.panelInsertMode = false
	m.panelScrollMode = true

	idx := m.activePanel
	lines := m.historyLines(m.panels[idx])
	total := len(lines)
	if total == 0 {
		m.scrollOffsets[idx] = 0
		m.scrollSelections[idx] = -1
		return
	}

	m.reconcileScrollState(idx, lines)
	if m.scrollSelections[idx] < 0 || m.scrollSelections[idx] >= total {
		m.scrollSelections[idx] = total - 1
	}
}

func (m *Model) handleScrollKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyPgUp:
		m.scrollViewportBy(-m.activePaneLineCapacity())
	case tea.KeyPgDown:
		m.scrollViewportBy(m.activePaneLineCapacity())
	case tea.KeyUp:
		m.moveSelectionBy(-1)
	case tea.KeyDown:
		m.moveSelectionBy(1)
	}

	if msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) == 1 {
		switch msg.Runes[0] {
		case 'j':
			m.moveSelectionBy(1)
		case 'k':
			m.moveSelectionBy(-1)
		case 'g':
			m.jumpSelectionTo(0)
		case 'G':
			m.jumpSelectionTo(-1)
		case 'm':
			m.toggleMark()
		}
	}
}

func (m *Model) scrollViewportBy(delta int) {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	m.ensureScrollState()

	idx := m.activePanel
	lines := m.historyLines(m.panels[idx])
	m.reconcileScrollState(idx, lines)
	if len(lines) == 0 {
		return
	}

	pageSize := max(1, m.activePaneLineCapacity())
	maxOffset := max(0, len(lines)-pageSize)
	m.scrollOffsets[idx] = clamp(m.scrollOffsets[idx]-delta, 0, maxOffset)

	start := m.viewportStart(len(lines), pageSize, m.scrollOffsets[idx])
	end := min(len(lines), start+pageSize)
	if end <= start {
		m.scrollSelections[idx] = -1
		return
	}

	if m.scrollSelections[idx] < start {
		m.scrollSelections[idx] = start
	}
	if m.scrollSelections[idx] >= end {
		m.scrollSelections[idx] = end - 1
	}
}

func (m *Model) moveSelectionBy(delta int) {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	m.ensureScrollState()

	idx := m.activePanel
	lines := m.historyLines(m.panels[idx])
	m.reconcileScrollState(idx, lines)
	if len(lines) == 0 {
		return
	}

	if m.scrollSelections[idx] < 0 || m.scrollSelections[idx] >= len(lines) {
		m.scrollSelections[idx] = len(lines) - 1
	}
	m.scrollSelections[idx] = clamp(m.scrollSelections[idx]+delta, 0, len(lines)-1)
	m.ensureSelectionVisible(idx, len(lines))
}

func (m *Model) jumpSelectionTo(target int) {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	m.ensureScrollState()

	idx := m.activePanel
	lines := m.historyLines(m.panels[idx])
	m.reconcileScrollState(idx, lines)
	if len(lines) == 0 {
		return
	}

	if target < 0 {
		target = len(lines) - 1
	}
	m.scrollSelections[idx] = clamp(target, 0, len(lines)-1)
	m.ensureSelectionVisible(idx, len(lines))
}

func (m *Model) toggleMark() {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	m.ensureScrollState()

	idx := m.activePanel
	lines := m.historyLines(m.panels[idx])
	m.reconcileScrollState(idx, lines)
	sel := m.scrollSelections[idx]
	if sel < 0 || sel >= len(lines) {
		return
	}

	if m.scrollMarks[idx] == lines[sel].ID {
		m.clearMark(idx)
		return
	}
	m.scrollMarks[idx] = lines[sel].ID
}

func (m *Model) ensureSelectionVisible(idx, total int) {
	pageSize := max(1, m.activePaneLineCapacity())
	maxOffset := max(0, total-pageSize)
	start := m.viewportStart(total, pageSize, m.scrollOffsets[idx])
	end := min(total, start+pageSize)
	sel := m.scrollSelections[idx]

	if sel < start {
		start = sel
	} else if sel >= end {
		start = sel - pageSize + 1
	}
	start = clamp(start, 0, maxOffset)
	m.scrollOffsets[idx] = maxOffset - start
}

func (m *Model) reconcileScrollState(idx int, lines []process.HistoryLine) {
	total := len(lines)
	pageSize := max(1, m.activePaneLineCapacity())
	maxOffset := max(0, total-pageSize)
	m.scrollOffsets[idx] = clamp(m.scrollOffsets[idx], 0, maxOffset)

	if total == 0 {
		m.scrollSelections[idx] = -1
		m.clearMark(idx)
		return
	}

	if m.scrollSelections[idx] >= total {
		m.scrollSelections[idx] = total - 1
	}
	if m.scrollSelections[idx] < -1 {
		m.scrollSelections[idx] = -1
	}

}

func (m *Model) viewportForPanel(idx, height int) *paneViewport {
	if !m.panelScrollMode || idx != m.activePanel {
		return nil
	}
	if idx < 0 || idx >= len(m.panels) {
		return nil
	}

	m.ensureScrollState()
	history := m.historyLines(m.panels[idx])
	pageSize := max(1, paneLineCapacity(height))
	if len(history) == 0 {
		m.reconcileScrollState(idx, history)
		return &paneViewport{Lines: make([]string, pageSize), SelectedRow: -1, MarkedRow: -1}
	}
	m.reconcileScrollState(idx, history)

	start := m.viewportStart(len(history), pageSize, m.scrollOffsets[idx])
	end := min(len(history), start+pageSize)
	selectedRow := -1
	if sel := m.scrollSelections[idx]; sel >= start && sel < end {
		selectedRow = sel - start
	}
	markedRow := -1
	if markID := m.scrollMarks[idx]; markID != 0 {
		if mark, ok := findLineIndexByID(history, markID); ok && mark >= start && mark < end {
			markedRow = mark - start
		}
	}

	viewportLines := make([]string, 0, end-start)
	for _, line := range history[start:end] {
		viewportLines = append(viewportLines, line.Text)
	}

	return &paneViewport{
		Lines:       viewportLines,
		SelectedRow: selectedRow,
		MarkedRow:   markedRow,
	}
}

func (m Model) viewportStart(total, pageSize, offset int) int {
	if total <= pageSize {
		return 0
	}
	maxOffset := total - pageSize
	offset = clamp(offset, 0, maxOffset)
	return total - pageSize - offset
}

func (m *Model) clearMark(idx int) {
	m.scrollMarks[idx] = 0
}

func findLineIndexByID(lines []process.HistoryLine, want uint64) (int, bool) {
	for i, line := range lines {
		if line.ID == want {
			return i, true
		}
	}
	return -1, false
}

func (m Model) gridPanelInnerSize() (int, int) {
	cell := layout.CellSizes(m.width, m.gridHeight(), m.grid.Rows, m.grid.Cols)
	return cell.Width - 2, cell.Height - 3
}

func (m Model) maximizedPanelInnerSize() (int, int) {
	return m.width - 2, m.gridHeight() - 3
}

func (m Model) renderStatusLine() string {
	panelName := "none"
	if m.activePanel >= 0 && m.activePanel < len(m.panels) {
		p := m.panels[m.activePanel]
		panelName = fmt.Sprintf("[%d] %s", m.activePanel+1, p.Name)
	}
	hint := m.statusHint()
	mode := m.statusModeLabel()
	modeFG := m.theme.color(m.theme.StatusModeNoneFG)
	modeBG := m.theme.color(m.theme.StatusModeNoneBG)
	switch mode {
	case "NORMAL":
		modeFG = m.theme.color(m.theme.StatusModeNormalFG)
		modeBG = m.theme.color(m.theme.StatusModeNormalBG)
	case "INSERT":
		modeFG = m.theme.color(m.theme.StatusModeInsertFG)
		modeBG = m.theme.color(m.theme.StatusModeInsertBG)
	case "SCROLL":
		modeFG = m.theme.color(m.theme.StatusModeNormalFG)
		modeBG = m.theme.color(m.theme.StatusModeNormalBG)
	}
	segments := []statusSegment{
		{Text: time.Now().Format("15:04:05"), FG: m.theme.color(m.theme.StatusTimeFG), BG: m.theme.color(m.theme.StatusTimeBG)},
		{Text: fmt.Sprintf("active panel: %s", panelName), FG: m.theme.color(m.theme.StatusActivePanelFG), BG: m.theme.color(m.theme.StatusActivePanelBG)},
		{Text: "MODE: " + mode, FG: modeFG, BG: modeBG},
		{Text: hint, FG: m.theme.color(m.theme.StatusHintFG), BG: m.theme.color(m.theme.StatusHintBG)},
	}
	return renderStatusLine(m.theme, m.width, segments)
}

func (m Model) activePaneLineCapacity() int {
	if idx, ok := m.visibleMaximizedPanel(); ok && idx == m.activePanel {
		return paneLineCapacity(m.gridHeight())
	}
	cell := layout.CellSizes(m.width, m.gridHeight(), m.grid.Rows, m.grid.Cols)
	return paneLineCapacity(cell.Height)
}

func paneLineCapacity(height int) int {
	return max(1, height-4)
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func keyMsgToBytes(msg tea.KeyMsg) []byte {
	if msg.Type == tea.KeyRunes {
		payload := []byte(string(msg.Runes))
		if msg.Alt {
			return append([]byte{0x1b}, payload...)
		}
		return payload
	}

	if msg.Type >= tea.KeyCtrlA && msg.Type <= tea.KeyCtrlZ {
		return []byte{byte(msg.Type)}
	}

	switch msg.Type {
	case tea.KeyCtrlAt:
		return []byte{0x00}
	case tea.KeyEsc:
		return []byte{0x1b}
	case tea.KeyEnter:
		return []byte{'\r'}
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeySpace:
		return []byte{' '}
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	}

	return nil
}
