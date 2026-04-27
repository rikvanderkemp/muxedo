// SPDX-License-Identifier: MIT
package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rikvanderkemp/muxedo/internal/layout"
	"github.com/rikvanderkemp/muxedo/internal/process"
	"github.com/rikvanderkemp/muxedo/internal/profile"
)

type tickMsg time.Time

type LogMsg string

type scrollbackMarker struct {
	AfterID uint64
	At      time.Time
}

type StartupCompleteMsg struct {
	panels []*process.Panel
}

type TeardownCompleteMsg struct{}

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

type teardownStatusMsg struct {
	idx         int
	status      startupStatus
	exitCode    int
	hasExitCode bool
	errText     string
}

type teardownLogMsg struct {
	idx  int
	line string
}

type exitProgressMsg struct {
	panelIdx int
	errText  string
}

type exitStatus uint8

const (
	exitStatusRunning exitStatus = iota
	exitStatusOK
	exitStatusError
)

type exitItem struct {
	Name    string
	Status  exitStatus
	ErrText string
}

type panelSelection struct {
	Active   bool
	Dragging bool
	Source   selectSource
	StartRow int
	StartCol int
	EndRow   int
	EndCol   int
}

const maxStartupLogLineBytes = 1 << 20 // 1 MiB
const msgChanBufferSize = 256
const msgSendTimeout = 100 * time.Millisecond

func newMsgChan() chan tea.Msg {
	return make(chan tea.Msg, msgChanBufferSize)
}

func isExpectedInterruptExit(err error) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	// Bubble up non-signal exits as real errors.
	ws, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		// Fallback: common SIGINT exit code (128 + 2).
		return exitErr.ExitCode() == 130
	}
	return ws.Signaled() && ws.Signal() == syscall.SIGINT
}

func gracefulQuitPanelCmd(idx int, p *process.Panel) tea.Cmd {
	return func() tea.Msg {
		var errText string
		// Ask process to stop but do not force-kill.
		p.RequestInterrupt()
		if err := p.WaitForExit(); err != nil && errText == "" && !isExpectedInterruptExit(err) {
			errText = fmt.Sprintf("exit error: %v", err)
		}
		return exitProgressMsg{panelIdx: idx, errText: errText}
	}
}

type Model struct {
	panels                          []*process.Panel
	theme                           Theme
	width                           int
	height                          int
	grid                            layout.Grid
	activePanel                     int
	maximizedPanel                  int
	panelInsertMode                 bool   // when a panel is focused: false = normal (vim-like), true = keys go to PTY
	prevPanelRunning                []bool // per-panel running state last tick; detects run→stop while focused
	selections                      []panelSelection
	scrollbackActive                bool
	scrollbackPanel                 int
	scrollbackView                  viewport.Model
	scrollbackLines                 []process.HistoryLine
	scrollbackShowLineNumbers       bool
	scrollbackLineNumberPrefixWidth int
	scrollbackRefreshedAt           time.Time
	scrollbackCursorID              uint64
	scrollbackCursorLine            int
	scrollbackMarked                map[uint64]struct{}
	scrollbackLastClickAt           time.Time
	scrollbackLastClickLine         int
	scrollbackRefreshMarkers        []scrollbackMarker
	scrollbackDisplayToHistory      []int
	scrollbackHistoryToDisplay      []int
	scrollbackSearchActive          bool
	scrollbackSearchQuery           string
	scrollbackSearchLocked          string
	scrollbackSearchErr             string
	scrollbackSearchMatches         []int
	scrollbackSearchIdx             int
	// scrollbackSearchMatchHit is a dense []bool indexed by history line index.
	// It replaces a map[int]struct{} to skip hashing in the per-row style hot
	// path and to be reusable across keystrokes via clear().
	scrollbackSearchMatchHit []bool
	scrollbackSearchRe       *regexp.Regexp
	// scrollbackDisplayBuf is the reusable backing slice for built display
	// lines; amortized across search-typing recomputes.
	scrollbackDisplayBuf []string
	sendInput            func(*process.Panel, []byte) error
	panelRunning         func(*process.Panel) bool
	restartPanel         func(*process.Panel) error
	historyLines         func(*process.Panel) []process.HistoryLine
	displayForView       func(*process.Panel) process.DisplayState
	copySelection        func(string) error
	exiting              bool
	exitItems            []exitItem
	exitCompleted        int
	exitHadError         bool
	exitSpinner          spinner.Model

	killingPanel    bool
	killingPanelIdx int
	killStatus      string

	messageBuffer    []string
	showBuffer       bool
	helpActive       bool
	title            string
	startupCompleted bool
	startupSpecs     []profile.StartupSpec
	startupItems     []startupItem
	startupSpinner   spinner.Model
	panelSpecs       []profile.PanelSpec
	scrollbackConfig profile.ScrollbackConfig

	teardownSpecs     []profile.StartupSpec
	teardownItems     []startupItem
	teardownCompleted bool
	teardownHadError  bool
	msgChan           chan tea.Msg
}

