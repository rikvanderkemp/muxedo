package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"muxedo/internal/layout"
	"muxedo/internal/process"
)

type tickMsg time.Time

type Model struct {
	panels           []*process.Panel
	theme            Theme
	width            int
	height           int
	grid             layout.Grid
	activePanel      int
	maximizedPanel   int
	panelInsertMode  bool   // when a panel is focused: false = normal (vim-like), true = keys go to PTY
	prevPanelRunning []bool // per-panel running state last tick; detects run→stop while focused
	afterGForTab     bool   // saw "g", expect "t"/"T" (vim :tabnext / :tabprev) when no panel focused
	vimTabClearAt    time.Time
	editor           string
	sendInput        func(*process.Panel, []byte) error
	panelRunning     func(*process.Panel) bool
	restartPanel     func(*process.Panel) error
	openEditor       func(editor, path string) tea.Cmd
}

func NewModel(panels []*process.Panel, editor string, themes ...Theme) Model {
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
		editor:         editor,
		sendInput: func(p *process.Panel, input []byte) error {
			return p.WriteInput(input)
		},
		panelRunning: func(p *process.Panel) bool {
			return p.Running()
		},
		restartPanel: func(p *process.Panel) error {
			return p.Restart()
		},
		openEditor: defaultOpenEditor,
	}
}

func defaultOpenEditor(editor, path string) tea.Cmd {
	if err := ensureFile(path); err != nil {
		return nil
	}
	c := exec.Command("sh", "-lc", fmt.Sprintf("%s %q", editor, path))
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorClosedMsg{err} })
}

func ensureFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

type editorClosedMsg struct{ err error }

func (m Model) Init() tea.Cmd {
	return tea.Batch(tick(), tea.ClearScreen)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyEsc {
			m.afterGForTab = false
			if m.activePanel >= 0 {
				if m.panelInsertMode {
					m.panelInsertMode = false
					return m, nil
				}
				m.maximizedPanel = -1
				m.activePanel = -1
				m.panelInsertMode = false
				m.resizePanels()
				return m, nil
			}
			return m, nil
		}

		if d, ok := panelSwitchDelta(msg); ok {
			m.afterGForTab = false
			m.panelInsertMode = false
			if n := len(m.panels); n > 0 {
				if m.activePanel < 0 {
					m.activePanel = 0
				} else {
					m.activePanel = (m.activePanel + d + n) % n
				}
				if m.maximizedPanel >= 0 {
					m.maximizedPanel = m.activePanel
					m.resizePanels()
				}
			}
			return m, nil
		}

		if m.activePanel < 0 && m.afterGForTab {
			if msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) == 1 {
				switch msg.Runes[0] {
				case 't', 'T':
					m.afterGForTab = false
					m.panelInsertMode = false
					if n := len(m.panels); n > 0 {
						delta := 1
						if msg.Runes[0] == 'T' {
							delta = -1
						}
						if m.activePanel < 0 {
							m.activePanel = 0
						} else {
							m.activePanel = (m.activePanel + delta + n) % n
						}
					}
					return m, nil
				}
			}
			m.afterGForTab = false
		}

		if m.activePanel < 0 && !m.afterGForTab &&
			msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) == 1 && msg.Runes[0] == 'g' {
			m.afterGForTab = true
			m.vimTabClearAt = time.Now().Add(2 * time.Second)
			return m, nil
		}

		if m.activePanel >= 0 && m.activePanel < len(m.panels) {
			p := m.panels[m.activePanel]

			if msg.Type == tea.KeyCtrlO {
				if path := p.ScrollbackPath(); path != "" {
					return m, m.openEditor(m.editor, path)
				}
				return m, nil
			}

			running := m.panelRunning(p)
			if !running {
				if m.panelInsertMode {
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
					case 's', 'S':
						if path := p.ScrollbackPath(); path != "" {
							return m, m.openEditor(m.editor, path)
						}
					}
				}
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
				case 'r', 'R':
					_ = m.restartPanel(p)
					m.resizePanels()
					return m, nil
				case 's', 'S':
					if path := p.ScrollbackPath(); path != "" {
						return m, m.openEditor(m.editor, path)
					}
					return m, nil
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			for _, p := range m.panels {
				p.Stop()
			}
			return m, tea.Quit
		}

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if idx, ok := m.panelIndexAt(msg.X, msg.Y); ok {
				m.afterGForTab = false
				m.activePanel = idx
				m.panelInsertMode = false
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

	case editorClosedMsg:
		return m, nil

	case tickMsg:
		t := time.Time(msg)
		if m.afterGForTab && !m.vimTabClearAt.IsZero() && !t.Before(m.vimTabClearAt) {
			m.afterGForTab = false
		}
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
		out := p.Output()
		stopped := !m.panelRunning(p)
		body := renderPane(
			m.theme,
			p.Name,
			out,
			m.width,
			gh,
			idx == m.activePanel,
			stopped,
			m.panelInsertMode,
			formatElapsed(p.Elapsed()),
		)
		if m.height > 1 {
			body = lipgloss.JoinVertical(lipgloss.Left, body, m.renderStatusLine())
		}
		return lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, body)
	}

	cell := layout.CellSizes(m.width, gh, m.grid.Rows, m.grid.Cols)

	var gridRows []string
	idx := 0
	for r := 0; r < m.grid.Rows; r++ {
		var cols []string
		for c := 0; c < m.grid.Cols; c++ {
			if idx < len(m.panels) {
				p := m.panels[idx]
				out := p.Output()
				stopped := !m.panelRunning(p)
				pane := renderPane(
					m.theme,
					p.Name,
					out,
					cell.Width,
					cell.Height,
					idx == m.activePanel,
					stopped,
					m.panelInsertMode,
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

	return lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, body)
}

func (m Model) statusModeLabel() string {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return "NONE"
	}
	if m.panelInsertMode {
		return "INSERT"
	}
	return "NORMAL"
}

func (m Model) statusHint() string {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return "No active: gt/gT · focused: †‡ (Opt+t/T on Mac) or Meta+t · Ctrl-C quit"
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
		} else {
			parts = append(parts, "STOPPED-NORMAL: I insert", maximizeAction, "R reload", "S scrollback", escapeAction)
		}
	} else if m.panelInsertMode {
		parts = append(parts, "INSERT: Esc normal")
	} else {
		parts = append(parts, "NORMAL: I insert", maximizeAction, "R reload", "S scrollback", escapeAction)
	}

	parts = append(parts, "†‡ next/prev", "README")
	if p.ScrollbackPath() != "" {
		parts = append(parts, "Ctrl+O scrollback")
	}
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

