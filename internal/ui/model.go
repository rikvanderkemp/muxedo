// SPDX-License-Identifier: MIT
package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"muxedo/internal/layout"
	"muxedo/internal/process"
	"muxedo/internal/profile"
)

type tickMsg time.Time

type LogMsg string

type StartupCompleteMsg struct {
	panels []*process.Panel
}

type startupStatus string
type selectSource uint8

const (
	startupStatusPending startupStatus = "pending"
	startupStatusRunning startupStatus = "running"
	startupStatusOK      startupStatus = "ok"
	startupStatusError   startupStatus = "error"

	selectSourceLive selectSource = iota
	selectSourceHistory
)

type startupItem struct {
	Label       string
	Mode        profile.StartupMode
	Status      startupStatus
	Spinner     int
	ExitCode    int
	HasExitCode bool
	ErrorText   string
}

type startupStatusMsg struct {
	idx         int
	status      startupStatus
	exitCode    int
	hasExitCode bool
	errText     string
}

type startupLogMsg struct {
	idx  int
	line string
}

type exitProgressMsg struct {
	panelIdx int
	status   string
}

type panelSelection struct {
	Active         bool
	Dragging       bool
	Source         selectSource
	ReturnToScroll bool
	StartRow       int
	StartCol       int
	EndRow         int
	EndCol         int
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
	panelSelectMode  bool
	prevPanelRunning []bool // per-panel running state last tick; detects run→stop while focused
	scrollOffsets    []int
	scrollSelections []int
	scrollMarks      []uint64
	selections       []panelSelection
	sendInput        func(*process.Panel, []byte) error
	panelRunning     func(*process.Panel) bool
	restartPanel     func(*process.Panel) error
	historyLines     func(*process.Panel) []process.HistoryLine
	displayForView   func(*process.Panel) string
	copySelection    func(string) error
	exiting          bool
	exitStatuses     []string
	exitCompleted    int

	killingPanel    bool
	killingPanelIdx int
	killStatus      string

	messageBuffer    []string
	showBuffer       bool
	startupCompleted bool
	startupSpecs     []profile.StartupSpec
	startupItems     []startupItem
	panelSpecs       []profile.PanelSpec
	scrollbackConfig profile.ScrollbackConfig
	msgChan          chan tea.Msg
}

func NewModel(panels []*process.Panel, themes ...Theme) Model {
	theme := DefaultTheme()
	if len(themes) > 0 {
		theme = themes[0]
	}

	return Model{
		panels:           panels,
		theme:            theme,
		grid:             layout.Compute(len(panels)),
		activePanel:      -1,
		maximizedPanel:   -1,
		startupCompleted: true,
		scrollbackConfig: profile.ScrollbackConfig{},
		msgChan:          make(chan tea.Msg),
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
		displayForView: func(p *process.Panel) string {
			return p.DisplayForView()
		},
		copySelection: copyTextToClipboard,
	}
}

func NewModelWithSpecs(startup []profile.StartupSpec, panels []profile.PanelSpec, sb profile.ScrollbackConfig, theme Theme) Model {
	return Model{
		startupSpecs:     startup,
		startupItems:     newStartupItems(startup),
		panelSpecs:       panels,
		scrollbackConfig: sb,
		theme:            theme,
		msgChan:          make(chan tea.Msg),
		grid:             layout.Compute(len(panels)),
		activePanel:      -1,
		maximizedPanel:   -1,
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
		displayForView: func(p *process.Panel) string {
			return p.DisplayForView()
		},
		copySelection: copyTextToClipboard,
	}
}

func (m Model) Init() tea.Cmd {
	if m.startupCompleted {
		return tea.Batch(tick(), m.waitForMsg, tea.ClearScreen)
	}
	return tea.Batch(tick(), m.startupSequence, m.waitForMsg, tea.ClearScreen)
}

