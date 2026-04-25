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

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/rikvanderkemp/muxedo/internal/layout"
	"github.com/rikvanderkemp/muxedo/internal/process"
	"github.com/rikvanderkemp/muxedo/internal/profile"
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

type scrollState struct {
	Offset         int
	SelectedLineID uint64
	SelectedLineKey uint64
	SelectedIndex  int
	Marks          []scrollMark
	Tracker        lineTracker
}

type scrollMark struct {
	LineKey   uint64
	LineID    uint64
	Text      string
	PlainText string
	CreatedAt time.Time
}

type lineTracker struct {
	NextKey uint64
	Lines   []trackedLine
}

type trackedLine struct {
	Key          uint64
	HistoryID    uint64
	Text         string
	PlainText    string
	HistoryIndex int
}

const maxStartupLogLineBytes = 1 << 20 // 1 MiB
const msgChanBufferSize = 256
const msgSendTimeout = 100 * time.Millisecond

func newMsgChan() chan tea.Msg {
	return make(chan tea.Msg, msgChanBufferSize)
}

func killPanelCmd(idx int, p *process.Panel) tea.Cmd {
	return func() tea.Msg {
		status := fmt.Sprintf("exiting panel %s.... exiting completed...", p.Name)
		if err := p.RunCmdKill(); err != nil {
			status = fmt.Sprintf("exiting panel %s.... kill command failed: %v. exiting completed...", p.Name, err)
		}
		p.Stop()
		return exitProgressMsg{
			panelIdx: idx,
			status:   status,
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
	scrollStates     []scrollState
	selections       []panelSelection
	sendInput        func(*process.Panel, []byte) error
	panelRunning     func(*process.Panel) bool
	restartPanel     func(*process.Panel) error
	historyLines     func(*process.Panel) []process.HistoryLine
	displayForView   func(*process.Panel) process.DisplayState
	copySelection    func(string) error
	exiting          bool
	exitStatuses     []string
	exitCompleted    int

	killingPanel    bool
	killingPanelIdx int
	killStatus      string

	messageBuffer    []string
	showBuffer       bool
	title            string
	startupCompleted bool
	startupSpecs     []profile.StartupSpec
	startupItems     []startupItem
	startupSpinner   spinner.Model
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
		msgChan:          newMsgChan(),
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
		displayForView: func(p *process.Panel) process.DisplayState {
			return p.DisplayState()
		},
		copySelection: copyTextToClipboard,
	}
}

func NewModelWithSpecs(title string, startup []profile.StartupSpec, panels []profile.PanelSpec, sb profile.ScrollbackConfig, theme Theme) Model {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))
	s.Style = lipgloss.NewStyle().Foreground(theme.color(theme.StatusModeNormalFG))

	return Model{
		title:            title,
		startupSpecs:     startup,
		startupItems:     newStartupItems(startup),
		startupSpinner:   s,
		panelSpecs:       panels,
		scrollbackConfig: sb,
		theme:            theme,
		msgChan:          newMsgChan(),
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
		displayForView: func(p *process.Panel) process.DisplayState {
			return p.DisplayState()
		},
		copySelection: copyTextToClipboard,
	}
}

func (m Model) emitMsg(msg tea.Msg) bool {
	select {
	case m.msgChan <- msg:
		return true
	case <-time.After(msgSendTimeout):
		return false
	}
}

func (m Model) emitMsgBlocking(msg tea.Msg) {
	m.msgChan <- msg
}

func (m Model) Init() tea.Cmd {
	windowTitle := "Muxedo"
	if m.title != "" {
		windowTitle = "Muxedo - " + m.title
	}

	if m.startupCompleted {
		return tea.Batch(tick(), m.waitForMsg, tea.ClearScreen, tea.SetWindowTitle(windowTitle))
	}
	return tea.Batch(tick(), m.startupSequence, m.waitForMsg, tea.ClearScreen, tea.SetWindowTitle(windowTitle), m.startupSpinner.Tick)
}

