// SPDX-License-Identifier: MIT
package ui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/rikvanderkemp/muxedo/internal/process"
	"github.com/rikvanderkemp/muxedo/internal/profile"
)

type quitOnStartupCompleteModel struct {
	inner Model
}

func (m quitOnStartupCompleteModel) Init() tea.Cmd {
	return m.inner.Init()
}

func (m quitOnStartupCompleteModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.inner.Update(msg)
	m.inner = next.(Model)
	if _, ok := msg.(StartupCompleteMsg); ok && m.inner.startupCompleted {
		return m, tea.Quit
	}
	return m, cmd
}

func (m quitOnStartupCompleteModel) View() string {
	return m.inner.View()
}

func runProgramForTest(t *testing.T, model tea.Model, timeout time.Duration, send func(*tea.Program)) (tea.Model, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var out bytes.Buffer
	prog := tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithInput(nil),
		tea.WithOutput(&out),
		tea.WithoutRenderer(),
		tea.WithoutSignals(),
	)
	if send != nil {
		go send(prog)
	}

	final, err := prog.Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		t.Fatalf("program timed out after %v", timeout)
	}
	return final, err
}

func historyLinesOf(lines ...string) []process.HistoryLine {
	out := make([]process.HistoryLine, len(lines))
	for i, line := range lines {
		out[i] = process.HistoryLine{ID: uint64(i + 1), Text: line}
	}
	return out
}

func TestUpdateClickActivatesPanel(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)

	next, _ = model.Update(tea.MouseMsg{
		X:      1,
		Y:      1,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	model = next.(Model)

	if model.activePanel != 0 {
		t.Fatalf("expected active panel 0, got %d", model.activePanel)
	}
}

func TestStatusBarClickDoesNotActivatePanel(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})

	const h = 40
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: h})
	model = next.(Model)

	next, _ = model.Update(tea.MouseMsg{
		X:      1,
		Y:      h - 1,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	model = next.(Model)

	if model.activePanel != -1 {
		t.Fatalf("expected no active panel after status bar click, got %d", model.activePanel)
	}

	next, _ = model.Update(tea.MouseMsg{
		X:      1,
		Y:      1,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	model = next.(Model)

	if model.activePanel != 0 {
		t.Fatalf("expected active panel 0 after grid click, got %d", model.activePanel)
	}
}

func TestStoppedPanelStaysActiveAcrossTickAfterClick(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.panelRunning = func(*process.Panel) bool { return false }

	next, _ := model.Update(tea.WindowSizeMsg{Width: 220, Height: 24})
	model = next.(Model)

	next, _ = model.Update(tickMsg{})
	model = next.(Model)

	next, _ = model.Update(tea.MouseMsg{
		X:      1,
		Y:      1,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("expected pane 0 active after click, got %d", model.activePanel)
	}

	next, _ = model.Update(tickMsg{})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("expected pane 0 to stay active across tick, got %d", model.activePanel)
	}
}

func TestTickDeactivatesWhenActivePanelStops(t *testing.T) {
	var running = true
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return running }

	next, _ := model.Update(tea.WindowSizeMsg{Width: 220, Height: 24})
	model = next.(Model)

	next, _ = model.Update(tickMsg{})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("expected active panel 0 while running, got %d", model.activePanel)
	}

	model.panelInsertMode = true
	running = false
	next, _ = model.Update(tickMsg{})
	model = next.(Model)
	if model.activePanel != -1 {
		t.Fatalf("expected no active panel after process stopped, got %d", model.activePanel)
	}
	if model.panelInsertMode {
		t.Fatal("expected insert mode cleared when process stopped")
	}
}

func TestEscapeFromNormalBlursPanel(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)

	if model.activePanel != -1 {
		t.Fatalf("expected no active panel, got %d", model.activePanel)
	}
	if model.panelInsertMode {
		t.Fatal("expected panelInsertMode false after blur")
	}
}

func TestEscapeFromMaximizedNormalRestoresGridAndBlursPanel(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.maximizedPanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)

	if model.activePanel != -1 {
		t.Fatalf("expected no active panel, got %d", model.activePanel)
	}
	if model.maximizedPanel != -1 {
		t.Fatalf("expected maximize cleared, got %d", model.maximizedPanel)
	}
	if model.panelInsertMode {
		t.Fatal("expected panelInsertMode false after blur")
	}
}

func TestEscapeTrickleInsertToNormalThenBlur(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = next.(Model)
	if !model.panelInsertMode || model.activePanel != 0 {
		t.Fatalf("want insert + focused 0, got insert=%v panel=%d", model.panelInsertMode, model.activePanel)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.panelInsertMode || model.activePanel != 0 {
		t.Fatalf("want normal + focused after 1st Esc, got insert=%v panel=%d", model.panelInsertMode, model.activePanel)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.activePanel != -1 || model.panelInsertMode {
		t.Fatalf("want blurred after 2nd Esc, panel=%d insert=%v", model.activePanel, model.panelInsertMode)
	}
}

func TestUpdateActivePanelCapturesKeyboard(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelRunning = func(p *process.Panel) bool { return true }

	var writes [][]byte
	model.sendInput = func(_ *process.Panel, data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		writes = append(writes, cp)
		return nil
	}

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("expected no cmd entering insert")
	}
	if !model.panelInsertMode {
		t.Fatal("expected insert mode")
	}

	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("expected no quit command while panel is active")
	}

	next, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("expected ctrl+c to be ignored by active panel")
	}

	if len(writes) != 1 {
		t.Fatalf("expected 1 key write (only 'q'), got %d", len(writes))
	}
	if string(writes[0]) != "q" {
		t.Fatalf("expected first write to be q, got %q", string(writes[0]))
	}
}