func (m Model) startupSequence() tea.Msg {
	go func() {
		if len(m.startupSpecs) == 0 {
			m.msgChan <- LogMsg("No startup commands specified.")
		}

		for i, spec := range m.startupSpecs {
			m.runStartupItem(i, spec)
		}

		m.msgChan <- LogMsg("--- Startup commands completed. Initializing panels...")

		panels := make([]*process.Panel, len(m.panelSpecs))
		for i, spec := range m.panelSpecs {
			p := process.NewWithScrollbackCommandSpec(spec.Name, spec.Command, spec.KillCommand, spec.WorkingDir, m.scrollbackConfig.Dir, m.scrollbackConfig.MaxBytes)
			p.ResetScrollback()
			if err := p.Start(); err != nil {
				m.msgChan <- LogMsg(fmt.Sprintf("error: starting panel %s: %v", p.Name, err))
			}
			panels[i] = p
		}

		m.msgChan <- StartupCompleteMsg{panels: panels}
	}()
	return nil
}

func (m Model) waitForMsg() tea.Msg {
	return <-m.msgChan
}

func newStartupItems(specs []profile.StartupSpec) []startupItem {
	items := make([]startupItem, 0, len(specs))
	for _, spec := range specs {
		items = append(items, startupItem{
			Label:  describeCommand(spec.Command),
			Mode:   spec.Mode,
			Status: startupStatusPending,
		})
	}
	return items
}

func (m Model) runStartupItem(idx int, spec profile.StartupSpec) {
	label := describeCommand(spec.Command)
	m.msgChan <- startupStatusMsg{idx: idx, status: startupStatusRunning}
	m.msgChan <- LogMsg(fmt.Sprintf("--- Starting %s (%s)", label, spec.Mode))

	cmd, stdout, stderr, err := buildStartupCommand(spec)
	if err != nil {
		m.msgChan <- startupStatusMsg{
			idx:     idx,
			status:  startupStatusError,
			errText: err.Error(),
		}
		m.msgChan <- LogMsg(fmt.Sprintf("error: could not prepare %q: %v", label, err))
		return
	}

	done := make(chan struct{}, 2)
	go m.streamStartupOutput(idx, stdout, done)
	go m.streamStartupOutput(idx, stderr, done)

	if err := cmd.Start(); err != nil {
		m.msgChan <- startupStatusMsg{
			idx:     idx,
			status:  startupStatusError,
			errText: fmt.Sprintf("start failed: %v", err),
		}
		m.msgChan <- LogMsg(fmt.Sprintf("error: could not start %q: %v", label, err))
		return
	}

	waitAndReport := func() {
		err := cmd.Wait()
		<-done
		<-done
		statusMsg := startupStatusMsg{
			idx:         idx,
			status:      startupStatusOK,
			exitCode:    0,
			hasExitCode: true,
		}
		if err != nil {
			statusMsg.status = startupStatusError
			statusMsg.exitCode, statusMsg.hasExitCode, statusMsg.errText = commandExitDetails(err)
			m.msgChan <- LogMsg(fmt.Sprintf("error: command %q exited: %v", label, err))
		}
		m.msgChan <- statusMsg
	}

	if spec.Mode == profile.StartupModeSync {
		waitAndReport()
		return
	}

	go waitAndReport()
}

func buildStartupCommand(spec profile.StartupSpec) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error) {
	cmd, err := spec.Command.Build(spec.WorkingDir, false)
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("capture stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("capture stderr: %w", err)
	}
	return cmd, stdout, stderr, nil
}

func (m Model) streamStartupOutput(idx int, r io.Reader, done chan<- struct{}) {
	defer func() {
		done <- struct{}{}
	}()

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		m.msgChan <- startupLogMsg{idx: idx, line: scanner.Text()}
	}
	if err := scanner.Err(); err != nil {
		m.msgChan <- startupLogMsg{idx: idx, line: fmt.Sprintf("error: scanning output: %v", err)}
	}
}

func commandExitDetails(err error) (exitCode int, hasExitCode bool, errText string) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true, ""
	}
	return 0, false, err.Error()
}