func (m Model) startupSequence() tea.Msg {
	go func() {
		if len(m.startupSpecs) == 0 {
			m.emitMsg(LogMsg("No startup commands specified."))
		}

		for i, spec := range m.startupSpecs {
			m.runStartupItem(i, spec)
		}

		m.emitMsg(LogMsg("--- Startup commands completed. Initializing panels..."))

		panels := make([]*process.Panel, len(m.panelSpecs))
		for i, spec := range m.panelSpecs {
			p := process.NewWithScrollbackCommandSpec(spec.Name, spec.Command, spec.KillCommand, spec.WorkingDir, m.scrollbackConfig.Dir, m.scrollbackConfig.MaxBytes)
			p.ResetScrollback()
			if err := p.Start(); err != nil {
				m.emitMsg(LogMsg(fmt.Sprintf("error: starting panel %s: %v", p.Name, err)))
			}
			panels[i] = p
		}

		m.emitMsgBlocking(StartupCompleteMsg{panels: panels})
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
	m.emitMsgBlocking(startupStatusMsg{idx: idx, status: startupStatusRunning})
	m.emitMsg(LogMsg(fmt.Sprintf("--- Starting %s (%s)", label, spec.Mode)))

	cmd, stdout, stderr, err := buildStartupCommand(spec)
	if err != nil {
		m.emitMsgBlocking(startupStatusMsg{
			idx:     idx,
			status:  startupStatusError,
			errText: err.Error(),
		})
		m.emitMsg(LogMsg(fmt.Sprintf("error: could not prepare %q: %v", label, err)))
		return
	}

	done := make(chan struct{}, 2)
	go m.streamStartupOutput(idx, stdout, done)
	go m.streamStartupOutput(idx, stderr, done)

	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		<-done
		<-done
		m.emitMsgBlocking(startupStatusMsg{
			idx:     idx,
			status:  startupStatusError,
			errText: fmt.Sprintf("start failed: %v", err),
		})
		m.emitMsg(LogMsg(fmt.Sprintf("error: could not start %q: %v", label, err)))
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
			m.emitMsg(LogMsg(fmt.Sprintf("error: command %q exited: %v", label, err)))
		}
		m.emitMsgBlocking(statusMsg)
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
	scanner.Buffer(make([]byte, 0, 64*1024), maxStartupLogLineBytes)
	for scanner.Scan() {
		m.emitMsg(startupLogMsg{idx: idx, line: scanner.Text()})
	}
	if err := scanner.Err(); err != nil {
		m.emitMsg(startupLogMsg{idx: idx, line: fmt.Sprintf("error: scanning output: %v", err)})
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
			m.startupItems[msg.idx] = item
		}
		return m, m.waitForMsg
	case StartupCompleteMsg:
		m.panels = msg.panels
		m.startupCompleted = true
		m.showBuffer = false
		m.resizePanels()
		return m, m.waitForMsg
	case spinner.TickMsg:
		if m.startupCompleted {
			return m, nil
		}
		var cmd tea.Cmd
		m.startupSpinner, cmd = m.startupSpinner.Update(msg)
		return m, cmd
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
						m.messageBuffer = append(m.messageBuffer, fmt.Sprintf("error: reloading panel %s: %v", p.Name, err))
					}
					m.clearScrollState(m.activePanel)
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
					if err := m.sendInput(p, input); err != nil {
						m.messageBuffer = append(m.messageBuffer, fmt.Sprintf("error: sending input to panel %s: %v", p.Name, err))
					}
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
						m.messageBuffer = append(m.messageBuffer, fmt.Sprintf("error: reloading panel %s: %v", p.Name, err))
					}
					m.clearScrollState(m.activePanel)
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
				case tea.MouseButtonLeft:
					if row, _, ok := m.panelContentPoint(idx, msg.X, msg.Y); ok {
						m.selectScrollLineAtViewportRow(row)
						return m, nil
					}
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
						m.messageBuffer = append(m.messageBuffer, fmt.Sprintf("error: panel %s exited: %v", p.Name, err))
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
	if len(m.scrollStates) != n {
		m.scrollStates = resizeScrollStateSlice(m.scrollStates, n)
	}
	if len(m.selections) != n {
		m.selections = resizeSelectionSlice(m.selections, n)
	}
}

func resizeScrollStateSlice(prev []scrollState, n int) []scrollState {
	out := make([]scrollState, n)
	copy(out, prev)
	for i := len(prev); i < n; i++ {
		out[i].SelectedIndex = -1
	}
	return out
}

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
	runningSpinner := m.startupSpinner.View()
	for _, item := range m.startupItems {
		lines = append(lines, formatStartupStatusLine(m.theme, item, runningSpinner))
	}
	return lines
}