func TestStoppedPanelIgnoresNonReloadKeys(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0

	var writes [][]byte
	model.sendInput = func(_ *process.Panel, data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		writes = append(writes, cp)
		return nil
	}
	model.panelRunning = func(p *process.Panel) bool { return false }

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	_ = next.(Model)
	if cmd != nil {
		t.Fatalf("expected nil cmd for non-reload key on stopped panel")
	}
	if len(writes) != 0 {
		t.Fatalf("expected no writes to stopped panel, got %d", len(writes))
	}
}

func TestStoppedPanelRestartsOnR(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0

	var restarted bool
	model.restartPanel = func(p *process.Panel) error {
		restarted = true
		return nil
	}
	model.panelRunning = func(p *process.Panel) bool { return false }

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	_ = next.(Model)
	if cmd != nil {
		t.Fatalf("expected nil cmd on restart")
	}
	if !restarted {
		t.Fatal("expected panel to be restarted on 'r'")
	}
}

func TestStoppedPanelRestartsOnUpperR(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0

	var restarted bool
	model.restartPanel = func(p *process.Panel) error {
		restarted = true
		return nil
	}
	model.panelRunning = func(p *process.Panel) bool { return false }

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	_ = next.(Model)
	if cmd != nil {
		t.Fatalf("expected nil cmd on restart")
	}
	if !restarted {
		t.Fatal("expected panel to be restarted on 'R'")
	}
}

func TestRunningPanelNormalDoesNotForwardR(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0

	var writes [][]byte
	model.sendInput = func(_ *process.Panel, data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		writes = append(writes, cp)
		return nil
	}
	var restarted bool
	model.restartPanel = func(*process.Panel) error {
		restarted = true
		return nil
	}
	model.panelRunning = func(p *process.Panel) bool { return true }

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	_ = next.(Model)

	if len(writes) != 0 {
		t.Fatalf("expected no forward in normal mode, got %v", writes)
	}
	if !restarted {
		t.Fatal("expected restart on r in normal mode")
	}
}

func TestRunningPanelForwardsAfterInsert(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0

	var writes [][]byte
	model.sendInput = func(_ *process.Panel, data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		writes = append(writes, cp)
		return nil
	}
	model.panelRunning = func(p *process.Panel) bool { return true }

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	_ = next.(Model)

	if len(writes) != 1 || string(writes[0]) != "r" {
		t.Fatalf("expected 'r' forwarded in insert mode, got %v", writes)
	}
}

func TestRunningPanelForwardsMInInsertMode(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0

	var writes [][]byte
	model.sendInput = func(_ *process.Panel, data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		writes = append(writes, cp)
		return nil
	}
	model.panelRunning = func(p *process.Panel) bool { return true }

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = next.(Model)

	if model.maximizedPanel != -1 {
		t.Fatalf("expected maximize unchanged in insert mode, got %d", model.maximizedPanel)
	}
	if len(writes) != 1 || string(writes[0]) != "m" {
		t.Fatalf("expected 'm' forwarded in insert mode, got %v", writes)
	}
}