func describeCommand(cmd process.CommandSpec) string {
	if cmd.Shell != "" {
		return cmd.Shell
	}
	if len(cmd.Args) == 0 {
		return cmd.Program
	}
	return cmd.Program + " " + strings.Join(cmd.Args, " ")
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case LogMsg:
		m.messageBuffer = append(m.messageBuffer, string(msg))
		return m, m.waitForMsg
	case startupLogMsg:
		m.messageBuffer = append(m.messageBuffer, m.formatStartupLog(msg.idx, msg.line))
		return m, m.waitForMsg
	case startupStatusMsg:
		if msg.idx >= 0 && msg.idx < len(m.startupItems) {
			item := m.startupItems[msg.idx]
			item.Status = msg.status
			item.HasExitCode = msg.hasExitCode
			item.ExitCode = msg.exitCode
			item.ErrorText = msg.errText
			if msg.status != startupStatusRunning {
				item.Spinner = 0
			}
			m.startupItems[msg.idx] = item
		}
		return m, m.waitForMsg
	case StartupCompleteMsg:
		m.panels = msg.panels
		m.startupCompleted = true
		m.showBuffer = false
		m.resizePanels()
		return m, m.waitForMsg
	}

	if m.killingPanel {
		switch msg := msg.(type) {
		case exitProgressMsg:
			if msg.panelIdx == m.killingPanelIdx {
				m.killStatus = msg.status
				// We don't immediately return m, nil here because we want to show the "exiting completed" state briefly
				// But according to the plan, we deactivate the window.
				// Let's stick to the plan for now: deactivate and stop showing dialog.
				m.killingPanel = false
				m.killingPanelIdx = -1
				m.activePanel = -1
				return m, nil
			}
		}
		// Ignore other messages while killing a panel
		return m, nil
	}

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
		if msg.String() == "ctrl+b" {
			m.showBuffer = !m.showBuffer
			return m, nil
		}

		if msg.Type == tea.KeyEsc {
			if m.activePanel >= 0 {
				if m.panelSelectMode {
					m.exitSelectMode()
					return m, nil
				}
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
				m.panelSelectMode = false
				m.clearActiveSelection()
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

			if !m.panelInsertMode && !m.panelScrollMode && !m.panelSelectMode &&
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
					if msg.String() == "ctrl+c" {
						return m, nil
					}
					return m, nil
				}
				if m.panelSelectMode {
					if m.handleSelectKey(msg) {
						return m, nil
					}
					return m, nil
				}
				if m.panelScrollMode {
					m.handleScrollKey(msg)
					return m, nil
				}
				if msg.Type == tea.KeyRunes && (string(msg.Runes) == "r" || string(msg.Runes) == "R") {
					if err := m.restartPanel(p); err != nil {
						go func() { m.msgChan <- LogMsg(fmt.Sprintf("error: reloading panel %s: %v", p.Name, err)) }()
					}
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
					case 'v', 'V':
						m.enterSelectMode()
					case 'x', 'X':
						m.killingPanel = true
						m.killingPanelIdx = m.activePanel
						m.killStatus = fmt.Sprintf("exiting panel %s....", p.Name)
						return m, killPanelCmd(m.activePanel, p)
					}
				}
				return m, nil
			}

			if m.panelScrollMode {
				if msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) == 1 {
					switch msg.Runes[0] {
					case 'v', 'V':
						m.enterSelectMode()
						return m, nil
					}
				}
				m.handleScrollKey(msg)
				return m, nil
			}

			if m.panelSelectMode {
				if m.handleSelectKey(msg) {
					return m, nil
				}
				return m, nil
			}

			if m.panelInsertMode {
				if msg.String() == "ctrl+c" {
					return m, nil
				}
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
				case 'v', 'V':
					m.enterSelectMode()
					return m, nil
				case 'r', 'R':
					if err := m.restartPanel(p); err != nil {
						go func() { m.msgChan <- LogMsg(fmt.Sprintf("error: reloading panel %s: %v", p.Name, err)) }()
					}
					m.resizePanels()
					return m, nil
				case 'x', 'X':
					m.killingPanel = true
					m.killingPanelIdx = m.activePanel
					m.killStatus = fmt.Sprintf("exiting panel %s....", p.Name)
					return m, killPanelCmd(m.activePanel, p)
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
		if m.panelSelectMode && m.activePanel >= 0 {
			if idx, ok := m.panelIndexAt(msg.X, msg.Y); ok && idx == m.activePanel {
				if row, col, ok := m.panelContentPoint(idx, msg.X, msg.Y); ok {
					switch msg.Action {
					case tea.MouseActionPress:
						if msg.Button == tea.MouseButtonLeft {
							m.startSelection(row, col)
							return m, nil
						}
					case tea.MouseActionMotion:
						if msg.Button == tea.MouseButtonLeft {
							m.updateSelection(row, col)
							return m, nil
						}
					case tea.MouseActionRelease:
						if msg.Button == tea.MouseButtonLeft {
							m.finishSelection(row, col)
							return m, nil
						}
					}
				}
			}
		}
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
				m.applyPanelFocus(idx)
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizePanels()
		return m, tea.ClearScreen

	case tickMsg:
		for i := range m.startupItems {
			if m.startupItems[i].Status == startupStatusRunning {
				m.startupItems[i].Spinner = (m.startupItems[i].Spinner + 1) % len(startupSpinnerFrames)
			}
		}
		n := len(m.panels)
		if len(m.prevPanelRunning) != n {
			m.prevPanelRunning = make([]bool, n)
			for i := range m.panels {
				m.prevPanelRunning[i] = m.panelRunning(m.panels[i])
			}
		} else {
			for i := range m.panels {
				p := m.panels[i]
				now := m.panelRunning(p)
				if m.prevPanelRunning[i] && !now {
					if err := p.ExitError(); err != nil {
						go func() { m.msgChan <- LogMsg(fmt.Sprintf("error: panel %s exited: %v", p.Name, err)) }()
					}
					if i == m.activePanel {
						m.maximizedPanel = -1
						m.activePanel = -1
						m.panelInsertMode = false
						m.panelScrollMode = false
						m.panelSelectMode = false
						m.clearActiveSelection()
						m.resizePanels()
					}
				}
				m.prevPanelRunning[i] = now
			}
		}
		return m, tick()
	}

	return m, nil
}