func formatStartupStatusLine(theme Theme, item startupItem, runningSpinner string) string {
	okIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true).Render("✓")
	errIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render("✗")
	pendingIcon := lipgloss.NewStyle().Foreground(theme.color(theme.StatusHintFG)).Render("…")

	base := fmt.Sprintf("%s [%s]", item.Label, item.Mode)
	switch item.Status {
	case startupStatusOK:
		if item.HasExitCode {
			return fmt.Sprintf("%s %s %d", okIcon, base, item.ExitCode)
		}
		return fmt.Sprintf("%s %s", okIcon, base)
	case startupStatusError:
		if item.HasExitCode && item.ErrorText != "" {
			return fmt.Sprintf("%s %s %d · %s", errIcon, base, item.ExitCode, item.ErrorText)
		}
		if item.HasExitCode {
			return fmt.Sprintf("%s %s %d", errIcon, base, item.ExitCode)
		}
		if item.ErrorText != "" {
			return fmt.Sprintf("%s %s · %s", errIcon, base, item.ErrorText)
		}
		return fmt.Sprintf("%s %s", errIcon, base)
	case startupStatusRunning:
		if runningSpinner == "" {
			runningSpinner = "…"
		}
		return fmt.Sprintf("%s %s", runningSpinner, base)
	default:
		return fmt.Sprintf("%s %s queued", pendingIcon, base)
	}
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
		m.scrollStates[idx].Offset = 0
		m.scrollStates[idx].SelectedLineID = 0
		m.scrollStates[idx].SelectedLineKey = 0
		m.scrollStates[idx].SelectedIndex = -1
		return
	}

	_, hadValidSelection := findLineIndexByID(lines, m.scrollStates[idx].SelectedLineID)
	m.reconcileScrollState(idx, lines)
	if !hadValidSelection {
		m.selectFirstVisibleHistoryLine(idx, lines)
	}
	m.ensureSelectionVisible(idx, total)
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
	m.scrollStates[idx].Offset = clamp(m.scrollStates[idx].Offset-delta, 0, maxOffset)
	m.selectVisibleLineAfterScroll(idx, lines)
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

	st := &m.scrollStates[idx]
	if st.SelectedIndex < 0 || st.SelectedIndex >= len(lines) {
		m.setScrollSelection(idx, lines, len(lines)-1)
	} else {
		m.setScrollSelection(idx, lines, clamp(st.SelectedIndex+delta, 0, len(lines)-1))
	}
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
	m.setScrollSelection(idx, lines, clamp(target, 0, len(lines)-1))
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
	if len(lines) == 0 {
		return
	}

	st := &m.scrollStates[idx]
	sel := st.SelectedIndex
	if sel < 0 || sel >= len(lines) {
		return
	}

	tracked := st.Tracker.Lines[sel]
	lineKey := tracked.Key
	for i, mark := range st.Marks {
		if mark.LineKey == lineKey {
			st.Marks = append(st.Marks[:i], st.Marks[i+1:]...)
			return
		}
	}
	st.Marks = append(st.Marks, scrollMark{
		LineKey:   lineKey,
		LineID:    tracked.HistoryID,
		Text:      tracked.Text,
		PlainText: tracked.PlainText,
		CreatedAt: time.Now(),
	})
}

func (m *Model) ensureSelectionVisible(idx, total int) {
	pageSize := max(1, m.activePaneLineCapacity())
	lines := m.historyLines(m.panels[idx])
	if len(lines) != total {
		total = len(lines)
	}
	if total == 0 {
		return
	}
	rows := m.historyDisplayRows(idx, lines)
	maxOffset := max(0, len(rows)-pageSize)
	start := m.viewportStart(len(rows), pageSize, m.scrollStates[idx].Offset)
	end := min(len(rows), start+pageSize)
	sel := m.displayRowIndexForSelectedLine(idx, rows)
	if sel < 0 {
		return
	}

	if sel < start {
		start = sel
	} else if sel >= end {
		start = sel - pageSize + 1
	}
	start = clamp(start, 0, maxOffset)
	m.scrollStates[idx].Offset = maxOffset - start
}