// panelSwitchDelta maps global panel shortcuts. Meta+bracket is usually ESC + '[' / ']',
// reported as KeyRunes with Alt. Ctrl+] is next; Alt+Ctrl+] is previous (Ctrl+[ is the
// same byte as Esc in terminals, so it cannot mean “prev” without breaking Esc).
//
// Alt+Ctrl+Left/Right are the xterm-style CSI sequences \e[1;7D / \e[1;7C. Bubble Tea has no
// separate “Command” bit; map Ctrl+Cmd+arrows in your terminal to those sequences and they
// work here (on macOS, Option+Ctrl+arrows often sends them already).
//
// macOS Option+t / Option+Shift+t insert † / ‡ (U+2020 / U+2021) with no Meta bit; those
// runes are treated like vim gt / gT so panel cycling works without leaking into the PTY.
func panelSwitchDelta(msg tea.KeyMsg) (delta int, ok bool) {
	if msg.Type == tea.KeyCtrlCloseBracket {
		if msg.Alt {
			return -1, true
		}
		return 1, true
	}
	if msg.Type == tea.KeyCtrlLeft && msg.Alt {
		return -1, true
	}
	if msg.Type == tea.KeyCtrlRight && msg.Alt {
		return 1, true
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		r := msg.Runes[0]
		// macOS US keyboard: Option+t → †, Option+Shift+t → ‡ (not Esc+t), so no Meta bit.
		switch r {
		case '\u2020': // † — same keys as vim "gt" next-tab mnemonic on Mac
			return 1, true
		case '\u2021': // ‡ — same as vim "gT" prev tab
			return -1, true
		}
		if msg.Alt {
			switch r {
			case '[':
				return -1, true
			case ']':
				return 1, true
			case 't':
				return 1, true // vim :tabnext / gt (terminals that send Meta+t)
			case 'T':
				return -1, true // vim :tabprev / gT
			}
		}
	}
	return 0, false
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
		panelName = m.panels[m.activePanel].Name
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
	}
	segments := []statusSegment{
		{Text: time.Now().Format("15:04:05"), FG: m.theme.color(m.theme.StatusTimeFG), BG: m.theme.color(m.theme.StatusTimeBG)},
		{Text: fmt.Sprintf("active panel: %q", panelName), FG: m.theme.color(m.theme.StatusActivePanelFG), BG: m.theme.color(m.theme.StatusActivePanelBG)},
		{Text: "MODE: " + mode, FG: modeFG, BG: modeBG},
		{Text: hint, FG: m.theme.color(m.theme.StatusHintFG), BG: m.theme.color(m.theme.StatusHintBG)},
	}
	return renderStatusLine(m.theme, m.width, segments)
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