func (m Model) renderMessageBuffer() string {
	gh := m.gridHeight()
	innerH := gh - 2
	if innerH < 0 {
		innerH = 0
	}

	lines := m.renderMessageBufferLines(innerH)
	content := strings.Join(lines, "\n")
	title := " MESSAGE BUFFER (ctrl+b to toggle) "
	if !m.startupCompleted {
		title = " STARTING MUXEDO... "
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.color(m.theme.ActiveNormalBorder)).
		Width(m.width - 2).
		Height(gh - 2).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Bold(true).Foreground(m.theme.color(m.theme.StatusModeNormalFG)).Render(title),
			content,
		))
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

	if !m.startupCompleted || m.showBuffer {
		body := m.renderMessageBuffer()
		if m.height > 1 {
			body = lipgloss.JoinVertical(lipgloss.Left, body, m.renderStatusLine())
		}
		return lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, body)
	}

	gh := m.gridHeight()
	if idx, ok := m.visibleMaximizedPanel(); ok {
		p := m.panels[idx]
		out := m.displayForView(p)
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
			m.panelSelectMode,
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
				out := m.displayForView(p)
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
					m.panelSelectMode && idx == m.activePanel,
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
	if !m.exiting && !m.killingPanel {
		return body
	}

	content := strings.Join(m.exitStatuses, "\n")
	if m.killingPanel {
		content = m.killStatus
	}

	dialogBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 3).
		Align(lipgloss.Center).
		Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialogBox)
}

func (m Model) statusModeLabel() string {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return "NONE"
	}
	if m.panelInsertMode {
		return "INSERT"
	}
	if m.panelSelectMode {
		return "SELECT"
	}
	if m.panelScrollMode {
		return "SCROLL"
	}
	return "NORMAL"
}