func (m *Model) reconcileScrollState(idx int, lines []process.HistoryLine) {
	total := len(lines)
	pageSize := max(1, m.activePaneLineCapacity())
	maxOffset := max(0, total-pageSize)
	st := &m.scrollStates[idx]
	st.Offset = clamp(st.Offset, 0, maxOffset)

	if total == 0 {
		st.SelectedLineID = 0
		st.SelectedLineKey = 0
		st.SelectedIndex = -1
		st.Tracker.Lines = nil
		return
	}

	m.reconcileLineTracker(idx, lines)

	if st.SelectedLineKey != 0 {
		if selected, ok := findTrackedLineIndexByKey(st.Tracker.Lines, st.SelectedLineKey); ok {
			st.SelectedIndex = selected
			st.SelectedLineID = st.Tracker.Lines[selected].HistoryID
			return
		}
	}

	if st.SelectedLineID != 0 {
		if selected, ok := findLineIndexByID(lines, st.SelectedLineID); ok {
			st.SelectedIndex = selected
			st.SelectedLineKey = st.Tracker.Lines[selected].Key
			return
		}
	}

	if st.SelectedIndex >= total {
		st.SelectedIndex = total - 1
	}
	if st.SelectedIndex < 0 {
		st.SelectedIndex = -1
		st.SelectedLineID = 0
		st.SelectedLineKey = 0
		return
	}
	st.SelectedLineID = lines[st.SelectedIndex].ID
	st.SelectedLineKey = st.Tracker.Lines[st.SelectedIndex].Key
}

func (m *Model) reconcileLineTracker(idx int, lines []process.HistoryLine) {
	if idx < 0 || idx >= len(m.scrollStates) {
		return
	}
	st := &m.scrollStates[idx]
	if len(lines) == 0 {
		st.Tracker.Lines = nil
		return
	}

	prev := st.Tracker.Lines
	next := make([]trackedLine, len(lines))
	matchedPrev := make([]bool, len(prev))
	matchedNext := make([]bool, len(lines))

	assign := func(nextIdx, prevIdx int) {
		if nextIdx < 0 || nextIdx >= len(lines) || prevIdx < 0 || prevIdx >= len(prev) {
			return
		}
		line := lines[nextIdx]
		next[nextIdx] = trackedLine{
			Key:          prev[prevIdx].Key,
			HistoryID:    line.ID,
			Text:         line.Text,
			PlainText:    ansi.Strip(line.Text),
			HistoryIndex: nextIdx,
		}
		matchedPrev[prevIdx] = true
		matchedNext[nextIdx] = true
	}

	prevSignatures := trackedLineSignatures(prev)
	nextSignatures := historyLineSignatures(lines)
	if overlap := longestStringSuffixPrefixOverlap(prevSignatures, nextSignatures); overlap > 0 {
		prevStart := len(prev) - overlap
		for i := 0; i < overlap; i++ {
			assign(i, prevStart+i)
		}
	}

	prevIDText := uniqueTrackedLineIDTextIndexes(prev, matchedPrev)
	nextIDText := uniqueHistoryLineIDTextIndexes(lines, matchedNext)
	for sig, prevIdx := range prevIDText {
		nextIdx, ok := nextIDText[sig]
		if !ok || matchedPrev[prevIdx] || matchedNext[nextIdx] {
			continue
		}
		assign(nextIdx, prevIdx)
	}

	prevText := uniqueTrackedLineTextIndexes(prev, matchedPrev)
	nextText := uniqueHistoryLineTextIndexes(lines, matchedNext)
	for text, prevIdx := range prevText {
		nextIdx, ok := nextText[text]
		if !ok || matchedPrev[prevIdx] || matchedNext[nextIdx] {
			continue
		}
		assign(nextIdx, prevIdx)
	}

	for i, line := range lines {
		if matchedNext[i] {
			continue
		}
		st.Tracker.NextKey++
		next[i] = trackedLine{
			Key:          st.Tracker.NextKey,
			HistoryID:    line.ID,
			Text:         line.Text,
			PlainText:    ansi.Strip(line.Text),
			HistoryIndex: i,
		}
	}

	st.Tracker.Lines = next
}