func TestRunningPanelInsertModeLogsSendInputError(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelInsertMode = true
	model.panelRunning = func(*process.Panel) bool { return true }
	model.sendInput = func(*process.Panel, []byte) error {
		return io.ErrClosedPipe
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = next.(Model)

	if len(model.messageBuffer) != 1 {
		t.Fatalf("messageBuffer len = %d, want 1", len(model.messageBuffer))
	}
	if !strings.Contains(model.messageBuffer[0], "error: sending input to panel one") {
		t.Fatalf("messageBuffer[0] = %q", model.messageBuffer[0])
	}
}

func TestRunningPanelNormalSwallowsUnknownRune(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0

	var writes [][]byte
	model.sendInput = func(_ *process.Panel, data []byte) error {
		writes = append(writes, data)
		return nil
	}
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	_ = next.(Model)
	if len(writes) != 0 {
		t.Fatalf("expected no write in normal mode, got %v", writes)
	}
}

func TestSelectionLinesForPanelLiveStripsANSIWithoutChangingWindow(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 40
	model.height = 8
	model.panelRunning = func(*process.Panel) bool { return true }
	model.displayForView = func(*process.Panel) process.DisplayState {
		return process.DisplayState{
			Output: "\x1b[31mred\x1b[0m\nplain\n",
		}
	}

	lines, plain := model.selectionLinesForPanel(0, 3)
	if len(lines) != 3 || len(plain) != 3 {
		t.Fatalf("unexpected lengths: lines=%d plain=%d", len(lines), len(plain))
	}
	if !strings.Contains(lines[0], "\x1b[31mred\x1b[0m") {
		t.Fatalf("styled line lost ANSI content: %q", lines[0])
	}
	if plain[0] != padOrTruncate("red", model.activePaneContentWidth()) {
		t.Fatalf("plain line = %q, want stripped red", plain[0])
	}
	if ansi.StringWidth(lines[0]) != ansi.StringWidth(plain[0]) {
		t.Fatalf("width mismatch styled=%d plain=%d", ansi.StringWidth(lines[0]), ansi.StringWidth(plain[0]))
	}
	if strings.TrimRight(ansi.Strip(lines[1]), " ") != strings.TrimRight(plain[1], " ") {
		t.Fatalf("row alignment mismatch: styled=%q plain=%q", lines[1], plain[1])
	}
}

func TestKillPanelCmdIncludesKillCommandFailure(t *testing.T) {
	panel := process.NewWithCommandSpec("one", process.CommandSpec{Shell: "true"}, process.CommandSpec{Program: "__definitely_missing_muxedo_binary__"}, ".")

	msg := killPanelCmd(0, panel)().(exitProgressMsg)
	if !strings.Contains(msg.status, "kill command failed") {
		t.Fatalf("status = %q, want kill command failure", msg.status)
	}
}

func TestStreamStartupOutputAcceptsLongLines(t *testing.T) {
	model := NewModel(nil)
	model.msgChan = make(chan tea.Msg, 2)

	done := make(chan struct{}, 1)
	line := strings.Repeat("x", 70*1024)

	model.streamStartupOutput(3, strings.NewReader(line+"\n"), done)
	<-done

	msg := (<-model.msgChan).(startupLogMsg)
	if msg.idx != 3 {
		t.Fatalf("msg.idx = %d, want 3", msg.idx)
	}
	if len(msg.line) != len(line) {
		t.Fatalf("len(msg.line) = %d, want %d", len(msg.line), len(line))
	}
	if msg.line != line {
		t.Fatal("long startup line truncated or changed")
	}
}

func TestClickResetsInsertMode(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)

	next, _ = model.Update(tea.MouseMsg{
		X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	model = next.(Model)

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	model = next.(Model)
	if !model.panelInsertMode {
		t.Fatal("expected insert after I")
	}

	next, _ = model.Update(tea.MouseMsg{
		X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	model = next.(Model)
	if model.panelInsertMode {
		t.Fatal("expected normal mode after re-click panel")
	}
}

func TestMWithoutActivePanelDoesNothing(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = next.(Model)

	if cmd != nil {
		t.Fatal("expected nil cmd when no panel is active")
	}
	if model.maximizedPanel != -1 {
		t.Fatalf("expected no maximized panel, got %d", model.maximizedPanel)
	}
}

func TestMFocusedNormalTogglesMaximize(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = next.(Model)
	if model.maximizedPanel != 0 {
		t.Fatalf("expected panel 0 maximized, got %d", model.maximizedPanel)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = next.(Model)
	if model.maximizedPanel != -1 {
		t.Fatalf("expected maximize cleared, got %d", model.maximizedPanel)
	}
}

func TestMaximizeResizesOnlyVisiblePanelAndRestoreResizesGrid(t *testing.T) {
	panels := []*process.Panel{
		process.New("one", "echo one", "", "."),
		process.New("two", "echo two", "", "."),
	}
	model := NewModel(panels)
	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)

	gridCols0, gridRows0 := panels[0].TerminalSize()
	gridCols1, gridRows1 := panels[1].TerminalSize()
	if gridCols0 != 58 || gridRows0 != 36 || gridCols1 != 58 || gridRows1 != 36 {
		t.Fatalf("unexpected grid sizes: p0=%dx%d p1=%dx%d", gridCols0, gridRows0, gridCols1, gridRows1)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = next.(Model)

	maxCols0, maxRows0 := panels[0].TerminalSize()
	maxCols1, maxRows1 := panels[1].TerminalSize()
	if maxCols0 != 118 || maxRows0 != 36 {
		t.Fatalf("expected maximized panel resized to 118x36, got %dx%d", maxCols0, maxRows0)
	}
	if maxCols1 != gridCols1 || maxRows1 != gridRows1 {
		t.Fatalf("expected hidden panel to keep grid size, got %dx%d", maxCols1, maxRows1)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = next.(Model)

	restoreCols0, restoreRows0 := panels[0].TerminalSize()
	restoreCols1, restoreRows1 := panels[1].TerminalSize()
	if restoreCols0 != 58 || restoreRows0 != 36 || restoreCols1 != 58 || restoreRows1 != 36 {
		t.Fatalf("expected both panels restored to grid size, got p0=%dx%d p1=%dx%d", restoreCols0, restoreRows0, restoreCols1, restoreRows1)
	}
}

func TestNewModelWithSpecsDefaultsStartupItemsToAsync(t *testing.T) {
	model := NewModelWithSpecs("test", []profile.StartupSpec{
		{
			Command: process.CommandSpec{Program: "echo", Args: []string{"hi"}},
			Mode:    profile.StartupModeAsync,
		},
	}, nil, profile.ScrollbackConfig{}, DefaultTheme())

	if len(model.startupItems) != 1 {
		t.Fatalf("len(startupItems) = %d, want 1", len(model.startupItems))
	}
	if model.startupItems[0].Mode != profile.StartupModeAsync {
		t.Fatalf("startupItems[0].Mode = %q, want %q", model.startupItems[0].Mode, profile.StartupModeAsync)
	}
}

func TestStartupBufferShowsStatusRowsAndLogs(t *testing.T) {
	model := NewModelWithSpecs("test", []profile.StartupSpec{
		{
			Command: process.CommandSpec{Program: "echo", Args: []string{"one"}},
			Mode:    profile.StartupModeAsync,
		},
	}, nil, profile.ScrollbackConfig{}, DefaultTheme())
	model.width = 100
	model.height = 12

	next, _ := model.Update(startupStatusMsg{idx: 0, status: startupStatusRunning})
	model = next.(Model)
	next, _ = model.Update(startupLogMsg{idx: 0, line: "warming up"})
	model = next.(Model)

	view := model.renderMessageBuffer()
	if !strings.Contains(view, "Startup status:") {
		t.Fatalf("renderMessageBuffer() missing startup header: %q", view)
	}
	if !strings.Contains(view, "Starting echo one [async] ->") {
		t.Fatalf("renderMessageBuffer() missing startup status row: %q", view)
	}
	if !strings.Contains(view, "[echo one] warming up") {
		t.Fatalf("renderMessageBuffer() missing startup log line: %q", view)
	}
}

func TestStartupTickAdvancesSpinner(t *testing.T) {
	model := NewModelWithSpecs("test", []profile.StartupSpec{
		{
			Command: process.CommandSpec{Program: "echo", Args: []string{"one"}},
			Mode:    profile.StartupModeAsync,
		},
	}, nil, profile.ScrollbackConfig{}, DefaultTheme())

	next, _ := model.Update(startupStatusMsg{idx: 0, status: startupStatusRunning})
	model = next.(Model)
	frame := model.startupItems[0].Spinner

	next, _ = model.Update(tickMsg{})
	model = next.(Model)
	if model.startupItems[0].Spinner == frame {
		t.Fatal("expected startup spinner to advance on tick")
	}
}

func TestStartupCompleteKeepsListeningForLogs(t *testing.T) {
	model := NewModelWithSpecs("test", nil, nil,
		profile.ScrollbackConfig{}, DefaultTheme())

	next, cmd := model.Update(StartupCompleteMsg{})
	model = next.(Model)
	if !model.startupCompleted {
		t.Fatal("expected startupCompleted true")
	}
	if cmd == nil {
		t.Fatal("expected waitForMsg command after startup completion")
	}

	go func() {
		model.msgChan <- LogMsg("late log")
	}()
	msg := cmd()
	if got, ok := msg.(LogMsg); !ok || got != LogMsg("late log") {
		t.Fatalf("cmd() = %#v, want LogMsg(%q)", msg, "late log")
	}
}

func TestStartupSequenceDeliversCompletionWhenQueueIsFull(t *testing.T) {
	model := NewModelWithSpecs("test", nil, nil,
		profile.ScrollbackConfig{}, DefaultTheme())
	model.msgChan = make(chan tea.Msg, 1)
	model.msgChan <- LogMsg("buffer full")

	cmd := model.startupSequence()
	if cmd != nil {
		t.Fatalf("startupSequence() = %v, want nil", cmd)
	}

	time.Sleep(msgSendTimeout + 20*time.Millisecond)

	msg := <-model.msgChan
	if got, ok := msg.(LogMsg); !ok || got != LogMsg("buffer full") {
		t.Fatalf("first msg = %#v, want preserved buffered log", msg)
	}

	select {
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for follow-up startup messages")
	default:
	}

	deadline := time.After(time.Second)
	for {
		select {
		case msg = <-model.msgChan:
			if _, ok := msg.(StartupCompleteMsg); ok {
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for StartupCompleteMsg after draining full queue")
		}
	}
}

func TestStartupSequenceCompletesThroughWaitLoopWhenQueueStartsFull(t *testing.T) {
	model := NewModelWithSpecs("test", nil, nil,
		profile.ScrollbackConfig{}, DefaultTheme())
	model.msgChan = make(chan tea.Msg, 1)
	model.msgChan <- LogMsg("buffer full")

	cmd := model.startupSequence()
	if cmd != nil {
		t.Fatalf("startupSequence() = %v, want nil", cmd)
	}

	deadline := time.After(2 * time.Second)
	for !model.startupCompleted {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for startup completion through wait loop")
		default:
		}

		msg := model.waitForMsg()
		next, _ := model.Update(msg)
		model = next.(Model)
	}
}

func TestStartupSequenceCompletesInBubbleTeaProgramWhenQueueStartsFull(t *testing.T) {
	model := NewModelWithSpecs("test", nil, nil,
		profile.ScrollbackConfig{}, DefaultTheme())
	model.msgChan = make(chan tea.Msg, 1)
	model.msgChan <- LogMsg("buffer full")

	final, err := runProgramForTest(t, quitOnStartupCompleteModel{inner: model}, 2*time.Second, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := final.(quitOnStartupCompleteModel).inner
	if !got.startupCompleted {
		t.Fatal("expected startupCompleted true after real program loop")
	}
	if got.showBuffer {
		t.Fatal("expected startup buffer hidden after completion")
	}
}

func TestRunStartupItemAsyncEmitsLogAndCompletion(t *testing.T) {
	model := NewModelWithSpecs("test", []profile.StartupSpec{
		{
			WorkingDir: ".",
			Command:    process.CommandSpec{Shell: "printf ready\\n"},
			Mode:       profile.StartupModeAsync,
		},
	}, nil, profile.ScrollbackConfig{}, DefaultTheme())
	model.msgChan = make(chan tea.Msg, 8)

	model.runStartupItem(0, model.startupSpecs[0])

	var sawRunning bool
	var sawLog bool
	var sawDone bool
	deadline := time.After(2 * time.Second)
	for !(sawRunning && sawLog && sawDone) {
		select {
		case msg := <-model.msgChan:
			switch msg := msg.(type) {
			case startupStatusMsg:
				if msg.status == startupStatusRunning {
					sawRunning = true
				}
				if msg.status == startupStatusOK {
					sawDone = true
				}
			case LogMsg:
				if strings.Contains(string(msg), "--- Starting printf ready") {
					sawLog = true
				}
			case startupLogMsg:
				if msg.line == "ready" {
					sawLog = true
				}
			}
		case <-deadline:
			t.Fatalf("timeout waiting for async startup messages: running=%v log=%v done=%v", sawRunning, sawLog, sawDone)
		}
	}
}

func TestGlobalQuitWithHungKillCommandReturnsPromptly(t *testing.T) {
	panel := process.NewWithCommandSpec("one", process.CommandSpec{Shell: "sleep 60"}, process.CommandSpec{Shell: "sleep 10"}, ".")
	if err := panel.Start(); err != nil {
		t.Fatalf("start panel: %v", err)
	}
	t.Cleanup(func() {
		panel.Stop()
	})

	model := NewModel([]*process.Panel{panel})
	model.activePanel = -1

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = next.(Model)
	if !model.exiting {
		t.Fatal("expected exiting state after q")
	}
	if cmd == nil {
		t.Fatal("expected quit cmd")
	}

	start := time.Now()
	msg := cmd()
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Fatalf("quit cmd took too long: %v", elapsed)
	}

	exitMsg, ok := msg.(exitProgressMsg)
	if !ok {
		t.Fatalf("cmd() = %#v, want exitProgressMsg", msg)
	}
	if !strings.Contains(exitMsg.status, "kill command failed") {
		t.Fatalf("status = %q, want kill command failure", exitMsg.status)
	}
}

func TestGlobalQuitWithHungKillCommandExitsInBubbleTeaProgram(t *testing.T) {
	panel := process.NewWithCommandSpec("one", process.CommandSpec{Shell: "sleep 60"}, process.CommandSpec{Shell: "sleep 10"}, ".")
	if err := panel.Start(); err != nil {
		t.Fatalf("start panel: %v", err)
	}
	t.Cleanup(func() {
		panel.Stop()
	})

	model := NewModel([]*process.Panel{panel})
	start := time.Now()
	final, err := runProgramForTest(t, model, 4*time.Second, func(prog *tea.Program) {
		time.Sleep(50 * time.Millisecond)
		prog.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("program quit took too long: %v", elapsed)
	}

	got := final.(Model)
	if !got.exiting {
		t.Fatal("expected final model to have entered exiting state")
	}
	if got.exitCompleted != 1 {
		t.Fatalf("exitCompleted = %d, want 1", got.exitCompleted)
	}
	if len(got.exitStatuses) != 1 || !strings.Contains(got.exitStatuses[0], "kill command failed") {
		t.Fatalf("exitStatuses = %v, want kill command failure", got.exitStatuses)
	}
	if panel.Running() {
		t.Fatal("expected panel stopped after quit flow")
	}
}

func TestFormatStartupStatusLineShowsExitCode(t *testing.T) {
	got := formatStartupStatusLine(startupItem{
		Label:       "echo one",
		Mode:        profile.StartupModeSync,
		Status:      startupStatusError,
		ExitCode:    23,
		HasExitCode: true,
	})
	if !strings.Contains(got, "ERROR (23)") {
		t.Fatalf("formatStartupStatusLine() = %q, want error exit code", got)
	}
}

func TestStoppedPanelNormalMTogglesMaximize(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return false }

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = next.(Model)
	if model.maximizedPanel != 0 {
		t.Fatalf("expected panel 0 maximized, got %d", model.maximizedPanel)
	}
}

func TestUpdateInactiveQQuits(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})

	teaModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatalf("expected quit command when no panel is active")
	}

	if !teaModel.(Model).exiting {
		t.Fatalf("expected model to be in exiting state")
	}
}

func TestDigitWhenUnfocusedFocusesPanel(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "", "."),
		process.New("b", "echo b", "", "."),
	}
	model := NewModel(panels)
	model.activePanel = -1

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	model = next.(Model)
	if model.activePanel != 1 {
		t.Fatalf("digit 2: want panel 1, got %d", model.activePanel)
	}

	model.activePanel = -1
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("digit 1: want panel 0, got %d", model.activePanel)
	}
}