func (m Model) statusHint() string {
	if !m.startupCompleted {
		return "STARTING... · Startup status and logs"
	}
	if m.showBuffer {
		return "BUFFER: Ctrl-B toggle back · Startup status and logs"
	}

	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return "No active: 1–9 focus · NORMAL: hjkl panes · Ctrl-B buffer · Ctrl-C quit"
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
		} else if m.panelSelectMode {
			parts = append(parts, "SELECT: drag mouse", "Y/Enter copy", "Esc back")
		} else if m.panelScrollMode {
			parts = append(parts, "SCROLL: PgUp/PgDn wheel", "J/K move", "M mark", "Esc normal")
		} else {
			parts = append(parts, "STOPPED-NORMAL: I insert", "Z scroll", "V select", "1–9 hjkl panes", maximizeAction, "R reload", "X stop", escapeAction)
		}
	} else if m.panelInsertMode {
		parts = append(parts, "INSERT: Esc normal")
	} else if m.panelSelectMode {
		parts = append(parts, "SELECT: drag mouse", "Y/Enter copy", "Esc back")
	} else if m.panelScrollMode {
		parts = append(parts, "SCROLL: PgUp/PgDn wheel", "J/K move", "M mark", "V select", "G live", "Esc normal")
	} else {
		parts = append(parts, "NORMAL: I insert", "Z scroll", "V select", "1–9 hjkl panes", maximizeAction, "R reload", "X stop", "Ctrl-B buffer", escapeAction)
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
	if len(m.selections) != n {
		m.selections = resizeSelectionSlice(m.selections, n)
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

var startupSpinnerFrames = []string{"-", "\\", "|", "/"}

func (m Model) formatStartupLog(idx int, line string) string {
	if idx < 0 || idx >= len(m.startupItems) {
		return line
	}
	return fmt.Sprintf("[%s] %s", m.startupItems[idx].Label, line)
}

func (m Model) renderMessageBufferLines(innerH int) []string {
	if innerH <= 0 {
		return nil
	}

	lines := make([]string, 0, innerH)
	statusLines := m.renderStartupStatusLines()
	if len(statusLines) > 0 {
		if len(statusLines) > innerH {
			return statusLines[len(statusLines)-innerH:]
		}
		lines = append(lines, statusLines...)
	}

	remaining := innerH - len(lines)
	if remaining <= 0 {
		return lines
	}
	if len(lines) > 0 {
		lines = append(lines, "")
		remaining--
	}
	if remaining <= 0 {
		return lines
	}

	logs := m.messageBuffer
	if len(logs) > remaining {
		logs = logs[len(logs)-remaining:]
	}
	lines = append(lines, logs...)
	return lines
}

func (m Model) renderStartupStatusLines() []string {
	if len(m.startupItems) == 0 {
		return nil
	}
	lines := make([]string, 0, len(m.startupItems)+1)
	lines = append(lines, "Startup status:")
	for _, item := range m.startupItems {
		lines = append(lines, formatStartupStatusLine(item))
	}
	return lines
}

func formatStartupStatusLine(item startupItem) string {
	prefix := fmt.Sprintf("Starting %s [%s]", item.Label, item.Mode)
	switch item.Status {
	case startupStatusOK:
		return fmt.Sprintf("%s -> OK (%d)", prefix, item.ExitCode)
	case startupStatusError:
		if item.HasExitCode {
			return fmt.Sprintf("%s -> ERROR (%d)", prefix, item.ExitCode)
		}
		if item.ErrorText != "" {
			return fmt.Sprintf("%s -> ERROR (%s)", prefix, item.ErrorText)
		}
		return prefix + " -> ERROR"
	case startupStatusRunning:
		return fmt.Sprintf("%s -> %s", prefix, startupSpinnerFrames[item.Spinner%len(startupSpinnerFrames)])
	default:
		return prefix + " -> queued"
	}
}

func resizeUint64Slice(prev []uint64, n int) []uint64 {
	out := make([]uint64, n)
	copy(out, prev)
	return out
}

func resizeSelectionSlice(prev []panelSelection, n int) []panelSelection {
	out := make([]panelSelection, n)
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

func (m *Model) enterSelectMode() {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	m.ensureScrollState()
	idx := m.activePanel
	sel := panelSelection{
		Source:         selectSourceLive,
		ReturnToScroll: m.panelScrollMode,
		StartRow:       0,
		StartCol:       0,
		EndRow:         0,
		EndCol:         0,
	}
	if m.panelScrollMode {
		sel.Source = selectSourceHistory
	}
	m.selections[idx] = sel
	m.panelInsertMode = false
	m.panelSelectMode = true
}

func (m *Model) exitSelectMode() {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		m.panelSelectMode = false
		return
	}
	idx := m.activePanel
	returnToScroll := m.selections[idx].ReturnToScroll
	m.selections[idx] = panelSelection{}
	m.panelSelectMode = false
	m.panelScrollMode = returnToScroll
}

func (m *Model) clearActiveSelection() {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	m.ensureScrollState()
	m.selections[m.activePanel] = panelSelection{}
}

func (m *Model) handleSelectKey(msg tea.KeyMsg) bool {
	if msg.Type == tea.KeyEnter {
		m.copyCurrentSelection()
		return true
	}
	if msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) == 1 {
		switch msg.Runes[0] {
		case 'y', 'Y':
			m.copyCurrentSelection()
			return true
		}
	}
	return false
}

func (m *Model) startSelection(row, col int) {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	m.ensureScrollState()
	idx := m.activePanel
	sel := m.selections[idx]
	sel.Active = true
	sel.Dragging = true
	sel.StartRow = row
	sel.StartCol = col
	sel.EndRow = row
	sel.EndCol = col
	m.selections[idx] = sel
}

func (m *Model) updateSelection(row, col int) {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	idx := m.activePanel
	sel := m.selections[idx]
	if !sel.Dragging {
		return
	}
	sel.Active = true
	sel.EndRow = row
	sel.EndCol = col
	m.selections[idx] = sel
}

func (m *Model) finishSelection(row, col int) {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	idx := m.activePanel
	sel := m.selections[idx]
	if !sel.Dragging {
		return
	}
	sel.Active = true
	sel.Dragging = false
	sel.EndRow = row
	sel.EndCol = col
	m.selections[idx] = sel
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
	if idx != m.activePanel {
		return nil
	}
	if idx < 0 || idx >= len(m.panels) {
		return nil
	}

	m.ensureScrollState()
	pageSize := max(1, paneLineCapacity(height))
	if m.panelSelectMode {
		return m.selectViewportForPanel(idx, pageSize)
	}
	if !m.panelScrollMode {
		return nil
	}
	return m.historyViewportForPanel(idx, pageSize)
}

func (m *Model) historyViewportForPanel(idx, pageSize int) *paneViewport {
	history := m.historyLines(m.panels[idx])
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
		PlainLines:  append([]string(nil), viewportLines...),
		SelectedRow: selectedRow,
		MarkedRow:   markedRow,
	}
}