func NewModel(panels []*process.Panel, themes ...Theme) Model {
	theme := DefaultTheme()
	if len(themes) > 0 {
		theme = themes[0]
	}

	s := spinner.New(spinner.WithSpinner(spinner.Dot))
	s.Style = lipgloss.NewStyle().Foreground(theme.color(theme.StatusModeNormalFG))

	return Model{
		panels:                    panels,
		theme:                     theme,
		grid:                      layout.Compute(len(panels)),
		activePanel:               -1,
		maximizedPanel:            -1,
		scrollbackPanel:           -1,
		scrollbackShowLineNumbers: true,
		scrollbackCursorLine:      -1,
		scrollbackLastClickLine:   -1,
		startupCompleted:          true,
		exitSpinner:               s,
		scrollbackConfig:          profile.ScrollbackConfig{},
		msgChan:                   newMsgChan(),
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

func NewModelWithSpecs(title string, startup []profile.StartupSpec, teardown []profile.StartupSpec, panels []profile.PanelSpec, sb profile.ScrollbackConfig, theme Theme) Model {
	startupSpinner := spinner.New(spinner.WithSpinner(spinner.Dot))
	startupSpinner.Style = lipgloss.NewStyle().Foreground(theme.color(theme.StatusModeNormalFG))
	exitSpinner := spinner.New(spinner.WithSpinner(spinner.Dot))
	exitSpinner.Style = lipgloss.NewStyle().Foreground(theme.color(theme.StatusModeNormalFG))

	return Model{
		title:                     title,
		startupSpecs:              startup,
		startupItems:              newStartupItems(startup),
		startupSpinner:            startupSpinner,
		exitSpinner:               exitSpinner,
		panelSpecs:                panels,
		scrollbackConfig:          sb,
		theme:                     theme,
		teardownSpecs:             teardown,
		teardownItems:             newStartupItems(teardown),
		msgChan:                   newMsgChan(),
		grid:                      layout.Compute(len(panels)),
		activePanel:               -1,
		maximizedPanel:            -1,
		scrollbackPanel:           -1,
		scrollbackShowLineNumbers: true,
		scrollbackCursorLine:      -1,
		scrollbackLastClickLine:   -1,
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
	t := time.NewTimer(msgSendTimeout)
	defer t.Stop()
	select {
	case m.msgChan <- msg:
		return true
	case <-t.C:
		return false
	}
}

func (m Model) emitMsgBlocking(msg tea.Msg) {
	m.msgChan <- msg
}

func (m Model) Init() tea.Cmd {
	if m.exiting || m.killingPanel {
		return tea.Batch(tick(), m.waitForMsg, tea.ClearScreen, m.exitSpinner.Tick)
	}
	if m.startupCompleted {
		return tea.Batch(tick(), m.waitForMsg, tea.ClearScreen)
	}
	return tea.Batch(tick(), m.startupSequence, m.waitForMsg, tea.ClearScreen, m.startupSpinner.Tick)
}

func (m Model) teardownSequence() tea.Msg {
	go func() {
		if len(m.teardownSpecs) == 0 {
			m.emitMsgBlocking(TeardownCompleteMsg{})
			return
		}
		var asyncDone []<-chan struct{}
		for i, spec := range m.teardownSpecs {
			if spec.Mode == profile.StartupModeSync {
				m.runTeardownItem(i, spec)
				continue
			}
			asyncDone = append(asyncDone, m.runTeardownItemAsync(i, spec))
		}
		for _, ch := range asyncDone {
			<-ch
		}
		m.emitMsgBlocking(TeardownCompleteMsg{})
	}()
	return nil
}

func (m Model) runTeardownItem(idx int, spec profile.StartupSpec) {
	<-m.runTeardownItemAsync(idx, profile.StartupSpec{
		WorkingDir: spec.WorkingDir,
		Command:    spec.Command,
		Mode:       profile.StartupModeSync,
	})
}

func (m Model) runTeardownItemAsync(idx int, spec profile.StartupSpec) <-chan struct{} {
	doneCh := make(chan struct{})
	label := describeCommand(spec.Command)
	m.emitMsgBlocking(teardownStatusMsg{idx: idx, status: startupStatusRunning})
	m.emitMsg(LogMsg(fmt.Sprintf("--- Teardown starting %s (%s)", label, spec.Mode)))

	cmd, stdout, stderr, err := buildStartupCommand(profile.StartupSpec{
		WorkingDir: spec.WorkingDir,
		Command:    spec.Command,
		Mode:       spec.Mode,
	})
	if err != nil {
		m.emitMsgBlocking(teardownStatusMsg{idx: idx, status: startupStatusError, errText: err.Error()})
		close(doneCh)
		return doneCh
	}

	done := make(chan struct{}, 2)
	go m.streamTeardownOutput(idx, stdout, done)
	go m.streamTeardownOutput(idx, stderr, done)

	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		<-done
		<-done
		m.emitMsgBlocking(teardownStatusMsg{idx: idx, status: startupStatusError, errText: fmt.Sprintf("start failed: %v", err)})
		close(doneCh)
		return doneCh
	}

	go func() {
		err := cmd.Wait()
		<-done
		<-done
		statusMsg := teardownStatusMsg{idx: idx, status: startupStatusOK, exitCode: 0, hasExitCode: true}
		if err != nil {
			statusMsg.status = startupStatusError
			statusMsg.exitCode, statusMsg.hasExitCode, statusMsg.errText = commandExitDetails(err)
		}
		m.emitMsgBlocking(statusMsg)
		close(doneCh)
	}()
	return doneCh
}

func (m Model) streamTeardownOutput(idx int, r io.Reader, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxStartupLogLineBytes)
	for scanner.Scan() {
		m.emitMsg(teardownLogMsg{idx: idx, line: scanner.Text()})
	}
	if err := scanner.Err(); err != nil {
		m.emitMsg(teardownLogMsg{idx: idx, line: fmt.Sprintf("error: scanning output: %v", err)})
	}
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
			p := process.NewWithScrollbackCommandSpec(spec.Name, spec.Command, process.CommandSpec{}, spec.WorkingDir, m.scrollbackConfig.Dir, m.scrollbackConfig.MaxBytes)
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
	case teardownLogMsg:
		m.messageBuffer = append(m.messageBuffer, m.formatTeardownLog(msg.idx, msg.line))
		return m, m.waitForMsg
	case teardownStatusMsg:
		if msg.idx >= 0 && msg.idx < len(m.teardownItems) {
			item := m.teardownItems[msg.idx]
			item.Status = startupStatus(msg.status)
			item.HasExitCode = msg.hasExitCode
			item.ExitCode = msg.exitCode
			item.ErrorText = msg.errText
			m.teardownItems[msg.idx] = item
			if msg.status == startupStatusError {
				m.teardownHadError = true
			}
		}
		return m, m.waitForMsg
	case TeardownCompleteMsg:
		m.teardownCompleted = true
		if !m.teardownHadError {
			return m, tea.Quit
		}
		return m, nil
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
		if m.exiting || m.killingPanel {
			var cmd tea.Cmd
			m.exitSpinner, cmd = m.exitSpinner.Update(msg)
			return m, cmd
		}
		if !m.startupCompleted {
			var cmd tea.Cmd
			m.startupSpinner, cmd = m.startupSpinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	if m.killingPanel {
		switch msg := msg.(type) {
		case exitProgressMsg:
			if msg.panelIdx == m.killingPanelIdx {
				m.killStatus = msg.errText
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
		case tea.KeyPressMsg:
			// Second quit triggers force stop + exit.
			switch msg.String() {
			case "q", "ctrl+c":
				for _, p := range m.panels {
					if p != nil && p.Running() {
						p.Stop()
					}
				}
				return m, tea.Quit
			}
			return m, nil
		case exitProgressMsg:
			if msg.panelIdx >= 0 && msg.panelIdx < len(m.exitItems) {
				it := m.exitItems[msg.panelIdx]
				if msg.errText != "" {
					it.Status = exitStatusError
					it.ErrText = msg.errText
					m.exitHadError = true
				} else {
					it.Status = exitStatusOK
					it.ErrText = ""
				}
				m.exitItems[msg.panelIdx] = it
			}
			m.exitCompleted++
			if m.exitCompleted == len(m.panels) {
				// After all panels stopped, run teardown if configured.
				if !m.teardownCompleted && len(m.teardownSpecs) > 0 {
					return m, tea.Batch(m.exitSpinner.Tick, m.teardownSequence)
				}
				if !m.exitHadError && (m.teardownCompleted || len(m.teardownSpecs) == 0) && !m.teardownHadError {
					return m, tea.Quit
				}
				return m, nil
			}
			return m, nil
		}
		// Ignore other messages while exiting
		return m, nil
	}

	// Help modal: toggle with `?`, close with `?` or `Esc`. Disabled in insert
	// mode so insert keys still go to the PTY.
	if k, ok := msg.(tea.KeyPressMsg); ok && !m.panelInsertMode {
		if m.helpActive {
			if isHelpToggleKey(k) || k.String() == "esc" {
				m.helpActive = false
				return m, nil
			}
			return m, nil
		}
		if isHelpToggleKey(k) {
			m.helpActive = true
			return m, nil
		}
	}

	if m.scrollbackActive {
		if next, cmd, handled := m.updateScrollback(msg); handled {
			return next, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+b" {
			m.showBuffer = !m.showBuffer
			return m, nil
		}

		if msg.String() == "esc" {
			if m.activePanel >= 0 {
				if m.panelInsertMode {
					m.panelInsertMode = false
					return m, nil
				}
				if m.clearActiveSelection() {
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

		if m.activePanel < 0 && msg.Mod == 0 {
			if runes := []rune(msg.Text); len(runes) == 1 {
				if idx, ok := panelIndexFromDigit(runes[0], len(m.panels)); ok {
					m.applyPanelFocus(idx)
					return m, nil
				}
			}
		}

		if m.activePanel >= 0 && m.activePanel < len(m.panels) {
			m.ensureScrollState()
			p := m.panels[m.activePanel]
			running := m.panelRunning(p)

			if m.panelInsertMode {
				if !running {
					return m, nil
				}
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

			if msg.Mod == 0 {
				if runes := []rune(msg.Text); len(runes) == 1 {
					r := runes[0]
					if next, ok := neighborPanelIndex(m.grid, len(m.panels), m.activePanel, r); ok {
						m.applyPanelFocus(next)
						return m, nil
					}
					if idx, ok := panelIndexFromDigit(r, len(m.panels)); ok {
						m.applyPanelFocus(idx)
						return m, nil
					}
				}
			}

			if m.handleCopyKey(msg) {
				return m, nil
			}

			if msg.Mod == 0 {
				if runes := []rune(msg.Text); len(runes) == 1 {
					switch runes[0] {
					case 'i', 'I':
						if running {
							m.clearActiveSelection()
							m.panelInsertMode = true
						}
						return m, nil
					case 's', 'S':
						m.enterScrollback()
						return m, nil
					case 'm', 'M':
						m.toggleMaximized()
						return m, nil
					case 'r', 'R':
						if err := m.restartPanel(p); err != nil {
							m.messageBuffer = append(m.messageBuffer, fmt.Sprintf("error: reloading panel %s: %v", p.Name, err))
						}
						m.resizePanels()
						return m, nil
					case 'x', 'X':
						m.killingPanel = true
						m.killingPanelIdx = m.activePanel
						m.killStatus = ""
						return m, func() tea.Msg {
							p.Stop()
							return exitProgressMsg{panelIdx: m.activePanel, errText: ""}
						}
					}
				}
			}
			if !running {
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			m.exiting = true
			m.exitItems = make([]exitItem, len(m.panels))
			m.exitCompleted = 0
			m.exitHadError = false
			m.teardownCompleted = len(m.teardownSpecs) == 0
			m.teardownHadError = false
			var cmds []tea.Cmd
			for i, p := range m.panels {
				// Only show panels with explicit kill commands in the exit dialog.
				m.exitItems[i] = exitItem{Name: p.Name, Status: exitStatusRunning}
				cmds = append(cmds, gracefulQuitPanelCmd(i, p))
			}
			// Start spinner ticks immediately.
			cmds = append(cmds, m.exitSpinner.Tick)
			return m, tea.Batch(cmds...)
		}

	case tea.KeyMsg:
		// Ignore key releases and other key events for now.
		return m, nil
	default:
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if idx, ok := m.panelIndexAt(msg.X, msg.Y); ok {
				m.clearAllSelections()
				if idx != m.activePanel {
					m.applyPanelFocus(idx)
				}
				if m.panelInsertMode {
					m.applyPanelFocus(idx)
					return m, nil
				}
				if m.activePanel >= 0 {
					if row, col, ok := m.panelContentPoint(idx, msg.X, msg.Y); ok {
						m.beginSelection(row, col)
						return m, nil
					}
				}
				return m, nil
			}
		}
	case tea.MouseMotionMsg:
		if m.activePanel >= 0 && !m.panelInsertMode && msg.Button == tea.MouseLeft {
			if idx, ok := m.panelIndexAt(msg.X, msg.Y); ok && idx == m.activePanel {
				if row, col, ok := m.panelContentPoint(idx, msg.X, msg.Y); ok {
					m.updateSelection(row, col)
					return m, nil
				}
			}
		}
	case tea.MouseReleaseMsg:
		if m.activePanel >= 0 && !m.panelInsertMode && msg.Button == tea.MouseLeft {
			if idx, ok := m.panelIndexAt(msg.X, msg.Y); ok && idx == m.activePanel {
				if row, col, ok := m.panelContentPoint(idx, msg.X, msg.Y); ok {
					m.finishSelection(row, col)
					return m, nil
				}
			}
		}
	case tea.MouseWheelMsg:
		return m, nil
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

func (m Model) view(content string) tea.View {
	content = strings.TrimSuffix(content, "\n")
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if m.title != "" {
		v.WindowTitle = "Muxedo - " + m.title
	} else {
		v.WindowTitle = "Muxedo"
	}
	return v
}

func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return m.view("Starting muxedo...")
	}

	if !m.startupCompleted || m.showBuffer {
		body := m.renderMessageBuffer()
		if m.height > 1 {
			body = lipgloss.JoinVertical(lipgloss.Left, body, m.renderStatusLine())
		}
		return m.view(lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, body))
	}

	gh := m.gridHeight()
	if m.scrollbackActive {
		body := m.renderScrollback()
		if m.height > 1 {
			body = lipgloss.JoinVertical(lipgloss.Left, body, m.renderStatusLine())
		}
		return m.view(lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, m.wrapOverlays(body)))
	}

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
			m.viewportForPanel(idx, gh),
			formatElapsed(p.Elapsed()),
		)
		if m.height > 1 {
			body = lipgloss.JoinVertical(lipgloss.Left, body, m.renderStatusLine())
		}
		return m.view(lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, m.wrapOverlays(body)))
	}

	cell := layout.CellSizes(m.width, gh, m.grid.Rows, m.grid.Cols)
	gapX := 1
	gapY := 1
	availW := m.width - gapX*(m.grid.Cols-1)
	availH := gh - gapY*(m.grid.Rows-1)
	if availW < 1 {
		availW = 1
	}
	if availH < 1 {
		availH = 1
	}
	cell = layout.CellSizes(availW, availH, m.grid.Rows, m.grid.Cols)

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
		sep := strings.Repeat(" ", gapX)
		row := lipgloss.JoinHorizontal(lipgloss.Top, interleave(cols, sep)...)
		gridRows = append(gridRows, row)
	}

	body := strings.Join(gridRows, strings.Repeat("\n", gapY))
	if m.height > 1 {
		body = lipgloss.JoinVertical(lipgloss.Left, body, m.renderStatusLine())
	}

	return m.view(lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, m.wrapOverlays(body)))
}