func TestVimGThenQuit(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("a", "echo a", "", "."),
	})
	model.activePanel = -1

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	model = next.(Model)
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("expected quit after g then q")
	}
}

func TestHjklGridFourPanels(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "", "."),
		process.New("b", "echo b", "", "."),
		process.New("c", "echo c", "", "."),
		process.New("d", "echo d", "", "."),
	}
	model := NewModel(panels)
	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	model = next.(Model)
	if model.activePanel != 1 {
		t.Fatalf("l from 0: want 1, got %d", model.activePanel)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = next.(Model)
	if model.activePanel != 3 {
		t.Fatalf("j from 1: want 3, got %d", model.activePanel)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model = next.(Model)
	if model.activePanel != 1 {
		t.Fatalf("k from 3: want 1, got %d", model.activePanel)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("h from 1: want 0, got %d", model.activePanel)
	}
}

func TestHjklWhenMaximizedKeepsSinglePanelMode(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "", "."),
		process.New("b", "echo b", "", "."),
	}
	model := NewModel(panels)
	model.activePanel = 0
	model.maximizedPanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	model = next.(Model)
	if model.activePanel != 1 {
		t.Fatalf("l: want panel 1, got %d", model.activePanel)
	}
	if model.maximizedPanel != 1 {
		t.Fatalf("want maximized panel 1, got %d", model.maximizedPanel)
	}
}