func (m *Model) viewportForPanel(idx, height int) *paneViewport {
	if idx < 0 || idx >= len(m.panels) {
		return nil
	}

	m.ensureScrollState()
	pageSize := max(1, paneLineCapacity(height))
	active := idx == m.activePanel
	if active && m.panelSelectMode {
		return m.selectViewportForPanel(idx, pageSize)
	}
	if active && m.panelInsertMode {
		return nil
	}
	if !active || !m.panelScrollMode {
		return m.liveBookmarkedViewportForPanel(idx, pageSize)
	}
	return m.historyViewportForPanel(idx, pageSize)
}

func (m *Model) historyViewportForPanel(idx, pageSize int) *paneViewport {
	history := m.historyLines(m.panels[idx])
	if len(history) == 0 {
		m.reconcileScrollState(idx, history)
		return &paneViewport{Lines: make([]string, pageSize), SelectedRow: -1}
	}
	m.reconcileScrollState(idx, history)

	rows := m.historyDisplayRows(idx, history)
	start := m.viewportStart(len(rows), pageSize, m.scrollStates[idx].Offset)
	end := min(len(rows), start+pageSize)
	selectedRow := -1
	if selected := m.displayRowIndexForSelectedLine(idx, rows); selected >= start && selected < end {
		selectedRow = selected - start
	}

	return viewportFromRows(rows[start:end], selectedRow)
}

func (m *Model) liveBookmarkedViewportForPanel(idx, pageSize int) *paneViewport {
	if idx < 0 || idx >= len(m.scrollStates) || len(m.scrollStates[idx].Marks) == 0 {
		return nil
	}
	history := m.historyLines(m.panels[idx])
	if len(history) == 0 {
		return nil
	}
	m.reconcileScrollState(idx, history)

	view := m.displayForView(m.panels[idx])
	rawLines, _ := visibleViewportLines(view.Output, pageSize, -1)
	start, ok := displayHistoryStart(history, rawLines)
	if !ok {
		// Best-effort fallback: treat visible terminal lines as tail of history.
		// This preserves live-rendered text while still applying bookmark IDs.
		start = len(history) - len(rawLines)
		if start < 0 {
			start = 0
		}
	}

	marks := m.scrollMarksByLineKey(idx)
	if len(marks) == 0 {
		return nil
	}
	rows := make([]paneViewportRow, 0, pageSize)
	hasBookmark := false
	for i := 0; i < pageSize; i++ {
		line := ""
		if i < len(rawLines) {
			line = rawLines[i]
		}
		historyIndex := start + i
		var lineID, lineKey uint64
		bookmarked := false
		if i < len(rawLines) && historyIndex >= 0 && historyIndex < len(m.scrollStates[idx].Tracker.Lines) {
			tracked := m.scrollStates[idx].Tracker.Lines[historyIndex]
			lineID = tracked.HistoryID
			lineKey = tracked.Key
			_, bookmarked = marks[lineKey]
			hasBookmark = hasBookmark || bookmarked
		}
		rows = append(rows, paneViewportRow{
			Line:         line,
			PlainLine:    ansi.Strip(line),
			HistoryIndex: historyIndex,
			LineID:       lineID,
			LineKey:      lineKey,
			Bookmarked:   bookmarked,
		})
	}
	if !hasBookmark {
		return nil
	}
	return viewportFromRows(rows, -1)
}