func (m Model) renderScrollback() string {
	title := "SCROLLBACK"
	if m.scrollbackPanel >= 0 && m.scrollbackPanel < len(m.panels) {
		title = formatPanelTitle(m.scrollbackPanel, m.panels[m.scrollbackPanel].Name) + " · SCROLLBACK"
	}
	if !m.scrollbackRefreshedAt.IsZero() {
		title += " · refreshed " + m.scrollbackRefreshedAt.Format("2006-01-02 15:04:05")
	}
	return renderScrollbackPane(m.theme, title, m.scrollbackView.View(), m.width, m.gridHeight())
}

func interleave(items []string, sep string) []string {
	if len(items) <= 1 {
		return items
	}
	out := make([]string, 0, len(items)*2-1)
	for i, it := range items {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, it)
	}
	return out
}

func (m Model) wrapOverlays(body string) string {
	// Exiting/killing dialog always wins.
	if m.exiting || m.killingPanel {
		return m.wrapExiting(body)
	}
	if m.helpActive {
		box := m.renderHelpDialog()
		return floatOverlayCentered(body, box, m.width, m.height)
	}
	return body
}

func (m Model) wrapExiting(body string) string {
	if !m.exiting && !m.killingPanel {
		return body
	}

	title := "EXITING"
	lines := []string{}

	sp := m.exitSpinner.View()
	if sp == "" {
		sp = "…"
	}

	okIcon := lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Background(m.theme.color(m.theme.OverlayBG)).
		Bold(true).
		Render("✓")
	errIcon := lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Background(m.theme.color(m.theme.OverlayBG)).
		Bold(true).
		Render("✗")

	if m.killingPanel {
		title = "STOPPING"
		name := "panel"
		if m.killingPanelIdx >= 0 && m.killingPanelIdx < len(m.panels) {
			name = m.panels[m.killingPanelIdx].Name
		}
		if m.killStatus == "" {
			lines = append(lines, fmt.Sprintf("%s exiting %s", sp, name))
		} else {
			lines = append(lines, fmt.Sprintf("%s %s %s", errIcon, name, m.killStatus))
		}
	} else {
		for _, it := range m.exitItems {
			switch it.Status {
			case exitStatusOK:
				lines = append(lines, fmt.Sprintf("%s exited %s", okIcon, it.Name))
			case exitStatusError:
				if it.ErrText != "" {
					lines = append(lines, fmt.Sprintf("%s %s %s", errIcon, it.Name, it.ErrText))
				} else {
					lines = append(lines, fmt.Sprintf("%s %s kill failed", errIcon, it.Name))
				}
			default:
				lines = append(lines, fmt.Sprintf("%s exiting %s", sp, it.Name))
			}
		}
		if len(m.teardownSpecs) > 0 {
			lines = append(lines, "", "Teardown:")
			runningSpinner := m.exitSpinner.View()
			for _, item := range m.teardownItems {
				lines = append(lines, formatStartupStatusLine(m.theme, item, runningSpinner))
			}
		}
		lines = append(lines, "")
		if m.exitHadError {
			lines = append(lines, "Exit error. Ctrl-C again (or q) to force quit")
		} else if m.teardownHadError {
			lines = append(lines, "Teardown error. Ctrl-C again (or q) to force quit")
		} else {
			lines = append(lines, "Ctrl-C again (or q) to force quit")
		}
	}

	// Fit dialog inside terminal with margin; clamp to [24..84] x [6..18].
	maxW := clamp(m.width-8, 24, 84)
	maxH := clamp(m.height-6, 6, 18)
	// Never exceed screen so borders aren't clipped.
	maxW = min(maxW, max(1, m.width-2))
	maxH = min(maxH, max(1, m.height-2))

	// Ensure at least one line.
	if len(lines) == 0 {
		lines = []string{sp + " exiting"}
	}

	// Height budget: title + blank + lines.
	contentLines := make([]string, 0, 2+len(lines))
	contentLines = append(contentLines, title, "")
	contentLines = append(contentLines, lines...)

	// Trim to height budget.
	if len(contentLines) > maxH {
		contentLines = contentLines[:maxH-1]
		contentLines = append(contentLines, "…")
	}

	// Content width: account for border (2) + padding L/R (2+2).
	innerW := maxW - 6
	if innerW < 1 {
		innerW = 1
	}
	for i := range contentLines {
		contentLines[i] = ansi.Truncate(contentLines[i], innerW, "")
		contentLines[i] = padOrTruncate(contentLines[i], innerW)
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.theme.color(m.theme.ActiveNormalTitleFG)).
		Background(m.theme.color(m.theme.ActiveNormalTitleBG)).
		Padding(0, 1)
	bodyStyle := lipgloss.NewStyle().
		Foreground(m.theme.color(m.theme.OverlayFG)).
		Background(m.theme.color(m.theme.OverlayBG))

	// Style title line.
	if len(contentLines) > 0 {
		tw := innerW - 2
		if tw < 0 {
			tw = 0
		}
		contentLines[0] = titleStyle.Render(" " + ansi.Truncate(contentLines[0], tw, "") + " ")
	}

	content := bodyStyle.Render(strings.Join(contentLines, "\n"))
	dialogBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.color(m.theme.ActiveNormalBorder)).
		Background(m.theme.color(m.theme.OverlayBG)).
		Padding(1, 2).
		Width(innerW).
		MaxWidth(maxW).
		Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialogBox)
}

func isHelpToggleKey(msg tea.KeyPressMsg) bool {
	return msg.Text == "?" || msg.String() == "?"
}