func TestDigitWhenUnfocusedSelectsPanel(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "", "."),
		process.New("b", "echo b", "", "."),
		process.New("c", "echo c", "", "."),
	}
	model := NewModel(panels)
	model.activePanel = -1

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	model = next.(Model)
	if model.activePanel != 2 {
		t.Fatalf("digit 3 from inactive: want panel 2, got %d", model.activePanel)
	}
}

func TestInsertModeForwardsHjklDoesNotSwitchPanel(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "", "."),
		process.New("b", "echo b", "", "."),
	}
	model := NewModel(panels)
	model.activePanel = 0
	model.panelInsertMode = true
	model.panelRunning = func(*process.Panel) bool { return true }

	var writes [][]byte
	model.sendInput = func(_ *process.Panel, data []byte) error {
		writes = append(writes, append([]byte(nil), data...))
		return nil
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("insert+h: want stay on panel 0, got %d", model.activePanel)
	}
	if len(writes) == 0 {
		t.Fatal("expected h forwarded to PTY")
	}
}

func TestNormalModeDigitJumpsWithoutForwarding(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "", "."),
		process.New("b", "echo b", "", "."),
	}
	model := NewModel(panels)
	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	var writes [][]byte
	model.sendInput = func(_ *process.Panel, data []byte) error {
		writes = append(writes, append([]byte(nil), data...))
		return nil
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	model = next.(Model)
	if model.activePanel != 1 {
		t.Fatalf("digit 2: want panel 1, got %d", model.activePanel)
	}
	if len(writes) != 0 {
		t.Fatalf("digit must not forward to panel, got %v", writes)
	}
}