func (m *Model) selectViewportForPanel(idx, pageSize int) *paneViewport {
	rows := m.selectionRowsForPanel(idx, pageSize)
	lines, plainLines := viewportLines(rows, pageSize)
	vp := &paneViewport{
		Rows:        rows,
		Lines:       lines,
		PlainLines:  plainLines,
		SelectedRow: -1,
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
	rows := m.selectionRowsForPanel(idx, pageSize)
	return viewportLines(rows, pageSize)
}

func (m *Model) selectionRowsForPanel(idx, pageSize int) []paneViewportRow {
	if idx < 0 || idx >= len(m.panels) {
		return nil
	}
	width := m.activePaneContentWidth()
	if width <= 0 {
		width = 1
	}
	if idx < len(m.selections) && m.selections[idx].Source == selectSourceHistory {
		history := m.historyLines(m.panels[idx])
		if len(history) == 0 {
			return blankViewportRows(pageSize, width)
		}
		m.reconcileScrollState(idx, history)
		rows := m.historyDisplayRows(idx, history)
		start := m.viewportStart(len(rows), pageSize, m.scrollStates[idx].Offset)
		end := min(len(rows), start+pageSize)
		return padViewportRows(rows[start:end], pageSize, width)
	}

	view := m.displayForView(m.panels[idx])
	rawLines, _ := visibleViewportLines(view.Output, pageSize, -1)
	rows := make([]paneViewportRow, pageSize)
	for i := 0; i < pageSize; i++ {
		if i < len(rawLines) {
			rows[i] = paneViewportRow{
				Line:         padOrTruncate(rawLines[i], width),
				PlainLine:    padOrTruncate(ansi.Strip(rawLines[i]), width),
				HistoryIndex: i,
			}
			continue
		}
		fill := strings.Repeat(" ", width)
		rows[i] = paneViewportRow{
			Line:         fill,
			PlainLine:    fill,
			HistoryIndex: i,
		}
	}
	return rows
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
		m.messageBuffer = append(m.messageBuffer, "warning: selection is empty")
		return
	}
	if err := m.copySelection(text); err != nil {
		m.messageBuffer = append(m.messageBuffer, fmt.Sprintf("error: copying selection: %v", err))
		return
	}
	m.messageBuffer = append(m.messageBuffer, "Copied panel selection to clipboard.")
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
	rows := m.selectionRowsForPanel(idx, m.activePaneLineCapacity())
	if len(rows) == 0 {
		return ""
	}
	startRow, startCol, endRow, endCol := normalizeSelection(sel.StartRow, sel.StartCol, sel.EndRow, sel.EndCol)
	startRow = clamp(startRow, 0, len(rows)-1)
	endRow = clamp(endRow, 0, len(rows)-1)
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
		selected = append(selected, strings.TrimRight(sliceByCells(rows[row].PlainLine, lineStart, lineEnd), " "))
	}
	return strings.Join(selected, "\n")
}

func (m *Model) clearScrollState(idx int) {
	m.ensureScrollState()
	if idx < 0 || idx >= len(m.scrollStates) {
		return
	}
	m.scrollStates[idx] = scrollState{SelectedIndex: -1}
	if idx < len(m.selections) {
		m.selections[idx] = panelSelection{}
	}
}

func (m *Model) setScrollSelection(idx int, lines []process.HistoryLine, selected int) {
	if idx < 0 || idx >= len(m.scrollStates) || len(lines) == 0 {
		return
	}
	m.reconcileLineTracker(idx, lines)
	selected = clamp(selected, 0, len(lines)-1)
	m.scrollStates[idx].SelectedIndex = selected
	m.scrollStates[idx].SelectedLineID = lines[selected].ID
	m.scrollStates[idx].SelectedLineKey = m.scrollStates[idx].Tracker.Lines[selected].Key
}

func (m *Model) selectFirstVisibleHistoryLine(idx int, lines []process.HistoryLine) {
	pageSize := max(1, m.activePaneLineCapacity())
	rows := m.historyDisplayRows(idx, lines)
	start := m.viewportStart(len(rows), pageSize, m.scrollStates[idx].Offset)
	end := min(len(rows), start+pageSize)
	for _, row := range rows[start:end] {
		if row.HistoryIndex >= 0 {
			m.setScrollSelection(idx, lines, row.HistoryIndex)
			return
		}
	}
	if len(lines) > 0 {
		m.setScrollSelection(idx, lines, 0)
	}
}

func (m *Model) selectVisibleLineAfterScroll(idx int, lines []process.HistoryLine) {
	pageSize := max(1, m.activePaneLineCapacity())
	rows := m.historyDisplayRows(idx, lines)
	start := m.viewportStart(len(rows), pageSize, m.scrollStates[idx].Offset)
	end := min(len(rows), start+pageSize)
	selected := m.displayRowIndexForSelectedLine(idx, rows)
	if selected >= start && selected < end {
		return
	}

	if selected < start {
		for row := start; row < end; row++ {
			if rows[row].HistoryIndex >= 0 {
				m.setScrollSelection(idx, lines, rows[row].HistoryIndex)
				return
			}
		}
	} else {
		for row := end - 1; row >= start; row-- {
			if rows[row].HistoryIndex >= 0 {
				m.setScrollSelection(idx, lines, rows[row].HistoryIndex)
				return
			}
		}
	}
}