func (m Model) renderHelpDialog() string {
	mode := m.statusModeLabel()
	if m.scrollbackSearchActive {
		mode = "SEARCH"
	}

	lines := []string{
		"Help · " + mode,
		"",
		"Global (not in insert):",
		"  ?        help (toggle)",
		"  Esc      close help",
		"  Ctrl-B   message buffer",
		"",
		"Normal / no-focus:",
		"  click/1–9  focus panel",
		"  hjkl      move focus",
		"  I         insert mode",
		"  S         scrollback",
		"  M         maximize/restore",
		"  R         reload focused panel",
		"  X         stop focused panel",
		"  Esc       blur / restore+blur",
		"",
		"Scrollback:",
		"  /         search (regex, case-insensitive)",
		"  n / N     next / prev match",
		"  Y         yank matching lines",
		"  M         yank matched substrings",
		"  l         toggle line numbers",
		"  r         refresh (insert marker)",
		"  m         mark/unmark line",
		"  j/k/↑/↓   cursor line",
		"  g/G       top/bottom",
		"  wheel/PgUp/PgDn  scroll",
		"  click/drag select · y/Enter copy",
	}

	// Fit dialog inside terminal with margin; clamp to [20..84] x [8..28].
	maxW := clamp(m.width-8, 20, 84)
	maxH := clamp(m.height-6, 8, 28)
	// Never exceed screen so borders aren't clipped.
	maxW = min(maxW, max(1, m.width-2))
	maxH = min(maxH, max(1, m.height-2))

	// Trim to height budget.
	if len(lines) > maxH {
		lines = lines[:maxH-1]
		lines = append(lines, "  …")
	}

	// Content width: account for border (2) + padding L/R (2+2).
	innerW := maxW - 6
	if innerW < 1 {
		innerW = 1
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], innerW, "")
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.theme.color(m.theme.ActiveNormalTitleFG)).
		Background(m.theme.color(m.theme.ActiveNormalTitleBG)).
		Padding(0, 1)
	bodyStyle := lipgloss.NewStyle().Foreground(m.theme.color(m.theme.StatusBarFG))

	if len(lines) > 0 {
		tw := innerW - 2
		if tw < 0 {
			tw = 0
		}
		lines[0] = titleStyle.Render(" " + ansi.Truncate(lines[0], tw, "") + " ")
	}
	content := bodyStyle.Render(strings.Join(lines, "\n"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.color(m.theme.ActiveNormalBorder)).
		Background(m.theme.color(m.theme.StatusBarBG)).
		Padding(1, 2).
		Width(maxW).
		MaxWidth(maxW).
		Render(content)
}

func (m Model) statusModeLabel() string {
	if m.scrollbackActive {
		return "SCROLLBACK"
	}
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return "NONE"
	}
	if m.panelInsertMode {
		return "INSERT"
	}
	return "NORMAL"
}

func (m Model) statusHint() string {
	if !m.startupCompleted {
		return "STARTING... · ?: help · Ctrl-B buffer"
	}
	if m.showBuffer {
		return "BUFFER: Ctrl-B back · ?: help · Esc close"
	}

	if m.scrollbackActive {
		if m.scrollbackSearchActive {
			if m.scrollbackSearchErr != "" {
				return "?: help · SEARCH: " + m.scrollbackSearchQuery + " (invalid regex: " + m.scrollbackSearchErr + ") · Enter confirm · Esc cancel"
			}
			return "?: help · SEARCH: " + m.scrollbackSearchQuery + " · Enter confirm · Esc cancel"
		}
		return "?: help · SCROLLBACK: / search · r refresh · l lines · m mark · y/Enter copy · Esc close"
	}

	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return "?: help · No active: click/1–9 focus · Ctrl-B buffer · Ctrl-C quit"
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
		parts = append(parts, "?: help", "STOPPED: S scrollback", "drag select", "y/Enter copy", "1–9 hjkl panes", maximizeAction, "R reload", "X stop", escapeAction)
	} else if m.panelInsertMode {
		parts = append(parts, "INSERT: Esc normal")
	} else {
		parts = append(parts, "?: help", "NORMAL: I insert", "S scrollback", "drag select", "y/Enter copy", "1–9 hjkl panes", maximizeAction, "R reload", "X stop", "Ctrl-B buffer", escapeAction)
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

func (m *Model) enterScrollback() {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) || m.panelInsertMode {
		return
	}
	m.ensureScrollState()
	m.clearActiveSelection()

	idx := m.activePanel
	m.scrollbackActive = true
	m.scrollbackPanel = idx
	m.scrollbackLines = append([]process.HistoryLine(nil), m.historyLines(m.panels[idx])...)
	m.scrollbackRefreshedAt = time.Now()
	if len(m.scrollbackLines) > 0 {
		m.scrollbackCursorLine = len(m.scrollbackLines) - 1
		m.scrollbackCursorID = m.scrollbackLines[m.scrollbackCursorLine].ID
	} else {
		m.scrollbackCursorLine = -1
		m.scrollbackCursorID = 0
	}

	width, height := m.scrollbackViewportSize()
	vp := viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
	vp.FillHeight = true
	vp.SoftWrap = false
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3
	displayLines, prefixWidth := m.scrollbackBuildDisplay(vp.Width())
	vp.SetContentLines(displayLines)
	vp.GotoBottom()
	m.scrollbackView = vp
	m.scrollbackLineNumberPrefixWidth = prefixWidth
	m.updateScrollbackSelectionStyle()
}

func (m *Model) exitScrollback() {
	if !m.scrollbackActive {
		return
	}
	if m.scrollbackPanel >= 0 && m.scrollbackPanel < len(m.selections) {
		m.selections[m.scrollbackPanel] = panelSelection{}
	}
	m.scrollbackActive = false
	m.scrollbackPanel = -1
	m.scrollbackLines = nil
	m.scrollbackView = viewport.Model{}
	m.scrollbackLineNumberPrefixWidth = 0
	m.scrollbackRefreshedAt = time.Time{}
	m.scrollbackCursorLine = -1
	m.scrollbackCursorID = 0
	m.scrollbackLastClickAt = time.Time{}
	m.scrollbackLastClickLine = -1
	m.scrollbackRefreshMarkers = nil
	m.scrollbackDisplayToHistory = nil
	m.scrollbackHistoryToDisplay = nil
	m.scrollbackSearchActive = false
	m.scrollbackSearchQuery = ""
	m.scrollbackSearchLocked = ""
	m.scrollbackSearchErr = ""
	m.scrollbackSearchMatches = nil
	m.scrollbackSearchIdx = -1
	m.scrollbackSearchMatchHit = m.scrollbackSearchMatchHit[:0]
	m.scrollbackSearchRe = nil
	m.panelInsertMode = false
	m.helpActive = false
}

func (m *Model) setScrollbackCursorLine(line int) {
	if !m.scrollbackActive || len(m.scrollbackLines) == 0 {
		m.scrollbackCursorLine = -1
		m.scrollbackCursorID = 0
		return
	}
	line = clamp(line, 0, len(m.scrollbackLines)-1)
	m.scrollbackCursorLine = line
	m.scrollbackCursorID = m.scrollbackLines[line].ID
	// Keep a one-line selection as the "cursor highlight".
	if m.scrollbackPanel >= 0 && m.scrollbackPanel < len(m.selections) {
		// Use a very large EndCol so copy (`y`/Enter) grabs full line.
		m.selections[m.scrollbackPanel] = panelSelection{
			Active:   true,
			Dragging: false,
			Source:   selectSourceHistory,
			StartRow: line,
			StartCol: 0,
			EndRow:   line,
			EndCol:   1 << 30,
		}
	}
	displayLine := line
	if line >= 0 && line < len(m.scrollbackHistoryToDisplay) && m.scrollbackHistoryToDisplay[line] >= 0 {
		displayLine = m.scrollbackHistoryToDisplay[line]
	}
	m.scrollbackView.EnsureVisible(displayLine, 0, 0)
	m.updateScrollbackSelectionStyle()
}

func (m *Model) refreshScrollbackView() {
	if !m.scrollbackActive || m.scrollbackPanel < 0 || m.scrollbackPanel >= len(m.panels) {
		return
	}
	prevAfterID := uint64(0)
	if len(m.scrollbackLines) > 0 {
		prevAfterID = m.scrollbackLines[len(m.scrollbackLines)-1].ID
	}
	cursorID := m.scrollbackCursorID
	m.scrollbackLines = append([]process.HistoryLine(nil), m.historyLines(m.panels[m.scrollbackPanel])...)
	m.scrollbackRefreshedAt = time.Now()

	m.scrollbackRefreshMarkers = append(m.scrollbackRefreshMarkers, scrollbackMarker{
		AfterID: prevAfterID,
		At:      m.scrollbackRefreshedAt,
	})

	m.scrollbackCursorLine = -1
	if cursorID != 0 {
		for i := range m.scrollbackLines {
			if m.scrollbackLines[i].ID == cursorID {
				m.scrollbackCursorLine = i
				break
			}
		}
	}
	if m.scrollbackCursorLine >= 0 {
		m.scrollbackCursorID = cursorID
	} else if len(m.scrollbackLines) > 0 {
		m.scrollbackCursorLine = len(m.scrollbackLines) - 1
		m.scrollbackCursorID = m.scrollbackLines[m.scrollbackCursorLine].ID
	} else {
		m.scrollbackCursorID = 0
	}
	yoff := m.scrollbackView.YOffset()
	xoff := m.scrollbackView.XOffset()
	displayLines, prefixWidth := m.scrollbackBuildDisplay(m.scrollbackView.Width())
	m.scrollbackView.SetContentLines(displayLines)
	m.scrollbackView.SetYOffset(yoff)
	m.scrollbackView.SetXOffset(xoff)
	m.scrollbackLineNumberPrefixWidth = prefixWidth
	m.updateScrollbackSelectionStyle()
}