func TestStatusHintRunningNormalShowsNormalShortcuts(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	hint := model.statusHint()
	if !strings.Contains(hint, "NORMAL: I insert") {
		t.Fatalf("expected normal status hint, got: %q", hint)
	}
	if !strings.Contains(hint, "M maximize") {
		t.Fatalf("expected normal hint to mention maximize, got: %q", hint)
	}
	if !strings.Contains(hint, "Esc blur") {
		t.Fatalf("expected normal hint to mention Esc blur, got: %q", hint)
	}
	if !strings.Contains(hint, "hjkl panes") {
		t.Fatalf("expected normal hint to mention hjkl panes, got: %q", hint)
	}
}

func TestStatusHintMaximizedShowsRestoreShortcut(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.maximizedPanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	hint := model.statusHint()
	if !strings.Contains(hint, "M restore") {
		t.Fatalf("expected maximized status hint, got: %q", hint)
	}
	if !strings.Contains(hint, "Esc restore+blur") {
		t.Fatalf("expected maximized hint to mention restore+blur, got: %q", hint)
	}
}

func TestStatusHintRunningInsertShowsInsertShortcuts(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelInsertMode = true
	model.panelRunning = func(*process.Panel) bool { return true }

	hint := model.statusHint()
	if !strings.Contains(hint, "INSERT: Esc normal") {
		t.Fatalf("expected insert status hint, got: %q", hint)
	}
	if strings.Contains(hint, "INSERT: I insert") {
		t.Fatalf("insert hint must not advertise I insert, got: %q", hint)
	}
	if strings.Contains(hint, "INSERT: R reload") {
		t.Fatalf("insert hint must not advertise R reload, got: %q", hint)
	}
}

func TestStatusHintUnfocusedMentionsDigitsAndHjkl(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	hint := model.statusHint()
	if !strings.Contains(hint, "1–9") || !strings.Contains(hint, "hjkl") {
		t.Fatalf("expected unfocused hint for digits and hjkl, got: %q", hint)
	}
}

func TestStatusModeLabel(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})

	if got := model.statusModeLabel(); got != "NONE" {
		t.Fatalf("expected NONE when no active panel, got %q", got)
	}

	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }
	if got := model.statusModeLabel(); got != "NORMAL" {
		t.Fatalf("expected NORMAL when focused in normal mode, got %q", got)
	}

	model.panelInsertMode = true
	if got := model.statusModeLabel(); got != "INSERT" {
		t.Fatalf("expected INSERT when focused in insert mode, got %q", got)
	}
}