func (m *Model) selectScrollLineAtViewportRow(row int) {
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
	rows := m.historyDisplayRows(idx, lines)
	start := m.viewportStart(len(rows), pageSize, m.scrollStates[idx].Offset)
	displayRow := start + row
	if displayRow < 0 || displayRow >= len(rows) {
		return
	}
	if rows[displayRow].HistoryIndex < 0 {
		return
	}
	m.setScrollSelection(idx, lines, rows[displayRow].HistoryIndex)
}

func (m *Model) displayRowIndexForSelectedLine(idx int, rows []paneViewportRow) int {
	if idx < 0 || idx >= len(m.scrollStates) {
		return -1
	}
	selected := m.scrollStates[idx].SelectedIndex
	if selected < 0 {
		return -1
	}
	selectedKey := m.scrollStates[idx].SelectedLineKey
	for i, row := range rows {
		if selectedKey != 0 && row.LineKey == selectedKey {
			return i
		}
		if row.HistoryIndex == selected {
			return i
		}
	}
	return -1
}

func (m *Model) historyDisplayRows(idx int, lines []process.HistoryLine) []paneViewportRow {
	m.reconcileLineTracker(idx, lines)
	marks := m.scrollMarksByLineKey(idx)
	rows := make([]paneViewportRow, 0, len(lines))
	for i, line := range lines {
		tracked := m.scrollStates[idx].Tracker.Lines[i]
		_, bookmarked := marks[tracked.Key]
		rows = append(rows, paneViewportRow{
			Line:         line.Text,
			PlainLine:    ansi.Strip(line.Text),
			HistoryIndex: i,
			LineID:       tracked.HistoryID,
			LineKey:      tracked.Key,
			Bookmarked:   bookmarked,
		})
	}
	return rows
}

func (m *Model) scrollMarksByLineKey(idx int) map[uint64]scrollMark {
	if idx < 0 || idx >= len(m.scrollStates) || len(m.scrollStates[idx].Marks) == 0 {
		return nil
	}
	marks := make(map[uint64]scrollMark, len(m.scrollStates[idx].Marks))
	for _, mark := range m.scrollStates[idx].Marks {
		if mark.LineKey != 0 {
			marks[mark.LineKey] = mark
			continue
		}
		if mark.LineID == 0 {
			continue
		}
		if line, ok := findTrackedLineByHistoryID(m.scrollStates[idx].Tracker.Lines, mark.LineID); ok {
			marks[line.Key] = mark
		}
	}
	return marks
}

func viewportFromRows(rows []paneViewportRow, selectedRow int) *paneViewport {
	lines, plainLines := viewportLines(rows, len(rows))
	return &paneViewport{
		Rows:        rows,
		Lines:       lines,
		PlainLines:  plainLines,
		SelectedRow: selectedRow,
	}
}

func viewportLines(rows []paneViewportRow, pageSize int) ([]string, []string) {
	lines := make([]string, pageSize)
	plainLines := make([]string, pageSize)
	for i := 0; i < pageSize; i++ {
		if i < len(rows) {
			lines[i] = rows[i].Line
			plainLines[i] = rows[i].PlainLine
		}
	}
	return lines, plainLines
}

func padViewportRows(rows []paneViewportRow, pageSize, width int) []paneViewportRow {
	out := append([]paneViewportRow(nil), rows...)
	for len(out) < pageSize {
		fill := strings.Repeat(" ", width)
		out = append(out, paneViewportRow{
			Line:         fill,
			PlainLine:    fill,
			HistoryIndex: -1,
		})
	}
	return out
}

func blankViewportRows(pageSize, width int) []paneViewportRow {
	return padViewportRows(nil, pageSize, width)
}

func (m Model) viewportStart(total, pageSize, offset int) int {
	if total <= pageSize {
		return 0
	}
	maxOffset := total - pageSize
	offset = clamp(offset, 0, maxOffset)
	return total - pageSize - offset
}