func (m *Model) selectViewportForPanel(idx, pageSize int) *paneViewport {
	lines, plainLines := m.selectionLinesForPanel(idx, pageSize)
	vp := &paneViewport{
		Lines:       lines,
		PlainLines:  plainLines,
		SelectedRow: -1,
		MarkedRow:   -1,
	}
	if idx >= len(m.selections) {
		return vp
	}
	sel := m.selections[idx]
	if !sel.Active {
		return vp
	}
	startRow, startCol, endRow, endCol := normalizeSelection(sel.StartRow, sel.StartCol, sel.EndRow, sel.EndCol)
	startRow = clamp(startRow, 0, max(0, len(lines)-1))
	endRow = clamp(endRow, 0, max(0, len(lines)-1))
	width := m.activePaneContentWidth()
	if width <= 0 {
		width = 1
	}
	startCol = clamp(startCol, 0, width-1)
	endCol = clamp(endCol, 0, width-1)

	vp.SelectionActive = true
	vp.SelectionStartRow = startRow
	vp.SelectionStartCol = startCol
	vp.SelectionEndRow = endRow
	vp.SelectionEndCol = endCol
	return vp
}

func (m *Model) selectionLinesForPanel(idx, pageSize int) ([]string, []string) {
	if idx < 0 || idx >= len(m.panels) {
		return nil, nil
	}
	width := m.activePaneContentWidth()
	if width <= 0 {
		width = 1
	}
	if idx < len(m.selections) && m.selections[idx].Source == selectSourceHistory {
		history := m.historyLines(m.panels[idx])
		if len(history) == 0 {
			lines := make([]string, pageSize)
			return lines, append([]string(nil), lines...)
		}
		m.reconcileScrollState(idx, history)
		start := m.viewportStart(len(history), pageSize, m.scrollOffsets[idx])
		end := min(len(history), start+pageSize)
		lines := make([]string, 0, pageSize)
		for _, line := range history[start:end] {
			lines = append(lines, padOrTruncate(line.Text, width))
		}
		for len(lines) < pageSize {
			lines = append(lines, strings.Repeat(" ", width))
		}
		return lines, append([]string(nil), lines...)
	}

	raw := m.displayForView(m.panels[idx])
	return fitLines(raw, pageSize, width), fitLines(ansi.Strip(raw), pageSize, width)
}