func (m *Model) scrollbackSearchRecompute(liveJump bool) {
	q := m.scrollbackSearchQuery
	if q == "" {
		m.scrollbackSearchErr = ""
		m.scrollbackSearchMatchHit = m.scrollbackSearchMatchHit[:0]
		m.scrollbackSearchMatches = m.scrollbackSearchMatches[:0]
		m.scrollbackSearchIdx = -1
		m.scrollbackSearchRe = nil
		return
	}
	re, err := regexp.Compile("(?i)" + q)
	if err != nil {
		// Preserve last valid matches/highlights so the user keeps context while
		// editing an in-progress regex (e.g. typing "(" before closing paren).
		m.scrollbackSearchErr = err.Error()
		return
	}
	m.scrollbackSearchErr = ""
	m.scrollbackSearchMatches = m.scrollbackSearchMatches[:0]
	m.scrollbackSearchIdx = -1
	m.scrollbackSearchRe = re

	for i := range m.scrollbackLines {
		if re.MatchString(m.scrollbackLines[i].Text) {
			m.scrollbackSearchMatches = append(m.scrollbackSearchMatches, i)
		}
	}

	// Resize+clear the hit slice in place to avoid per-keystroke map allocations.
	n := len(m.scrollbackLines)
	if cap(m.scrollbackSearchMatchHit) < n {
		m.scrollbackSearchMatchHit = make([]bool, n)
	} else {
		m.scrollbackSearchMatchHit = m.scrollbackSearchMatchHit[:n]
		clear(m.scrollbackSearchMatchHit)
	}
	for _, hi := range m.scrollbackSearchMatches {
		if hi >= 0 && hi < n {
			m.scrollbackSearchMatchHit[hi] = true
		}
	}

	if len(m.scrollbackSearchMatches) == 0 {
		return
	}

	start := m.scrollbackCursorLine
	if start < 0 {
		start = 0
	}
	best := 0
	for k := range m.scrollbackSearchMatches {
		if m.scrollbackSearchMatches[k] >= start {
			best = k
			break
		}
	}
	m.scrollbackSearchIdx = best

	if liveJump {
		m.scrollbackSearchJumpToIdx(best)
	}
}

func (m *Model) scrollbackSearchJumpToIdx(idx int) {
	if idx < 0 || idx >= len(m.scrollbackSearchMatches) {
		return
	}
	hi := m.scrollbackSearchMatches[idx]
	if hi < 0 || hi >= len(m.scrollbackLines) {
		return
	}
	// Jump to display row for history line.
	displayRow := hi
	if hi < len(m.scrollbackHistoryToDisplay) && m.scrollbackHistoryToDisplay[hi] >= 0 {
		displayRow = m.scrollbackHistoryToDisplay[hi]
	}
	m.scrollbackView.EnsureVisible(displayRow, 0, 0)
	m.setScrollbackCursorLine(hi)
}

func (m *Model) scrollbackRebuildDisplayPreserveOffsets() {
	yoff := m.scrollbackView.YOffset()
	xoff := m.scrollbackView.XOffset()
	displayLines, prefixWidth := m.scrollbackBuildDisplay(m.scrollbackView.Width())
	m.scrollbackView.SetContentLines(displayLines)
	m.scrollbackView.SetYOffset(yoff)
	m.scrollbackView.SetXOffset(xoff)
	m.scrollbackLineNumberPrefixWidth = prefixWidth
}

// toggleScrollbackMark toggles a bookmark for the given history line ID and
// rebuilds the display preserving offsets. No-op when id is zero.
func (m *Model) toggleScrollbackMark(id uint64) {
	if id == 0 {
		return
	}
	if m.scrollbackMarked == nil {
		m.scrollbackMarked = make(map[uint64]struct{}, 8)
	}
	if _, ok := m.scrollbackMarked[id]; ok {
		delete(m.scrollbackMarked, id)
	} else {
		m.scrollbackMarked[id] = struct{}{}
	}
	m.scrollbackRebuildDisplayPreserveOffsets()
	m.updateScrollbackSelectionStyle()
}

func (m *Model) scrollbackCopyAllSearchMatches() {
	if !m.scrollbackActive {
		return
	}
	if len(m.scrollbackSearchMatches) == 0 {
		m.messageBuffer = append(m.messageBuffer, "warning: no search matches to copy")
		return
	}
	lines := make([]string, 0, len(m.scrollbackSearchMatches))
	for _, hi := range m.scrollbackSearchMatches {
		if hi < 0 || hi >= len(m.scrollbackLines) {
			continue
		}
		text := strings.TrimRight(m.scrollbackLines[hi].Text, " ")
		if strings.TrimSpace(text) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%d: %s", hi+1, text))
	}
	if len(lines) == 0 {
		m.messageBuffer = append(m.messageBuffer, "warning: no non-empty search matches to copy")
		return
	}
	out := strings.Join(lines, "\n")
	if err := m.copySelection(out); err != nil {
		m.messageBuffer = append(m.messageBuffer, fmt.Sprintf("error: copying search matches: %v", err))
		return
	}
	m.messageBuffer = append(m.messageBuffer, "Copied search matches to clipboard.")
}

func (m *Model) scrollbackCopySearchMatchSubstrings() {
	if !m.scrollbackActive {
		return
	}
	if m.scrollbackSearchRe == nil || len(m.scrollbackSearchMatches) == 0 {
		m.messageBuffer = append(m.messageBuffer, "warning: no search matches to copy")
		return
	}
	out := make([]string, 0, len(m.scrollbackSearchMatches))
	for _, hi := range m.scrollbackSearchMatches {
		if hi < 0 || hi >= len(m.scrollbackLines) {
			continue
		}
		raw := m.scrollbackLines[hi].Text
		locs := m.scrollbackSearchRe.FindAllStringIndex(raw, -1)
		for _, loc := range locs {
			if len(loc) != 2 {
				continue
			}
			start, end := loc[0], loc[1]
			if start < 0 || end < 0 || start >= end || start > len(raw) || end > len(raw) {
				continue
			}
			match := raw[start:end]
			if match == "" {
				continue
			}
			out = append(out, match)
		}
	}
	if len(out) == 0 {
		m.messageBuffer = append(m.messageBuffer, "warning: no search matches to copy")
		return
	}
	text := strings.Join(out, "\n")
	if err := m.copySelection(text); err != nil {
		m.messageBuffer = append(m.messageBuffer, fmt.Sprintf("error: copying search matches: %v", err))
		return
	}
	m.messageBuffer = append(m.messageBuffer, "Copied search matches to clipboard.")
}