func findLineIndexByID(lines []process.HistoryLine, want uint64) (int, bool) {
	for i, line := range lines {
		if line.ID == want {
			return i, true
		}
	}
	return -1, false
}

func findTrackedLineIndexByKey(lines []trackedLine, want uint64) (int, bool) {
	for i, line := range lines {
		if line.Key == want {
			return i, true
		}
	}
	return -1, false
}

func findTrackedLineByHistoryID(lines []trackedLine, want uint64) (trackedLine, bool) {
	for _, line := range lines {
		if line.HistoryID == want {
			return line, true
		}
	}
	return trackedLine{}, false
}

func trackedLineSignatures(lines []trackedLine) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = line.Text
	}
	return out
}

func historyLineSignatures(lines []process.HistoryLine) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = line.Text
	}
	return out
}

type historyIDTextSignature struct {
	ID   uint64
	Text string
}

func uniqueTrackedLineIDTextIndexes(lines []trackedLine, matched []bool) map[historyIDTextSignature]int {
	counts := make(map[historyIDTextSignature]int, len(lines))
	indexes := make(map[historyIDTextSignature]int, len(lines))
	for i, line := range lines {
		if i < len(matched) && matched[i] {
			continue
		}
		sig := historyIDTextSignature{ID: line.HistoryID, Text: line.Text}
		counts[sig]++
		indexes[sig] = i
	}
	return uniqueIndexes(counts, indexes)
}

func uniqueHistoryLineIDTextIndexes(lines []process.HistoryLine, matched []bool) map[historyIDTextSignature]int {
	counts := make(map[historyIDTextSignature]int, len(lines))
	indexes := make(map[historyIDTextSignature]int, len(lines))
	for i, line := range lines {
		if i < len(matched) && matched[i] {
			continue
		}
		sig := historyIDTextSignature{ID: line.ID, Text: line.Text}
		counts[sig]++
		indexes[sig] = i
	}
	return uniqueIndexes(counts, indexes)
}

func uniqueIndexes[T comparable](counts map[T]int, indexes map[T]int) map[T]int {
	out := make(map[T]int, len(indexes))
	for sig, idx := range indexes {
		if counts[sig] == 1 {
			out[sig] = idx
		}
	}
	return out
}

func uniqueTrackedLineTextIndexes(lines []trackedLine, matched []bool) map[string]int {
	counts := make(map[string]int, len(lines))
	indexes := make(map[string]int, len(lines))
	for i, line := range lines {
		if i < len(matched) && matched[i] {
			continue
		}
		counts[line.Text]++
		indexes[line.Text] = i
	}
	return uniqueIndexes(counts, indexes)
}

func uniqueHistoryLineTextIndexes(lines []process.HistoryLine, matched []bool) map[string]int {
	counts := make(map[string]int, len(lines))
	indexes := make(map[string]int, len(lines))
	for i, line := range lines {
		if i < len(matched) && matched[i] {
			continue
		}
		counts[line.Text]++
		indexes[line.Text] = i
	}
	return uniqueIndexes(counts, indexes)
}

func longestStringSuffixPrefixOverlap(text, pattern []string) int {
	if len(text) == 0 || len(pattern) == 0 {
		return 0
	}

	lps := make([]int, len(pattern))
	for i, j := 1, 0; i < len(pattern); {
		if pattern[i] == pattern[j] {
			j++
			lps[i] = j
			i++
			continue
		}
		if j > 0 {
			j = lps[j-1]
			continue
		}
		i++
	}

	matched := 0
	for _, v := range text {
		for matched > 0 && (matched == len(pattern) || v != pattern[matched]) {
			matched = lps[matched-1]
		}
		if matched < len(pattern) && v == pattern[matched] {
			matched++
		}
	}
	return matched
}

func displayHistoryStart(history []process.HistoryLine, rawLines []string) (int, bool) {
	if len(rawLines) == 0 {
		return 0, false
	}
	if len(history) < len(rawLines) {
		return 0, false
	}
	start := len(history) - len(rawLines)
	for i, raw := range rawLines {
		if normalizeDisplayLine(raw) != normalizeDisplayLine(history[start+i].Text) {
			return 0, false
		}
	}
	return start, true
}

func normalizeDisplayLine(line string) string {
	return strings.TrimRight(ansi.Strip(line), " ")
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