func (m Model) activePaneContentWidth() int {
	if idx, ok := m.visibleMaximizedPanel(); ok && idx == m.activePanel {
		width, _ := m.maximizedPanelInnerSize()
		return width
	}
	width, _ := m.gridPanelInnerSize()
	return width
}

func (m Model) panelFrame(idx int) (x, y, width, height int, ok bool) {
	if idx < 0 || idx >= len(m.panels) || m.width <= 0 || m.height <= 0 {
		return 0, 0, 0, 0, false
	}
	gh := m.gridHeight()
	if visible, ok := m.visibleMaximizedPanel(); ok && visible == idx {
		return 0, 0, m.width, gh, true
	}
	cell := layout.CellSizes(m.width, gh, m.grid.Rows, m.grid.Cols)
	if cell.Width <= 0 || cell.Height <= 0 {
		return 0, 0, 0, 0, false
	}
	row := idx / m.grid.Cols
	col := idx % m.grid.Cols
	return col * cell.Width, row * cell.Height, cell.Width, cell.Height, true
}

func (m Model) panelContentPoint(idx, x, y int) (row, col int, ok bool) {
	px, py, width, height, ok := m.panelFrame(idx)
	if !ok {
		return 0, 0, false
	}
	contentWidth := max(1, width-2)
	contentRows := paneLineCapacity(height)
	contentX := px + 1
	contentY := py + 2
	if x < contentX || x >= contentX+contentWidth || y < contentY || y >= contentY+contentRows {
		return 0, 0, false
	}
	return y - contentY, x - contentX, true
}

func (m *Model) copyCurrentSelection() {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	text := m.currentSelectionText()
	if text == "" {
		go func() { m.msgChan <- LogMsg("warning: selection is empty") }()
		return
	}
	if err := m.copySelection(text); err != nil {
		go func() { m.msgChan <- LogMsg(fmt.Sprintf("error: copying selection: %v", err)) }()
		return
	}
	go func() { m.msgChan <- LogMsg("Copied panel selection to clipboard.") }()
}

func (m *Model) currentSelectionText() string {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return ""
	}
	m.ensureScrollState()
	idx := m.activePanel
	if idx >= len(m.selections) {
		return ""
	}
	sel := m.selections[idx]
	if !sel.Active {
		return ""
	}
	_, lines := m.selectionLinesForPanel(idx, m.activePaneLineCapacity())
	if len(lines) == 0 {
		return ""
	}
	startRow, startCol, endRow, endCol := normalizeSelection(sel.StartRow, sel.StartCol, sel.EndRow, sel.EndCol)
	startRow = clamp(startRow, 0, len(lines)-1)
	endRow = clamp(endRow, 0, len(lines)-1)
	width := m.activePaneContentWidth()
	if width <= 0 {
		width = 1
	}
	startCol = clamp(startCol, 0, width-1)
	endCol = clamp(endCol, 0, width-1)

	selected := make([]string, 0, endRow-startRow+1)
	for row := startRow; row <= endRow; row++ {
		lineStart := 0
		lineEnd := width
		if row == startRow {
			lineStart = startCol
		}
		if row == endRow {
			lineEnd = endCol + 1
		}
		selected = append(selected, strings.TrimRight(sliceByCells(lines[row], lineStart, lineEnd), " "))
	}
	return strings.Join(selected, "\n")
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
	case "SELECT":
		modeFG = m.theme.color(m.theme.StatusModeNormalFG)
		modeBG = m.theme.color(m.theme.StatusModeNormalBG)
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