func (m *Model) updateScrollback(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "esc" {
			if m.scrollbackSearchActive {
				m.scrollbackSearchActive = false
				m.scrollbackSearchQuery = ""
				m.scrollbackSearchErr = ""
				return *m, nil, true
			}
			m.exitScrollback()
			return *m, nil, true
		}
		if m.scrollbackSearchActive {
			switch msg.String() {
			case "enter":
				m.scrollbackSearchLocked = m.scrollbackSearchQuery
				m.scrollbackSearchActive = false
				// Confirmed: jump to best match if valid.
				if m.scrollbackSearchErr == "" && m.scrollbackSearchIdx >= 0 {
					m.scrollbackSearchJumpToIdx(m.scrollbackSearchIdx)
				}
				m.scrollbackRebuildDisplayPreserveOffsets()
				m.updateScrollbackSelectionStyle()
				return *m, nil, true
			case "backspace":
				if m.scrollbackSearchQuery != "" {
					rs := []rune(m.scrollbackSearchQuery)
					m.scrollbackSearchQuery = string(rs[:len(rs)-1])
					m.scrollbackSearchRecompute(true)
					m.scrollbackRebuildDisplayPreserveOffsets()
					m.updateScrollbackSelectionStyle()
				}
				return *m, nil, true
			}
			// Accept any printable single rune, including shifted ones (uppercase
			// letters, regex metachars like ?, |, ^, $). Reject control-modifier
			// combos so Ctrl+L etc. don't end up in the query.
			if !msg.Mod.Contains(tea.ModCtrl) && !msg.Mod.Contains(tea.ModAlt) && !msg.Mod.Contains(tea.ModSuper) {
				if runes := []rune(msg.Text); len(runes) == 1 {
					m.scrollbackSearchQuery += msg.Text
					m.scrollbackSearchRecompute(true)
					m.scrollbackRebuildDisplayPreserveOffsets()
					m.updateScrollbackSelectionStyle()
					return *m, nil, true
				}
			}
			return *m, nil, true
		}
		// Prefer yank-matches over selection copy. Some terminals send shift+y as
		// Text=\"y\" with ModShift, others as Text=\"Y\".
		if msg.Text == "Y" || (msg.Text == "y" && msg.Mod.Contains(tea.ModShift)) || msg.String() == "Y" {
			m.scrollbackCopyAllSearchMatches()
			return *m, nil, true
		}
		// Copy match substrings (grep-like results). Handle shift+m variance.
		if msg.Text == "M" || (msg.Text == "m" && msg.Mod.Contains(tea.ModShift)) || msg.String() == "M" {
			m.scrollbackCopySearchMatchSubstrings()
			return *m, nil, true
		}
		if m.handleCopyKey(msg) {
			return *m, nil, true
		}
		switch msg.String() {
		case "up":
			if m.scrollbackCursorLine < 0 {
				m.setScrollbackCursorLine(0)
			} else {
				m.setScrollbackCursorLine(m.scrollbackCursorLine - 1)
			}
			return *m, nil, true
		case "down":
			if m.scrollbackCursorLine < 0 {
				m.setScrollbackCursorLine(0)
			} else {
				m.setScrollbackCursorLine(m.scrollbackCursorLine + 1)
			}
			return *m, nil, true
		}
		if msg.Mod == 0 {
			if runes := []rune(msg.Text); len(runes) == 1 {
				switch runes[0] {
				case '/':
					m.scrollbackSearchActive = true
					m.scrollbackSearchQuery = ""
					m.scrollbackSearchErr = ""
					m.scrollbackSearchRe = nil
					return *m, nil, true
				case 'n':
					if len(m.scrollbackSearchMatches) > 0 {
						if m.scrollbackSearchIdx < 0 {
							m.scrollbackSearchIdx = 0
						} else {
							m.scrollbackSearchIdx = (m.scrollbackSearchIdx + 1) % len(m.scrollbackSearchMatches)
						}
						m.scrollbackSearchJumpToIdx(m.scrollbackSearchIdx)
						m.updateScrollbackSelectionStyle()
					}
					return *m, nil, true
				case 'N':
					if len(m.scrollbackSearchMatches) > 0 {
						if m.scrollbackSearchIdx < 0 {
							m.scrollbackSearchIdx = 0
						} else {
							m.scrollbackSearchIdx--
							if m.scrollbackSearchIdx < 0 {
								m.scrollbackSearchIdx = len(m.scrollbackSearchMatches) - 1
							}
						}
						m.scrollbackSearchJumpToIdx(m.scrollbackSearchIdx)
						m.updateScrollbackSelectionStyle()
					}
					return *m, nil, true
				case 'm':
					if m.scrollbackCursorLine >= 0 && m.scrollbackCursorLine < len(m.scrollbackLines) {
						m.toggleScrollbackMark(m.scrollbackLines[m.scrollbackCursorLine].ID)
					}
					return *m, nil, true
				case 'r':
					m.refreshScrollbackView()
					return *m, nil, true
				case 'l':
					m.scrollbackShowLineNumbers = !m.scrollbackShowLineNumbers
					m.scrollbackRebuildDisplayPreserveOffsets()
					m.updateScrollbackSelectionStyle()
					return *m, nil, true
				case 'g':
					m.scrollbackView.GotoTop()
					return *m, nil, true
				case 'G':
					m.scrollbackView.GotoBottom()
					return *m, nil, true
				}
			}
		}
		var cmd tea.Cmd
		m.scrollbackView, cmd = m.scrollbackView.Update(msg)
		return *m, cmd, true
	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		m.scrollbackView, cmd = m.scrollbackView.Update(msg)
		return *m, cmd, true
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if row, col, ok := m.scrollbackContentPoint(msg.X, msg.Y); ok {
				line, ok := m.scrollbackHistoryLineAt(row)
				if !ok {
					return *m, nil, true
				}
				if len(m.scrollbackLines) > 0 {
					m.scrollbackCursorLine = line
					m.scrollbackCursorID = m.scrollbackLines[line].ID
				}
				// Double click: toggle mark on the clicked line.
				if m.scrollbackLastClickLine == line && !m.scrollbackLastClickAt.IsZero() && time.Since(m.scrollbackLastClickAt) < 350*time.Millisecond {
					m.scrollbackLastClickAt = time.Time{}
					m.scrollbackLastClickLine = -1
					m.setScrollbackCursorLine(line)
					m.toggleScrollbackMark(m.scrollbackCursorID)
					return *m, nil, true
				}
				m.scrollbackLastClickAt = time.Now()
				m.scrollbackLastClickLine = line
				// Second click on same single-line selection clears highlight (mark stays).
				if m.scrollbackPanel >= 0 && m.scrollbackPanel < len(m.selections) {
					sel := m.selections[m.scrollbackPanel]
					if sel.Source == selectSourceHistory && sel.Active && !sel.Dragging &&
						sel.StartRow == line && sel.EndRow == line {
						m.selections[m.scrollbackPanel] = panelSelection{}
						m.updateScrollbackSelectionStyle()
						return *m, nil, true
					}
				}

				m.setScrollbackCursorLine(line)
				m.beginScrollbackSelection(row, col)
				return *m, nil, true
			}
		}
		return *m, nil, true
	case tea.MouseMotionMsg:
		if msg.Button == tea.MouseLeft {
			if row, col, ok := m.scrollbackContentPoint(msg.X, msg.Y); ok {
				m.updateScrollbackSelection(row, col)
				return *m, nil, true
			}
		}
		return *m, nil, true
	case tea.MouseReleaseMsg:
		if msg.Button == tea.MouseLeft {
			if row, col, ok := m.scrollbackContentPoint(msg.X, msg.Y); ok {
				m.finishScrollbackSelection(row, col)
				return *m, nil, true
			}
		}
		return *m, nil, true
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizePanels()
		m.resizeScrollbackViewport()
		return *m, tea.ClearScreen, true
	}
	return *m, nil, false
}

func (m *Model) resizeScrollbackViewport() {
	if !m.scrollbackActive {
		return
	}
	offset := m.scrollbackView.YOffset()
	xoff := m.scrollbackView.XOffset()
	width, height := m.scrollbackViewportSize()
	m.scrollbackView.SetWidth(width)
	m.scrollbackView.SetHeight(height)
	displayLines, prefixWidth := m.scrollbackBuildDisplay(m.scrollbackView.Width())
	m.scrollbackView.SetContentLines(displayLines)
	m.scrollbackView.SetYOffset(offset)
	m.scrollbackView.SetXOffset(xoff)
	m.scrollbackLineNumberPrefixWidth = prefixWidth
	m.updateScrollbackSelectionStyle()
}

func (m Model) scrollbackHistoryLineAt(viewportRow int) (int, bool) {
	if !m.scrollbackActive || len(m.scrollbackLines) == 0 {
		return -1, false
	}
	absRow := m.scrollbackView.YOffset() + viewportRow
	if absRow < 0 || absRow >= len(m.scrollbackDisplayToHistory) {
		return -1, false
	}
	h := m.scrollbackDisplayToHistory[absRow]
	if h < 0 || h >= len(m.scrollbackLines) {
		return -1, false
	}
	return h, true
}