func TestViewStatusLineIncludesMode(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	model = next.(Model)
	view := model.View()
	if !strings.Contains(view, "MODE: NONE") {
		t.Fatalf("expected MODE indicator in status line, got view %q", view)
	}

	model.activePanel = 0
	model.panelInsertMode = true
	view = model.View()
	if !strings.Contains(view, "MODE: INSERT") {
		t.Fatalf("expected INSERT mode indicator in status line, got view %q", view)
	}

	model.panelInsertMode = false
	view = model.View()
	if !strings.Contains(view, "active panel: [1] one") {
		t.Fatalf("expected numbered active panel in status line, got view %q", view)
	}
}

func TestZEntersScrollMode(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("a", "b", "c")
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	model = next.(Model)

	if !model.panelScrollMode {
		t.Fatal("expected scroll mode after z")
	}
	if model.scrollSelections[0] != 2 {
		t.Fatalf("expected selection at live bottom, got %d", model.scrollSelections[0])
	}
}

func TestVEntersSelectModeFromNormalAndEscReturnsToNormal(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.displayForView = func(*process.Panel) process.DisplayState {
		return process.DisplayState{Output: "alpha\nbeta"}
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	model = next.(Model)
	if !model.panelSelectMode {
		t.Fatal("expected select mode after v")
	}
	if model.panelScrollMode {
		t.Fatal("expected live select, not scroll mode")
	}
	if got := model.statusModeLabel(); got != "SELECT" {
		t.Fatalf("expected SELECT mode label, got %q", got)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.panelSelectMode {
		t.Fatal("expected select mode cleared after Esc")
	}
	if model.panelScrollMode {
		t.Fatal("expected return to normal after Esc")
	}
}

func TestVEntersSelectModeFromScrollAndEscReturnsToScroll(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("a", "b", "c")
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	model = next.(Model)
	if !model.panelSelectMode {
		t.Fatal("expected select mode after v from scroll")
	}
	if !model.panelScrollMode {
		t.Fatal("expected scroll state retained while selecting history")
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.panelSelectMode {
		t.Fatal("expected select mode cleared after Esc")
	}
	if !model.panelScrollMode {
		t.Fatal("expected return to scroll mode after Esc")
	}
}

func TestSelectModeCopiesSelectedText(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 20
	model.height = 8
	model.panelRunning = func(*process.Panel) bool { return true }
	model.displayForView = func(*process.Panel) process.DisplayState {
		return process.DisplayState{Output: "alpha\nbeta"}
	}

	var copied string
	model.copySelection = func(text string) error {
		copied = text
		return nil
	}

	model.enterSelectMode()
	model.startSelection(0, 1)
	model.finishSelection(1, 2)
	model.copyCurrentSelection()

	if copied != "lpha\nbet" {
		t.Fatalf("expected copied text %q, got %q", "lpha\nbet", copied)
	}
}

func TestSelectModeMouseDragUpdatesSelection(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 20
	model.height = 8
	model.panelRunning = func(*process.Panel) bool { return true }
	model.displayForView = func(*process.Panel) process.DisplayState {
		return process.DisplayState{Output: "alpha\nbeta"}
	}
	model.enterSelectMode()

	next, _ := model.Update(tea.MouseMsg{
		X: 2, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	model = next.(Model)
	next, _ = model.Update(tea.MouseMsg{
		X: 4, Y: 3, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft,
	})
	model = next.(Model)
	next, _ = model.Update(tea.MouseMsg{
		X: 4, Y: 3, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft,
	})
	model = next.(Model)

	sel := model.selections[0]
	if !sel.Active {
		t.Fatal("expected active selection after drag")
	}
	if sel.Dragging {
		t.Fatal("expected selection drag ended on release")
	}
	if sel.StartRow != 0 || sel.StartCol != 1 || sel.EndRow != 1 || sel.EndCol != 3 {
		t.Fatalf("unexpected selection %#v", sel)
	}
}

func TestScrollModeConsumesPgUpPgDownAndMouseWheel(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelScrollMode = true
	model.width = 80
	model.height = 12
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("0", "1", "2", "3", "4", "5", "6", "7", "8", "9")
	}
	model.panelRunning = func(*process.Panel) bool { return true }
	model.ensureScrollState()
	model.scrollSelections[0] = 9

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = next.(Model)
	if model.scrollOffsets[0] == 0 {
		t.Fatal("expected pgup to move viewport away from bottom")
	}

	offsetAfterPgUp := model.scrollOffsets[0]
	next, _ = model.Update(tea.MouseMsg{X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	model = next.(Model)
	if model.scrollOffsets[0] >= offsetAfterPgUp {
		t.Fatalf("expected wheel down to move viewport toward live bottom, got %d -> %d", offsetAfterPgUp, model.scrollOffsets[0])
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	model = next.(Model)
	if model.scrollOffsets[0] != 0 {
		t.Fatalf("expected pgdown to return to live bottom, got offset %d", model.scrollOffsets[0])
	}
}

func TestScrollModeSelectionAndMarkPersistAcrossAppend(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelScrollMode = true
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }

	history := []string{"0", "1", "2", "3", "4", "5", "6", "7"}
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf(history...)
	}
	model.ensureScrollState()
	model.scrollSelections[0] = 6

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = next.(Model)
	if model.scrollMarks[0] != 7 {
		t.Fatalf("expected mark at selected line, got %d", model.scrollMarks[0])
	}

	history = append(history, "8", "9")
	vp := model.viewportForPanel(0, 11)
	if vp.MarkedRow != 3 {
		t.Fatalf("expected marked line to scroll upward with new content, got row %d", vp.MarkedRow)
	}
	if vp.Lines[vp.MarkedRow] != "6" {
		t.Fatalf("expected original marked content to remain highlighted, got %q", vp.Lines[vp.MarkedRow])
	}
}

func TestScrollModeMarkedLineScrollsOffButCanBeFoundAgain(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelScrollMode = true
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }

	history := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf(history...)
	}
	model.ensureScrollState()
	model.scrollSelections[0] = 8

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = next.(Model)
	if model.scrollMarks[0] != 9 {
		t.Fatalf("expected mark stored at line 8, got %d", model.scrollMarks[0])
	}

	history = append(history, "10", "11", "12", "13", "14", "15")
	vp := model.viewportForPanel(0, 11)
	if vp.MarkedRow != -1 {
		t.Fatalf("expected marked line to scroll offscreen at live bottom, got row %d", vp.MarkedRow)
	}
	if model.scrollMarks[0] != 9 {
		t.Fatalf("expected mark to remain attached to the same history entry, got %d", model.scrollMarks[0])
	}

	model.scrollViewportBy(-6)
	vp = model.viewportForPanel(0, 11)
	if vp.MarkedRow == -1 {
		t.Fatal("expected marked line to be visible again after scrolling up")
	}
	if vp.Lines[vp.MarkedRow] != "8" {
		t.Fatalf("expected marked content to remain line 8 after scrolling up, got %q", vp.Lines[vp.MarkedRow])
	}
}

func TestScrollModeRetainsMarkWhenLineTemporarilyMissing(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelScrollMode = true
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }

	history := []process.HistoryLine{
		{ID: 1, Text: "0"},
		{ID: 2, Text: "1"},
		{ID: 3, Text: "2"},
		{ID: 4, Text: "3"},
	}
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return append([]process.HistoryLine(nil), history...)
	}
	model.ensureScrollState()
	model.scrollSelections[0] = 1

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = next.(Model)
	if model.scrollMarks[0] != 2 {
		t.Fatalf("expected initial mark, got %d", model.scrollMarks[0])
	}

	history = []process.HistoryLine{
		{ID: 3, Text: "2"},
		{ID: 4, Text: "3"},
		{ID: 5, Text: "4"},
		{ID: 6, Text: "5"},
	}
	model.scrollViewportBy(0)
	if model.scrollMarks[0] != 2 {
		t.Fatalf("expected mark retained while line is temporarily missing, got %d", model.scrollMarks[0])
	}

	history = []process.HistoryLine{
		{ID: 2, Text: "1"},
		{ID: 3, Text: "2"},
		{ID: 4, Text: "3"},
		{ID: 5, Text: "4"},
	}
	vp := model.viewportForPanel(0, 11)
	if vp.MarkedRow == -1 {
		t.Fatal("expected marked line to reappear when its history entry returns")
	}
	if vp.Lines[vp.MarkedRow] != "1" {
		t.Fatalf("expected restored mark on original line, got %q", vp.Lines[vp.MarkedRow])
	}
}

func TestScrollModeMarkStaysOnOriginalDuplicateLine(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelScrollMode = true
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }

	history := []string{"1", "2", "--- pause ---", "3", "4"}
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf(history...)
	}
	model.ensureScrollState()
	model.scrollSelections[0] = 2

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = next.(Model)
	if model.scrollMarks[0] != 3 {
		t.Fatalf("expected mark stored for the first duplicate line, got %d", model.scrollMarks[0])
	}

	history = append(history, "--- pause ---", "5", "6")
	vp := model.viewportForPanel(0, 11)
	if vp.MarkedRow == -1 {
		t.Fatal("expected marked duplicate line to remain visible")
	}
	if vp.Lines[vp.MarkedRow] != "--- pause ---" {
		t.Fatalf("expected marked line text to stay on pause line, got %q", vp.Lines[vp.MarkedRow])
	}
	if vp.MarkedRow != 1 {
		t.Fatalf("expected original duplicate line to stay marked, got row %d", vp.MarkedRow)
	}
}

func TestStatusModeLabelIncludesScroll(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelScrollMode = true

	if got := model.statusModeLabel(); got != "SCROLL" {
		t.Fatalf("expected SCROLL when focused in scroll mode, got %q", got)
	}
}

func TestXShortcutKillsPanel(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelRunning = func(p *process.Panel) bool { return true }

	// Press 'x'
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = next.(Model)

	if !model.killingPanel {
		t.Fatal("expected killingPanel state to be true")
	}
	if model.killingPanelIdx != 0 {
		t.Fatalf("expected killingPanelIdx to be 0, got %d", model.killingPanelIdx)
	}
	if cmd == nil {
		t.Fatal("expected a command to be returned")
	}

	// Simulate exitProgressMsg
	msg := exitProgressMsg{panelIdx: 0, status: "exiting panel one.... exiting completed..."}
	next, cmd = model.Update(msg)
	model = next.(Model)

	if model.killingPanel {
		t.Fatal("expected killingPanel state to be false")
	}
	if model.activePanel != -1 {
		t.Fatalf("expected activePanel to be -1, got %d", model.activePanel)
	}
	if cmd != nil {
		t.Fatal("expected no more commands")
	}
}