func (m *Model) scrollbackBuildDisplay(viewportWidth int) ([]string, int) {
	// Cache the highlight ANSI envelope (open/close) once per build instead of
	// invoking lipgloss.Style.Render per match. style.Render allocates a fresh
	// ANSI-wrapped string on every call; the envelope is identical across rows
	// so we extract it once with a sentinel and write the open/close directly.
	var hlOpen, hlClose string
	if m.scrollbackSearchRe != nil {
		highlightStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(m.theme.color(m.theme.OverlayFG)).
			Background(m.theme.color(m.theme.StatusActivePanelBG))
		const sentinel = "\x00"
		rendered := highlightStyle.Render(sentinel)
		if i := strings.Index(rendered, sentinel); i >= 0 {
			hlOpen = rendered[:i]
			hlClose = rendered[i+len(sentinel):]
		}
	}

	var displayLines []string
	var prefixWidth int
	if len(m.scrollbackLines) > 0 {
		// Reuse the backing slice across calls; per-keystroke search recompute
		// would otherwise allocate a fresh []string of N entries every time.
		n := len(m.scrollbackLines)
		if cap(m.scrollbackDisplayBuf) < n {
			m.scrollbackDisplayBuf = make([]string, n)
		} else {
			m.scrollbackDisplayBuf = m.scrollbackDisplayBuf[:n]
		}
		displayLines = m.scrollbackDisplayBuf

		if !m.scrollbackShowLineNumbers {
			for i := range m.scrollbackLines {
				displayLines[i] = highlightMatches(m.scrollbackLines[i].Text, m.scrollbackSearchRe, hlOpen, hlClose)
			}
		} else {
			digits := len(strconv.Itoa(len(m.scrollbackLines)))
			prefixWidth = digits + 5
			var b strings.Builder
			for i, line := range m.scrollbackLines {
				mark := " "
				if m.scrollbackMarked != nil {
					if _, ok := m.scrollbackMarked[line.ID]; ok {
						mark = "●"
					}
				}
				txt := highlightMatches(line.Text, m.scrollbackSearchRe, hlOpen, hlClose)
				// Hand-rolled formatting: avoids fmt parse cost in the per-row
				// hot path; benchmarked materially cheaper than fmt.Sprintf.
				num := strconv.Itoa(i + 1)
				b.Reset()
				b.Grow(prefixWidth + len(txt))
				for k := digits - len(num); k > 0; k-- {
					b.WriteByte(' ')
				}
				b.WriteString(num)
				b.WriteString(" │ ")
				b.WriteString(mark)
				b.WriteByte(' ')
				b.WriteString(txt)
				displayLines[i] = b.String()
			}
		}
	}

	m.scrollbackDisplayToHistory = make([]int, 0, len(displayLines)+len(m.scrollbackRefreshMarkers))
	m.scrollbackHistoryToDisplay = make([]int, len(m.scrollbackLines))
	for i := range m.scrollbackHistoryToDisplay {
		m.scrollbackHistoryToDisplay[i] = -1
	}

	// Fast path: no refresh markers at all. Skip building the AfterID->times
	// map entirely (saves an alloc per recompute for sessions that never
	// pressed `r`). nil maps are safe to read; we just never enter the
	// per-line marker check below.
	var after map[uint64][]time.Time
	if len(m.scrollbackRefreshMarkers) > 0 {
		after = make(map[uint64][]time.Time, len(m.scrollbackRefreshMarkers))
		for _, mk := range m.scrollbackRefreshMarkers {
			after[mk.AfterID] = append(after[mk.AfterID], mk.At)
		}
	}

	prefix := ""
	if m.scrollbackShowLineNumbers && prefixWidth > 0 {
		prefix = strings.Repeat(" ", prefixWidth)
	}
	innerW := viewportWidth
	if innerW < 1 {
		innerW = 1
	}
	if m.scrollbackShowLineNumbers {
		innerW -= prefixWidth
		if innerW < 1 {
			innerW = 1
		}
	}

	appendMarkers := func(out []string, times []time.Time) []string {
		for _, at := range times {
			out = append(out, m.buildRefreshMarkerLine(at, prefix, innerW))
			m.scrollbackDisplayToHistory = append(m.scrollbackDisplayToHistory, -1)
		}
		return out
	}

	if len(displayLines) == 0 {
		if times, ok := after[0]; ok {
			out := make([]string, 0, len(times))
			out = appendMarkers(out, times)
			return out, prefixWidth
		}
		m.scrollbackDisplayToHistory = nil
		m.scrollbackHistoryToDisplay = nil
		return nil, prefixWidth
	}

	out := make([]string, 0, len(displayLines)+len(m.scrollbackRefreshMarkers))
	if times, ok := after[0]; ok {
		out = appendMarkers(out, times)
	}
	for hi := range m.scrollbackLines {
		m.scrollbackHistoryToDisplay[hi] = len(out)
		out = append(out, displayLines[hi])
		m.scrollbackDisplayToHistory = append(m.scrollbackDisplayToHistory, hi)
		if after != nil {
			if times, ok := after[m.scrollbackLines[hi].ID]; ok {
				out = appendMarkers(out, times)
			}
		}
	}
	return out, prefixWidth
}

// buildRefreshMarkerLine renders a single full-width refresh marker rule with
// a centered timestamp label, prefixed by `prefix` (which aligns under the
// line-number gutter when enabled).
func (m *Model) buildRefreshMarkerLine(at time.Time, prefix string, innerW int) string {
	label := "refreshed at " + at.Format("2006-01-02 15:04:05")
	need := ansi.StringWidth(label) + 2
	if need > innerW {
		label = ansi.Truncate(label, max(0, innerW-2), "")
		need = ansi.StringWidth(label) + 2
	}
	ruleW := innerW - need
	leftW := ruleW / 2
	rightW := ruleW - leftW
	rule := strings.Repeat("─", leftW) + " " + label + " " + strings.Repeat("─", rightW)
	rule = padOrTruncate(rule, innerW)
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(m.theme.color(m.theme.StatusHintFG)).
		Render(prefix + rule)
}

func (m Model) scrollbackViewportSize() (int, int) {
	width := m.width - 2
	if width < 1 {
		width = 1
	}
	height := m.gridHeight() - 3
	if height < 1 {
		height = 1
	}
	return width, height
}

// highlightMatches wraps regex matches in `s` with the precomputed ANSI envelope
// (open/close). Reusing a single envelope across rows avoids repeated
// lipgloss.Style.Render allocations in the per-keystroke search hot path.
func highlightMatches(s string, re *regexp.Regexp, open, close string) string {
	if re == nil || s == "" {
		return s
	}
	locs := re.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + len(locs)*(len(open)+len(close)))
	last := 0
	for _, loc := range locs {
		if len(loc) != 2 {
			continue
		}
		start, end := loc[0], loc[1]
		if start < last {
			continue
		}
		if start > len(s) || end > len(s) || start >= end {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(open)
		b.WriteString(s[start:end])
		b.WriteString(close)
		last = end
	}
	if last < len(s) {
		b.WriteString(s[last:])
	}
	return b.String()
}

func (m Model) scrollbackContentPoint(x, y int) (row, col int, ok bool) {
	if !m.scrollbackActive || x < 1 || y < 2 {
		return 0, 0, false
	}
	width := max(1, m.scrollbackView.Width())
	height := max(1, m.scrollbackView.Height())
	if x >= 1+width || y >= 2+height {
		return 0, 0, false
	}
	displayCol := m.scrollbackView.XOffset() + x - 1
	if m.scrollbackShowLineNumbers {
		displayCol -= m.scrollbackLineNumberPrefixWidth
		if displayCol < 0 {
			displayCol = 0
		}
	}
	return y - 2, displayCol, true
}

func (m *Model) beginScrollbackSelection(row, col int) {
	// Click starts an active selection so highlight appears immediately.
	m.setScrollbackSelection(row, col, true)
}

func (m *Model) updateScrollbackSelection(row, col int) {
	m.setScrollbackSelection(row, col, true)
}

func (m *Model) finishScrollbackSelection(row, col int) {
	if m.scrollbackPanel < 0 || m.scrollbackPanel >= len(m.selections) {
		return
	}
	sel := m.selections[m.scrollbackPanel]
	if sel.Dragging && sel.Active {
		m.setScrollbackSelection(row, col, true)
		sel = m.selections[m.scrollbackPanel]
	}
	if !sel.Active {
		m.selections[m.scrollbackPanel] = panelSelection{}
		m.updateScrollbackSelectionStyle()
		return
	}
	sel.Dragging = false
	m.selections[m.scrollbackPanel] = sel
	m.updateScrollbackSelectionStyle()
}

func (m *Model) setScrollbackSelection(row, col int, active bool) {
	if !m.scrollbackActive || m.scrollbackPanel < 0 || m.scrollbackPanel >= len(m.panels) {
		return
	}
	m.ensureScrollState()
	if len(m.scrollbackLines) == 0 {
		return
	}
	line, ok := m.scrollbackHistoryLineAt(row)
	if !ok {
		return
	}
	col = max(0, col)

	idx := m.scrollbackPanel
	sel := m.selections[idx]
	if !sel.Dragging || sel.Source != selectSourceHistory {
		sel = panelSelection{
			Dragging: true,
			Source:   selectSourceHistory,
			StartRow: line,
			StartCol: col,
			EndRow:   line,
			EndCol:   col,
		}
	} else {
		sel.EndRow = line
		sel.EndCol = col
	}
	sel.Active = active
	m.selections[idx] = sel
	m.updateScrollbackSelectionStyle()
}

func (m *Model) updateScrollbackSelectionStyle() {
	if !m.scrollbackActive || m.scrollbackPanel < 0 || m.scrollbackPanel >= len(m.selections) {
		return
	}
	sel := m.selections[m.scrollbackPanel]
	if len(m.scrollbackLines) == 0 {
		m.scrollbackView.StyleLineFunc = nil
		return
	}

	// Capture selection bounds instead of building a set: viewport renders only
	// a window of rows at a time (~screen height), so a range check is cheaper
	// than allocating and populating a map every selection update / drag tick.
	var selActive bool
	var selStart, selEnd int
	if sel.Source == selectSourceHistory && (sel.Active || sel.Dragging) {
		s, _, e, _ := normalizeSelection(sel.StartRow, sel.StartCol, sel.EndRow, sel.EndCol)
		selStart = clamp(s, 0, len(m.scrollbackLines)-1)
		selEnd = clamp(e, 0, len(m.scrollbackLines)-1)
		selActive = true
	}

	selectionStyle := lipgloss.NewStyle().
		Foreground(m.theme.color(m.theme.StatusBarFG)).
		Background(m.theme.color(m.theme.StatusActivePanelBG))
	bookmarkStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.theme.color(m.theme.OverlayFG)).
		Background(m.theme.color(m.theme.ActiveNormalTitleBG))
	searchMatchStyle := lipgloss.NewStyle().
		Foreground(m.theme.color(m.theme.StatusBarFG)).
		Background(m.theme.color(m.theme.StatusHintBG))
	searchCurrentStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.theme.color(m.theme.OverlayFG)).
		Background(m.theme.color(m.theme.StatusModeNormalBG))

	m.scrollbackView.StyleLineFunc = func(line int) lipgloss.Style {
		if line < 0 || line >= len(m.scrollbackDisplayToHistory) {
			return lipgloss.NewStyle()
		}
		hline := m.scrollbackDisplayToHistory[line]
		if hline < 0 {
			// Marker row.
			return lipgloss.NewStyle().
				Bold(true).
				Foreground(m.theme.color(m.theme.StatusHintFG))
		}
		if hline >= len(m.scrollbackLines) {
			return lipgloss.NewStyle()
		}
		if selActive && hline >= selStart && hline <= selEnd &&
			strings.TrimSpace(m.scrollbackLines[hline].Text) != "" {
			return selectionStyle
		}
		if m.scrollbackMarked != nil {
			if _, ok := m.scrollbackMarked[m.scrollbackLines[hline].ID]; ok {
				return bookmarkStyle
			}
		}
		if hline < len(m.scrollbackSearchMatchHit) && m.scrollbackSearchMatchHit[hline] {
			if m.scrollbackSearchIdx >= 0 &&
				m.scrollbackSearchIdx < len(m.scrollbackSearchMatches) &&
				m.scrollbackSearchMatches[m.scrollbackSearchIdx] == hline {
				return searchCurrentStyle
			}
			return searchMatchStyle
		}
		return lipgloss.NewStyle()
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
	if len(m.selections) != n {
		m.selections = resizeSelectionSlice(m.selections, n)
	}
}

func (m Model) formatStartupLog(idx int, line string) string {
	if idx < 0 || idx >= len(m.startupItems) {
		return line
	}
	return fmt.Sprintf("[%s] %s", m.startupItems[idx].Label, line)
}

func (m Model) formatTeardownLog(idx int, line string) string {
	if idx < 0 || idx >= len(m.teardownItems) {
		return line
	}
	return fmt.Sprintf("[teardown:%s] %s", m.teardownItems[idx].Label, line)
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

func (m *Model) hasActiveSelection() bool {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return false
	}
	m.ensureScrollState()
	return m.selections[m.activePanel].Active
}

func (m *Model) clearActiveSelection() bool {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return false
	}
	m.ensureScrollState()
	sel := m.selections[m.activePanel]
	if !sel.Active && !sel.Dragging {
		return false
	}
	m.selections[m.activePanel] = panelSelection{}
	return true
}

func (m *Model) clearAllSelections() bool {
	m.ensureScrollState()
	cleared := false
	for i, sel := range m.selections {
		if sel.Active || sel.Dragging {
			m.selections[i] = panelSelection{}
			cleared = true
		}
	}
	return cleared
}

func (m *Model) handleCopyKey(msg tea.KeyPressMsg) bool {
	if !m.hasActiveSelection() {
		return false
	}
	if msg.String() == "enter" {
		m.copyCurrentSelection()
		return true
	}
	if msg.Mod == 0 {
		if runes := []rune(msg.Text); len(runes) == 1 {
			switch runes[0] {
			case 'y', 'Y':
				m.copyCurrentSelection()
				return true
			}
		}
	}
	return false
}

func (m *Model) beginSelection(row, col int) {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	m.ensureScrollState()
	idx := m.activePanel
	m.selections[idx] = panelSelection{
		Dragging: true,
		Source:   selectSourceLive,
		StartRow: row,
		StartCol: col,
		EndRow:   row,
		EndCol:   col,
	}
}

func (m *Model) startSelection(row, col int) {
	if m.activePanel < 0 || m.activePanel >= len(m.panels) {
		return
	}
	m.ensureScrollState()
	idx := m.activePanel
	sel := panelSelection{Source: selectSourceLive}
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
	m.ensureScrollState()
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
	m.ensureScrollState()
	idx := m.activePanel
	sel := m.selections[idx]
	if !sel.Dragging {
		return
	}
	if !sel.Active {
		m.selections[idx] = panelSelection{}
		return
	}
	sel.Active = true
	sel.Dragging = false
	sel.EndRow = row
	sel.EndCol = col
	m.selections[idx] = sel
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
	if idx < len(m.selections) && m.selections[idx].Active {
		if m.selections[idx].Source != selectSourceLive {
			return nil
		}
		return m.selectViewportForPanel(idx, pageSize)
	}
	return nil
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

	view := m.displayForView(m.panels[idx])
	rawLines, _ := visibleViewportLines(view.Output, pageSize, -1)
	lines := make([]string, pageSize)
	plainLines := make([]string, pageSize)
	for i := 0; i < pageSize; i++ {
		if i < len(rawLines) {
			lines[i] = padOrTruncate(rawLines[i], width)
			plainLines[i] = padOrTruncate(ansi.Strip(rawLines[i]), width)
			continue
		}
		fill := strings.Repeat(" ", width)
		lines[i] = fill
		plainLines[i] = fill
	}
	return lines, plainLines
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
	if sel.Source == selectSourceHistory && m.scrollbackActive && idx == m.scrollbackPanel {
		return m.scrollbackSelectionText(sel)
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
		fragment := strings.TrimRight(sliceByCells(lines[row], lineStart, lineEnd), " ")
		if strings.TrimSpace(fragment) == "" {
			continue
		}
		selected = append(selected, fragment)
	}
	return strings.Join(selected, "\n")
}

func (m *Model) scrollbackSelectionText(sel panelSelection) string {
	if len(m.scrollbackLines) == 0 {
		return ""
	}
	startRow, startCol, endRow, endCol := normalizeSelection(sel.StartRow, sel.StartCol, sel.EndRow, sel.EndCol)
	startRow = clamp(startRow, 0, len(m.scrollbackLines)-1)
	endRow = clamp(endRow, 0, len(m.scrollbackLines)-1)

	visibleWidth := max(1, m.scrollbackView.Width())
	visibleStart := max(0, m.scrollbackView.XOffset())
	visibleEnd := visibleStart + visibleWidth
	if m.scrollbackShowLineNumbers {
		visibleStart -= m.scrollbackLineNumberPrefixWidth
		visibleEnd -= m.scrollbackLineNumberPrefixWidth
		if visibleStart < 0 {
			visibleStart = 0
		}
		if visibleEnd < 0 {
			visibleEnd = 0
		}
	}
	startCol = max(0, startCol)
	endCol = max(0, endCol)

	selected := make([]string, 0, endRow-startRow+1)
	for row := startRow; row <= endRow; row++ {
		lineStart := visibleStart
		lineEnd := visibleEnd
		if row == startRow {
			lineStart = startCol
		}
		if row == endRow {
			lineEnd = endCol + 1
		}
		if lineEnd < lineStart {
			lineStart, lineEnd = lineEnd, lineStart
		}
		fragment := strings.TrimRight(sliceByCells(m.scrollbackLines[row].Text, lineStart, lineEnd), " ")
		if strings.TrimSpace(fragment) == "" {
			continue
		}
		selected = append(selected, fragment)
	}
	return strings.Join(selected, "\n")
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
	case "NORMAL", "SCROLLBACK":
		modeFG = m.theme.color(m.theme.StatusModeNormalFG)
		modeBG = m.theme.color(m.theme.StatusModeNormalBG)
	case "INSERT":
		modeFG = m.theme.color(m.theme.StatusModeInsertFG)
		modeBG = m.theme.color(m.theme.StatusModeInsertBG)
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

func keyMsgToBytes(msg tea.KeyPressMsg) []byte {
	if msg.Text != "" {
		payload := []byte(msg.Text)
		if msg.Mod.Contains(tea.ModAlt) {
			return append([]byte{0x1b}, payload...)
		}
		return payload
	}

	if msg.Mod == tea.ModCtrl && msg.Code >= 'a' && msg.Code <= 'z' {
		return []byte{byte(msg.Code - 'a' + 1)}
	}

	switch msg.String() {
	case "ctrl+@":
		return []byte{0x00}
	case "esc":
		return []byte{0x1b}
	case "enter":
		return []byte{'\r'}
	case "tab":
		return []byte{'\t'}
	case "backspace":
		return []byte{0x7f}
	case "space":
		return []byte{' '}
	case "up":
		return []byte("\x1b[A")
	case "down":
		return []byte("\x1b[B")
	case "right":
		return []byte("\x1b[C")
	case "left":
		return []byte("\x1b[D")
	}

	return nil
}
