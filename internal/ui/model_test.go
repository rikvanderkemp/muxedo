// SPDX-License-Identifier: MIT
package ui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
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

func (m quitOnStartupCompleteModel) View() tea.View {
	return m.inner.View()
}

func keyRune(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func keySpecial(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

func keyCtrl(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}
}

func mouseLeftClick(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func mouseWheel(x, y int, button tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{X: x, Y: y, Button: button}
}

func mouseMotion(x, y int, button tea.MouseButton) tea.MouseMotionMsg {
	return tea.MouseMotionMsg{X: x, Y: y, Button: button}
}

func mouseRelease(x, y int, button tea.MouseButton) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: button}
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

	next, _ = model.Update(mouseLeftClick(1, 1))
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

	next, _ = model.Update(mouseLeftClick(1, h-1))
	model = next.(Model)

	if model.activePanel != -1 {
		t.Fatalf("expected no active panel after status bar click, got %d", model.activePanel)
	}

	next, _ = model.Update(mouseLeftClick(1, 1))
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

	next, _ = model.Update(mouseLeftClick(1, 1))
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

	next, _ := model.Update(keySpecial(tea.KeyEsc))
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

	next, _ := model.Update(keySpecial(tea.KeyEsc))
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

	next, _ := model.Update(keyRune('i'))
	model = next.(Model)
	if !model.panelInsertMode || model.activePanel != 0 {
		t.Fatalf("want insert + focused 0, got insert=%v panel=%d", model.panelInsertMode, model.activePanel)
	}

	next, _ = model.Update(keySpecial(tea.KeyEsc))
	model = next.(Model)
	if model.panelInsertMode || model.activePanel != 0 {
		t.Fatalf("want normal + focused after 1st Esc, got insert=%v panel=%d", model.panelInsertMode, model.activePanel)
	}

	next, _ = model.Update(keySpecial(tea.KeyEsc))
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

	next, cmd := model.Update(keyRune('i'))
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("expected no cmd entering insert")
	}
	if !model.panelInsertMode {
		t.Fatal("expected insert mode")
	}

	next, cmd = model.Update(keyRune('q'))
	model = next.(Model)
	if cmd != nil {
		t.Fatalf("expected no quit command while panel is active")
	}

	next, cmd = model.Update(keyCtrl('c'))
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

	next, cmd := model.Update(keyRune('a'))
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

	next, cmd := model.Update(keyRune('r'))
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

	next, cmd := model.Update(keyRune('R'))
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

	next, _ := model.Update(keyRune('r'))
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

	next, _ := model.Update(keyRune('i'))
	model = next.(Model)
	next, _ = model.Update(keyRune('r'))
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

	next, _ := model.Update(keyRune('i'))
	model = next.(Model)
	next, _ = model.Update(keyRune('m'))
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

	next, _ := model.Update(keyRune('q'))
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

	next, _ := model.Update(keyRune('x'))
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
	t.Skip("per-panel kill commands removed from profiles; keep process-layer tests instead")
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

	next, _ = model.Update(mouseLeftClick(1, 1))
	model = next.(Model)

	next, _ = model.Update(keyRune('I'))
	model = next.(Model)
	if !model.panelInsertMode {
		t.Fatal("expected insert after I")
	}

	next, _ = model.Update(mouseLeftClick(1, 1))
	model = next.(Model)
	if model.panelInsertMode {
		t.Fatal("expected normal mode after re-click panel")
	}
}

func TestMWithoutActivePanelDoesNothing(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})

	next, cmd := model.Update(keyRune('m'))
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

	next, _ := model.Update(keyRune('m'))
	model = next.(Model)
	if model.maximizedPanel != 0 {
		t.Fatalf("expected panel 0 maximized, got %d", model.maximizedPanel)
	}

	next, _ = model.Update(keyRune('m'))
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

	next, _ = model.Update(keyRune('m'))
	model = next.(Model)

	maxCols0, maxRows0 := panels[0].TerminalSize()
	maxCols1, maxRows1 := panels[1].TerminalSize()
	if maxCols0 != 118 || maxRows0 != 36 {
		t.Fatalf("expected maximized panel resized to 118x36, got %dx%d", maxCols0, maxRows0)
	}
	if maxCols1 != gridCols1 || maxRows1 != gridRows1 {
		t.Fatalf("expected hidden panel to keep grid size, got %dx%d", maxCols1, maxRows1)
	}

	next, _ = model.Update(keyRune('m'))
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
	}, nil, nil, profile.ScrollbackConfig{}, DefaultTheme())

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
	}, nil, nil, profile.ScrollbackConfig{}, DefaultTheme())
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
	if !strings.Contains(view, "echo one [async]") {
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
	}, nil, nil, profile.ScrollbackConfig{}, DefaultTheme())

	next, _ := model.Update(startupStatusMsg{idx: 0, status: startupStatusRunning})
	model = next.(Model)
	frame := model.startupSpinner.View()

	next, _ = model.Update(spinner.TickMsg{})
	model = next.(Model)
	if model.startupSpinner.View() == frame {
		t.Fatal("expected startup spinner to advance on tick")
	}
}

func TestStartupCompleteKeepsListeningForLogs(t *testing.T) {
	model := NewModelWithSpecs("test", nil, nil, nil,
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
	model := NewModelWithSpecs("test", nil, nil, nil,
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
	model := NewModelWithSpecs("test", nil, nil, nil,
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
	model := NewModelWithSpecs("test", nil, nil, nil,
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
	}, nil, nil, profile.ScrollbackConfig{}, DefaultTheme())
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

func TestGlobalQuitSecondCtrlCForcesQuit(t *testing.T) {
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Skipf("skipping PTY-dependent test: %v", err)
	}

	// Panel uses kill command that would normally take long; we should still be
	// able to force quit promptly with a second Ctrl-C.
	panel := process.NewWithCommandSpec("one", process.CommandSpec{Shell: "sleep 60"}, process.CommandSpec{Shell: "sleep 10"}, ".")
	if err := panel.Start(); err != nil {
		t.Fatalf("start panel: %v", err)
	}
	t.Cleanup(func() { panel.Stop() })

	model := NewModel([]*process.Panel{panel})
	model.activePanel = -1

	next, cmd := model.Update(keyRune('q'))
	model = next.(Model)
	if !model.exiting {
		t.Fatal("expected exiting state after q")
	}
	if cmd == nil {
		t.Fatal("expected quit cmd batch")
	}

	// Force quit via second Ctrl-C keypress while exiting.
	start := time.Now()
	next, quitCmd := model.Update(keyCtrl('c'))
	_ = next.(Model)
	if quitCmd == nil {
		t.Fatal("expected force quit cmd")
	}
	_ = quitCmd()
	if time.Since(start) > 2*time.Second {
		t.Fatalf("force quit took too long: %v", time.Since(start))
	}
}

func TestGlobalQuitHidesPanelsWithoutKillCommandFromExitBox(t *testing.T) {
	t.Skip("exit dialog now lists all panels; kill commands handled by global teardown")
}

func TestFormatStartupStatusLineShowsExitCode(t *testing.T) {
	got := formatStartupStatusLine(DefaultTheme(), startupItem{
		Label:       "echo one",
		Mode:        profile.StartupModeSync,
		Status:      startupStatusError,
		ExitCode:    23,
		HasExitCode: true,
	}, "…")
	if !strings.Contains(got, "23") {
		t.Fatalf("formatStartupStatusLine() = %q, want error exit code", got)
	}
}

func TestStoppedPanelNormalMTogglesMaximize(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return false }

	next, _ := model.Update(keyRune('m'))
	model = next.(Model)
	if model.maximizedPanel != 0 {
		t.Fatalf("expected panel 0 maximized, got %d", model.maximizedPanel)
	}
}

func TestUpdateInactiveQQuits(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})

	teaModel, cmd := model.Update(keyRune('q'))
	if cmd == nil {
		t.Fatalf("expected quit command when no panel is active")
	}

	if !teaModel.(Model).exiting {
		t.Fatalf("expected model to be in exiting state")
	}
}

func TestExitSpinnerTicksWhileExiting(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "true", "."),
	})
	model.activePanel = -1

	next, _ := model.Update(keyRune('q'))
	model = next.(Model)
	if !model.exiting {
		t.Fatal("expected exiting state")
	}

	if len(model.exitItems) != 1 || model.exitItems[0].Name != "one" {
		t.Fatalf("expected panel with kill command to be shown, got %#v", model.exitItems)
	}

	before := model.exitSpinner.View()
	next, cmd := model.Update(spinner.TickMsg{})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("expected spinner tick cmd while exiting")
	}
	after := model.exitSpinner.View()
	if before == after {
		t.Fatalf("expected spinner to advance, before=%q after=%q", before, after)
	}
}

func TestExitDialogFitsViewportWithTeardown(t *testing.T) {
	model := NewModelWithSpecs("test", nil, []profile.StartupSpec{
		{
			WorkingDir: ".",
			Command: process.CommandSpec{
				Shell: "printf 'this is intentionally long teardown output'; sleep 5",
			},
			Mode: profile.StartupModeAsync,
		},
		{
			WorkingDir: ".",
			Command: process.CommandSpec{
				Shell: "sleep 3",
			},
			Mode: profile.StartupModeSync,
		},
	}, []profile.PanelSpec{
		{Name: "clock", WorkingDir: ".", Command: process.CommandSpec{Shell: "date"}},
		{Name: "echo", WorkingDir: ".", Command: process.CommandSpec{Shell: "cat"}},
	}, profile.ScrollbackConfig{}, DefaultTheme())
	model.width = 80
	model.height = 24
	model.exiting = true
	model.exitItems = []exitItem{
		{Name: "clock", Status: exitStatusOK},
		{Name: "echo", Status: exitStatusRunning},
	}
	model.teardownItems[0].Status = startupStatusRunning
	model.teardownItems[1].Status = startupStatusRunning

	got := model.wrapExiting("")
	for i, line := range strings.Split(got, "\n") {
		if width := ansi.StringWidth(line); width > model.width {
			t.Fatalf("line %d exceeds viewport width: got %d want <= %d: %q", i+1, width, model.width, ansi.Strip(line))
		}
	}
}

func TestAsyncTeardownDoesNotQuitEarly(t *testing.T) {
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Skipf("skipping PTY-dependent test: %v", err)
	}

	panel := process.NewWithCommandSpec("one", process.CommandSpec{Shell: "sleep 60"}, process.CommandSpec{}, ".")
	if err := panel.Start(); err != nil {
		t.Fatalf("start panel: %v", err)
	}
	t.Cleanup(func() { panel.Stop() })

	model := NewModelWithSpecs("test", nil, []profile.StartupSpec{
		{WorkingDir: ".", Command: process.CommandSpec{Shell: "sleep 1"}, Mode: profile.StartupModeAsync},
	}, []profile.PanelSpec{{Name: "one", WorkingDir: ".", Command: process.CommandSpec{Shell: "sleep 60"}}}, profile.ScrollbackConfig{}, DefaultTheme())
	model.panels = []*process.Panel{panel}
	model.activePanel = -1

	// Enter exiting state.
	next, _ := model.Update(keyRune('q'))
	model = next.(Model)

	// Simulate panel exit completion.
	next, _ = model.Update(exitProgressMsg{panelIdx: 0, errText: ""})
	model = next.(Model)

	// Teardown should now be running, but program must not auto-quit yet.
	if model.teardownCompleted {
		t.Fatal("expected teardown not completed immediately for async teardown")
	}
}

func TestDigitWhenUnfocusedFocusesPanel(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "", "."),
		process.New("b", "echo b", "", "."),
	}
	model := NewModel(panels)
	model.activePanel = -1

	next, _ := model.Update(keyRune('2'))
	model = next.(Model)
	if model.activePanel != 1 {
		t.Fatalf("digit 2: want panel 1, got %d", model.activePanel)
	}

	model.activePanel = -1
	next, _ = model.Update(keyRune('1'))
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

	next, _ := model.Update(keyRune('g'))
	model = next.(Model)
	next, cmd := model.Update(keyRune('q'))
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

	next, _ := model.Update(keyRune('l'))
	model = next.(Model)
	if model.activePanel != 1 {
		t.Fatalf("l from 0: want 1, got %d", model.activePanel)
	}
	next, _ = model.Update(keyRune('j'))
	model = next.(Model)
	if model.activePanel != 3 {
		t.Fatalf("j from 1: want 3, got %d", model.activePanel)
	}
	next, _ = model.Update(keyRune('k'))
	model = next.(Model)
	if model.activePanel != 1 {
		t.Fatalf("k from 3: want 1, got %d", model.activePanel)
	}
	next, _ = model.Update(keyRune('h'))
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

	next, _ := model.Update(keyRune('l'))
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

	next, _ := model.Update(keyRune('3'))
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

	next, _ := model.Update(keyRune('h'))
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

	next, _ := model.Update(keyRune('2'))
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
	if !strings.Contains(hint, "?: help") {
		t.Fatalf("expected help hint in status hint, got: %q", hint)
	}
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
	if !strings.Contains(hint, "S scrollback") || !strings.Contains(hint, "drag select") {
		t.Fatalf("expected normal hint to mention scrollback and selection, got: %q", hint)
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

func TestStatusHintUnfocusedMentionsFocusAndQuit(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	hint := model.statusHint()
	if !strings.Contains(hint, "?: help") {
		t.Fatalf("expected help hint in unfocused hint, got: %q", hint)
	}
	if !strings.Contains(hint, "click/1–9") || !strings.Contains(hint, "Ctrl-C quit") {
		t.Fatalf("expected unfocused hint for focus and quit, got: %q", hint)
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
	if !strings.Contains(view.Content, "MODE: NONE") {
		t.Fatalf("expected MODE indicator in status line, got view %q", view.Content)
	}

	model.activePanel = 0
	model.panelInsertMode = true
	view = model.View()
	if !strings.Contains(view.Content, "MODE: INSERT") {
		t.Fatalf("expected INSERT mode indicator in status line, got view %q", view.Content)
	}

	model.panelInsertMode = false
	view = model.View()
	if !strings.Contains(view.Content, "active panel: [1] one") {
		t.Fatalf("expected numbered active panel in status line, got view %q", view.Content)
	}
}

func TestHelpDialogToggleInNormalMode(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	model = next.(Model)

	next, _ = model.Update(keyRune('1'))
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("precondition: expected panel focused")
	}

	next, _ = model.Update(keyRune('?'))
	model = next.(Model)
	if !model.helpActive {
		t.Fatalf("expected helpActive true after '?'")
	}
	view := model.View()
	if !strings.Contains(view.Content, "Help") {
		t.Fatalf("expected help dialog in view, got %q", view.Content)
	}

	// While help open: other keys swallowed.
	next, _ = model.Update(keyRune('m'))
	model = next.(Model)
	if model.maximizedPanel != -1 {
		t.Fatalf("expected maximize not to trigger while help open")
	}

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)
	if model.helpActive {
		t.Fatalf("expected helpActive false after Esc")
	}
}

func TestHelpDialogDoesNotOpenInInsertMode(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	model = next.(Model)
	next, _ = model.Update(keyRune('1'))
	model = next.(Model)

	// Enter insert mode.
	next, _ = model.Update(keyRune('i'))
	model = next.(Model)
	if !model.panelInsertMode {
		t.Fatalf("precondition: expected insert mode")
	}

	next, _ = model.Update(keyRune('?'))
	model = next.(Model)
	if model.helpActive {
		t.Fatalf("expected help not to open in insert mode")
	}
}

func TestPanelScrollInputsDoNothingOutsideScrollback(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	var historyCalls int
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		historyCalls++
		return historyLinesOf("0", "1", "2", "3")
	}

	for _, msg := range []tea.Msg{
		keySpecial(tea.KeyPgUp),
		keySpecial(tea.KeyPgDown),
		mouseWheel(1, 1, tea.MouseWheelUp),
		mouseWheel(1, 1, tea.MouseWheelDown),
	} {
		next, _ := model.Update(msg)
		model = next.(Model)
	}

	if model.scrollbackActive {
		t.Fatal("expected scrollback to stay closed in normal mode")
	}
	if historyCalls != 0 {
		t.Fatalf("expected normal scroll inputs not to read history, got %d calls", historyCalls)
	}

	model.panelInsertMode = true
	var writes int
	model.sendInput = func(*process.Panel, []byte) error {
		writes++
		return nil
	}
	for _, msg := range []tea.Msg{
		keySpecial(tea.KeyPgUp),
		keySpecial(tea.KeyPgDown),
		mouseWheel(1, 1, tea.MouseWheelUp),
		mouseWheel(1, 1, tea.MouseWheelDown),
	} {
		next, _ := model.Update(msg)
		model = next.(Model)
	}

	if model.scrollbackActive {
		t.Fatal("expected scrollback to stay closed in insert mode")
	}
	if historyCalls != 0 {
		t.Fatalf("expected insert scroll inputs not to read history, got %d calls", historyCalls)
	}
	if writes != 0 {
		t.Fatalf("expected scroll inputs not to be forwarded in insert mode, got %d writes", writes)
	}
}

func TestSFromFocusedNormalOpensScrollbackAtBottom(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("0", "1", "2", "3", "4", "5", "6", "7", "8", "9")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)

	if !model.scrollbackActive {
		t.Fatal("expected scrollback to open")
	}
	if model.scrollbackPanel != 0 {
		t.Fatalf("expected scrollback panel 0, got %d", model.scrollbackPanel)
	}
	if got, want := len(model.scrollbackLines), 10; got != want {
		t.Fatalf("expected cached history length %d, got %d", want, got)
	}
	if !model.scrollbackView.AtBottom() {
		t.Fatalf("expected scrollback at bottom, offset=%d", model.scrollbackView.YOffset())
	}
	view := model.View()
	if !strings.Contains(view.Content, "[1] one · SCROLLBACK") {
		t.Fatalf("expected scrollback title in view, got %q", view.Content)
	}
	if !strings.Contains(view.Content, "MODE: SCROLLBACK") {
		t.Fatalf("expected scrollback mode in status line, got %q", view.Content)
	}
}

func TestScrollbackLineNumbersDefaultOn(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("alpha", "beta")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	if !model.scrollbackActive {
		t.Fatal("expected scrollback to open")
	}

	content := model.scrollbackView.GetContent()
	if !strings.Contains(content, "1 │") || !strings.Contains(content, "alpha") || !strings.Contains(content, "2 │") || !strings.Contains(content, "beta") {
		t.Fatalf("expected numbered scrollback content, got %q", content)
	}
}

func TestScrollbackToggleLineNumbersWithLDoesNotAffectCopyText(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("alpha", "beta")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	if !model.scrollbackActive {
		t.Fatal("expected scrollback to open")
	}

	model.ensureScrollState()
	model.selections[0] = panelSelection{
		Active:   true,
		Source:   selectSourceHistory,
		StartRow: 0,
		StartCol: 0,
		EndRow:   1,
		EndCol:   3,
	}
	if got := model.currentSelectionText(); got != "alpha\nbeta" {
		t.Fatalf("selection text = %q, want %q", got, "alpha\nbeta")
	}

	next, _ = model.Update(keyRune('l'))
	model = next.(Model)

	content := model.scrollbackView.GetContent()
	if strings.Contains(content, "│") {
		t.Fatalf("expected line numbers hidden after l toggle, got %q", content)
	}
	if got := model.currentSelectionText(); got != "alpha\nbeta" {
		t.Fatalf("selection text after toggle = %q, want %q", got, "alpha\nbeta")
	}
}

func TestScrollbackRRefreshesContent(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }

	var history []process.HistoryLine
	history = historyLinesOf("alpha")
	model.historyLines = func(*process.Panel) []process.HistoryLine { return history }

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	before := model.scrollbackView.GetContent()
	if !strings.Contains(before, "alpha") {
		t.Fatalf("expected initial content to contain alpha, got %q", before)
	}

	history = historyLinesOf("alpha", "beta")
	next, _ = model.Update(keyRune('r'))
	model = next.(Model)
	after := model.scrollbackView.GetContent()
	if !strings.Contains(after, "beta") {
		t.Fatalf("expected refreshed content to contain beta, got %q", after)
	}
}

func TestScrollbackRefreshInsertsPersistentMarkersAndNewContentAfter(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }

	history := historyLinesOf("alpha", "beta")
	model.historyLines = func(*process.Panel) []process.HistoryLine { return history }

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)

	next, _ = model.Update(keyRune('r'))
	model = next.(Model)
	content1 := model.scrollbackView.GetContent()
	if !strings.Contains(content1, "refreshed at") {
		t.Fatalf("expected refresh marker inserted, got %q", content1)
	}
	if !strings.Contains(content1, "beta") {
		t.Fatalf("expected content to include beta, got %q", content1)
	}

	history = historyLinesOf("alpha", "beta", "gamma")
	next, _ = model.Update(keyRune('r'))
	model = next.(Model)

	content2 := model.scrollbackView.GetContent()
	iBeta := strings.Index(content2, "beta")
	iGamma := strings.Index(content2, "gamma")
	iMark := strings.Index(content2, "refreshed at")
	if iBeta < 0 || iGamma < 0 || iMark < 0 {
		t.Fatalf("expected beta, marker, gamma present; got %q", content2)
	}
	if !(iBeta < iMark && iMark < iGamma) {
		t.Fatalf("expected order beta < marker < gamma, got indexes beta=%d marker=%d gamma=%d content=%q", iBeta, iMark, iGamma, content2)
	}
}

func TestScrollbackMarkerRowIgnoresClick(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("alpha", "beta")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	// Put cursor somewhere stable.
	model.setScrollbackCursorLine(1)
	beforeCursor := model.scrollbackCursorLine

	next, _ = model.Update(keyRune('r'))
	model = next.(Model)

	// Find marker display row.
	markerRow := -1
	for i, h := range model.scrollbackDisplayToHistory {
		if h < 0 {
			markerRow = i
			break
		}
	}
	if markerRow < 0 {
		t.Fatalf("expected marker row in display mapping, got %v", model.scrollbackDisplayToHistory)
	}

	// Ensure marker row visible at top.
	next, _ = model.Update(keyRune('g'))
	model = next.(Model)

	pw := model.scrollbackLineNumberPrefixWidth
	next, _ = model.Update(mouseLeftClick(pw+1, 2+markerRow))
	model = next.(Model)
	if model.scrollbackCursorLine != beforeCursor {
		t.Fatalf("expected marker click ignored, cursor=%d want %d", model.scrollbackCursorLine, beforeCursor)
	}
}

func TestScrollbackRefreshMarkerAfterEmptySnapshotRendersBeforeFirstLine(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }

	history := []process.HistoryLine(nil)
	model.historyLines = func(*process.Panel) []process.HistoryLine { return history }

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)

	// First refresh while empty => marker AfterID==0.
	next, _ = model.Update(keyRune('r'))
	model = next.(Model)

	// Now history becomes non-empty on next refresh.
	history = historyLinesOf("alpha")
	next, _ = model.Update(keyRune('r'))
	model = next.(Model)

	// Find first marker and ensure it is before first history line.
	markerRow := -1
	firstHistoryRow := -1
	for i, h := range model.scrollbackDisplayToHistory {
		if h < 0 && markerRow < 0 {
			markerRow = i
		}
		if h == 0 && firstHistoryRow < 0 {
			firstHistoryRow = i
		}
	}
	if markerRow < 0 || firstHistoryRow < 0 {
		t.Fatalf("expected marker+history rows, got displayToHistory=%v", model.scrollbackDisplayToHistory)
	}
	if markerRow > firstHistoryRow {
		t.Fatalf("expected marker before first history line, markerRow=%d firstHistoryRow=%d map=%v", markerRow, firstHistoryRow, model.scrollbackDisplayToHistory)
	}
}

func TestScrollbackShiftYAndShiftMCopyActions(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("ab ab", "xxabyy")
	}

	var copied string
	model.copySelection = func(text string) error {
		copied = text
		return nil
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	next, _ = model.Update(keyRune('/'))
	model = next.(Model)
	for _, r := range []rune("ab") {
		next, _ = model.Update(keyRune(r))
		model = next.(Model)
	}
	next, _ = model.Update(keySpecial(tea.KeyEnter))
	model = next.(Model)

	// Shift+m path: Text=\"m\" with ModShift should behave like \"M\".
	copied = ""
	next, _ = model.Update(tea.KeyPressMsg{Text: "m", Mod: tea.ModShift})
	model = next.(Model)
	if copied != "ab\nab\nab" {
		t.Fatalf("shift+m copied=%q, want %q", copied, "ab\nab\nab")
	}

	// Shift+y path: Text=\"y\" with ModShift should behave like \"Y\".
	copied = ""
	next, _ = model.Update(tea.KeyPressMsg{Text: "y", Mod: tea.ModShift})
	model = next.(Model)
	if !strings.Contains(copied, "1: ab ab") || !strings.Contains(copied, "2: xxabyy") {
		t.Fatalf("shift+y copied=%q, want both lines with numbering", copied)
	}
}

func TestScrollbackSearchSlashEntersModeAndEnterJumpsBestMatch(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("alpha", "Beta", "gamma", "beta again")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	// Put cursor at top so best match should be first beta (case-insensitive).
	model.setScrollbackCursorLine(0)

	next, _ = model.Update(keyRune('/'))
	model = next.(Model)
	if !model.scrollbackSearchActive {
		t.Fatal("expected / to enter scrollback search mode")
	}
	for _, r := range []rune("beta") {
		next, _ = model.Update(keyRune(r))
		model = next.(Model)
	}
	content := model.scrollbackView.GetContent()
	if ansi.Strip(content) == content {
		t.Fatalf("expected search to add highlight ANSI codes, got %q", content)
	}
	next, _ = model.Update(keySpecial(tea.KeyEnter))
	model = next.(Model)
	if model.scrollbackSearchActive {
		t.Fatal("expected Enter to exit search mode")
	}
	if model.scrollbackCursorLine != 1 {
		t.Fatalf("expected best match to jump to line 1, got %d", model.scrollbackCursorLine)
	}
}

func TestScrollbackSearchInvalidRegexDoesNotClearLastValidMatches(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("alpha", "beta", "gamma", "beta2")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)

	next, _ = model.Update(keyRune('/'))
	model = next.(Model)
	for _, r := range []rune("beta") {
		next, _ = model.Update(keyRune(r))
		model = next.(Model)
	}
	if model.scrollbackSearchErr != "" {
		t.Fatalf("expected valid regex, got err %q", model.scrollbackSearchErr)
	}
	if got := len(model.scrollbackSearchMatches); got != 2 {
		t.Fatalf("expected 2 matches for beta, got %d", got)
	}

	// Make regex invalid: add unclosed bracket.
	next, _ = model.Update(keyRune('['))
	model = next.(Model)
	if model.scrollbackSearchErr == "" {
		t.Fatal("expected invalid regex error after adding '['")
	}
	if got := len(model.scrollbackSearchMatches); got != 2 {
		t.Fatalf("expected invalid regex to preserve previous matches, got len=%d", got)
	}
}

func TestScrollbackSearchNAndNWrap(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("beta", "x", "beta", "y")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	model.setScrollbackCursorLine(0)

	next, _ = model.Update(keyRune('/'))
	model = next.(Model)
	for _, r := range []rune("beta") {
		next, _ = model.Update(keyRune(r))
		model = next.(Model)
	}
	next, _ = model.Update(keySpecial(tea.KeyEnter))
	model = next.(Model)
	if model.scrollbackCursorLine != 0 {
		t.Fatalf("expected initial best match at line 0, got %d", model.scrollbackCursorLine)
	}

	next, _ = model.Update(keyRune('n'))
	model = next.(Model)
	if model.scrollbackCursorLine != 2 {
		t.Fatalf("expected n to jump to next match line 2, got %d", model.scrollbackCursorLine)
	}

	next, _ = model.Update(keyRune('n'))
	model = next.(Model)
	if model.scrollbackCursorLine != 0 {
		t.Fatalf("expected n to wrap to first match line 0, got %d", model.scrollbackCursorLine)
	}

	next, _ = model.Update(keyRune('N'))
	model = next.(Model)
	if model.scrollbackCursorLine != 2 {
		t.Fatalf("expected N to wrap to last match line 2, got %d", model.scrollbackCursorLine)
	}
}

func TestScrollbackYCopiesAllSearchMatchesWithLineNumbers(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("alpha", "Beta", "", "beta again")
	}

	var copied string
	model.copySelection = func(text string) error {
		copied = text
		return nil
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	next, _ = model.Update(keyRune('/'))
	model = next.(Model)
	for _, r := range []rune("beta") {
		next, _ = model.Update(keyRune(r))
		model = next.(Model)
	}
	next, _ = model.Update(keySpecial(tea.KeyEnter))
	model = next.(Model)

	next, _ = model.Update(keyRune('Y'))
	model = next.(Model)

	if copied == "" {
		t.Fatal("expected Y to copy matches")
	}
	if !strings.Contains(copied, "2: Beta") || !strings.Contains(copied, "4: beta again") {
		t.Fatalf("unexpected copied content: %q", copied)
	}
	if strings.Contains(copied, "3:") {
		t.Fatalf("expected empty line to be skipped, got %q", copied)
	}
}

func TestScrollbackYWithoutSearchMatchesWarns(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("alpha", "beta")
	}

	var copied string
	model.copySelection = func(text string) error {
		copied = text
		return nil
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	next, _ = model.Update(keyRune('Y'))
	model = next.(Model)

	if copied != "" {
		t.Fatalf("expected no copy without matches, got %q", copied)
	}
	if len(model.messageBuffer) == 0 || !strings.Contains(model.messageBuffer[len(model.messageBuffer)-1], "no search matches") {
		t.Fatalf("expected warning message, got %v", model.messageBuffer)
	}
}

func TestScrollbackMCopiesOnlyMatchedSubstringsOnePerLine(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("ab ab", "xxabyy", "nope")
	}

	var copied string
	model.copySelection = func(text string) error {
		copied = text
		return nil
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	next, _ = model.Update(keyRune('/'))
	model = next.(Model)
	for _, r := range []rune("ab") {
		next, _ = model.Update(keyRune(r))
		model = next.(Model)
	}
	next, _ = model.Update(keySpecial(tea.KeyEnter))
	model = next.(Model)

	next, _ = model.Update(keyRune('M'))
	model = next.(Model)

	if copied != "ab\nab\nab" {
		t.Fatalf("copied = %q, want %q", copied, "ab\nab\nab")
	}
}

func TestScrollbackMWithoutSearchMatchesWarns(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("alpha", "beta")
	}

	var copied string
	model.copySelection = func(text string) error {
		copied = text
		return nil
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	next, _ = model.Update(keyRune('M'))
	model = next.(Model)

	if copied != "" {
		t.Fatalf("expected no copy without matches, got %q", copied)
	}
	if len(model.messageBuffer) == 0 || !strings.Contains(model.messageBuffer[len(model.messageBuffer)-1], "no search matches") {
		t.Fatalf("expected warning message, got %v", model.messageBuffer)
	}
}

func TestScrollbackClickThenBBookmarksLine(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("alpha", "beta", "gamma")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)

	pw := model.scrollbackLineNumberPrefixWidth
	// Click first row ("alpha"), then bookmark it.
	next, _ = model.Update(mouseLeftClick(pw+1, 2))
	model = next.(Model)
	next, _ = model.Update(keyRune('m'))
	model = next.(Model)

	content := model.scrollbackView.GetContent()
	if !strings.Contains(content, "● alpha") {
		t.Fatalf("expected bookmark marker on alpha line, got %q", content)
	}
}

func TestScrollbackArrowDownMovesCursorHighlight(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("alpha", "beta", "gamma")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	// Cursor starts at last line.
	if model.scrollbackCursorLine != 2 {
		t.Fatalf("expected cursor at last line, got %d", model.scrollbackCursorLine)
	}

	next, _ = model.Update(keySpecial(tea.KeyUp))
	model = next.(Model)
	if model.scrollbackCursorLine != 1 {
		t.Fatalf("expected cursor to move up, got %d", model.scrollbackCursorLine)
	}
}

func TestScrollbackDoubleClickTogglesMark(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("alpha", "beta", "gamma")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)

	pw := model.scrollbackLineNumberPrefixWidth
	// Prime last click to simulate a double click on line 0.
	model.scrollbackLastClickLine = 0
	model.scrollbackLastClickAt = time.Now()
	next, _ = model.Update(mouseLeftClick(pw+1, 2))
	model = next.(Model)

	content := model.scrollbackView.GetContent()
	if !strings.Contains(content, "● alpha") {
		t.Fatalf("expected double click to mark alpha, got %q", content)
	}
}

func TestSWithoutActivePanelDoesNothing(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)

	if model.scrollbackActive {
		t.Fatal("expected scrollback to stay closed without an active panel")
	}
}

func TestSInInsertModeDoesNotOpenScrollback(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelInsertMode = true
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)

	if model.scrollbackActive {
		t.Fatal("expected scrollback to stay closed in insert mode")
	}
}

func TestEscFromScrollbackReturnsToFocusedNormal(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("0", "1", "2", "3", "4", "5", "6", "7", "8", "9")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	if !model.scrollbackActive {
		t.Fatal("expected scrollback to open")
	}

	next, _ = model.Update(keySpecial(tea.KeyEsc))
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("expected Esc to keep panel focused, got %d", model.activePanel)
	}
	if model.scrollbackActive {
		t.Fatal("expected Esc to close scrollback")
	}
	if model.panelInsertMode {
		t.Fatal("expected normal mode after closing scrollback")
	}

	next, _ = model.Update(keySpecial(tea.KeyEsc))
	model = next.(Model)
	if model.activePanel != -1 {
		t.Fatalf("expected next Esc to blur panel, got %d", model.activePanel)
	}
}

func TestPanelSelectionCopiesSelectedText(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 20
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.displayForView = func(*process.Panel) process.DisplayState {
		return process.DisplayState{Output: "alpha\nbeta"}
	}

	var copied string
	model.copySelection = func(text string) error {
		copied = text
		return nil
	}

	model.startSelection(0, 1)
	model.finishSelection(1, 2)
	next, _ := model.Update(keyRune('y'))
	model = next.(Model)

	if copied != "lpha\nbet" {
		t.Fatalf("expected copied text %q, got %q", "lpha\nbet", copied)
	}
}

func TestPanelSelectionCopySkipsEmptyLines(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 20
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.displayForView = func(*process.Panel) process.DisplayState {
		return process.DisplayState{Output: "alpha\n\n   \nbeta"}
	}

	model.startSelection(0, 0)
	model.finishSelection(3, 3)

	if got := model.currentSelectionText(); got != "alpha\nbeta" {
		t.Fatalf("expected empty lines skipped, got %q", got)
	}
}

func TestPanelMouseDragUpdatesSelection(t *testing.T) {
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

	next, _ := model.Update(mouseLeftClick(2, 2))
	model = next.(Model)
	next, _ = model.Update(mouseMotion(4, 3, tea.MouseLeft))
	model = next.(Model)
	next, _ = model.Update(mouseRelease(4, 3, tea.MouseLeft))
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

func TestSingleClickClearsAllSelections(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
		process.New("two", "echo two", "", "."),
	})
	model.activePanel = 0
	model.width = 40
	model.height = 8
	model.panelRunning = func(*process.Panel) bool { return true }
	model.ensureScrollState()
	model.selections[0] = panelSelection{Active: true, Dragging: false, Source: selectSourceLive}
	model.selections[1] = panelSelection{Active: true, Dragging: false, Source: selectSourceLive}

	next, _ := model.Update(mouseLeftClick(2, 2))
	model = next.(Model)
	next, _ = model.Update(mouseRelease(2, 2, tea.MouseLeft))
	model = next.(Model)

	for i, sel := range model.selections {
		if sel.Active || sel.Dragging {
			t.Fatalf("selection %d remained after single click: %#v", i, sel)
		}
	}
}

func TestInsertModeDisablesMouseSelection(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 20
	model.height = 8
	model.panelInsertMode = true
	model.panelRunning = func(*process.Panel) bool { return true }
	model.displayForView = func(*process.Panel) process.DisplayState {
		return process.DisplayState{Output: "alpha\nbeta"}
	}

	next, _ := model.Update(mouseLeftClick(2, 2))
	model = next.(Model)
	next, _ = model.Update(mouseMotion(4, 3, tea.MouseLeft))
	model = next.(Model)
	next, _ = model.Update(mouseRelease(4, 3, tea.MouseLeft))
	model = next.(Model)

	if len(model.selections) > 0 && model.selections[0].Active {
		t.Fatalf("expected no active selection in insert mode, got %#v", model.selections[0])
	}
}

func TestEnteringInsertModeClearsActiveSelection(t *testing.T) {
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
	model.startSelection(0, 1)
	model.finishSelection(1, 2)

	next, _ := model.Update(keyRune('i'))
	model = next.(Model)

	if !model.panelInsertMode {
		t.Fatal("expected insert mode")
	}
	if model.selections[0].Active {
		t.Fatalf("expected insert mode to clear active selection, got %#v", model.selections[0])
	}
}

func TestIWhileScrollbackOpenDoesNotEnterInsertMode(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("0", "1", "2", "3")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	next, _ = model.Update(keyRune('i'))
	model = next.(Model)

	if !model.scrollbackActive {
		t.Fatal("expected scrollback to remain open")
	}
	if model.panelInsertMode {
		t.Fatal("expected i not to enter insert mode while scrollback is open")
	}
}

func TestScrollbackViewportConsumesNavigation(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("0", "1", "2", "3", "4", "5", "6", "7", "8", "9")
	}
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	bottom := model.scrollbackView.YOffset()
	if bottom == 0 {
		t.Fatal("test requires content taller than scrollback viewport")
	}

	next, _ = model.Update(keySpecial(tea.KeyPgUp))
	model = next.(Model)
	if model.scrollbackView.YOffset() >= bottom {
		t.Fatalf("expected PgUp to move toward top, got %d from %d", model.scrollbackView.YOffset(), bottom)
	}

	next, _ = model.Update(keySpecial(tea.KeyPgDown))
	model = next.(Model)
	if model.scrollbackView.YOffset() != bottom {
		t.Fatalf("expected PgDn to return to bottom, got offset %d", model.scrollbackView.YOffset())
	}

	next, _ = model.Update(mouseWheel(1, 2, tea.MouseWheelUp))
	model = next.(Model)
	if model.scrollbackView.YOffset() >= bottom {
		t.Fatalf("expected wheel up to move toward top, got %d", model.scrollbackView.YOffset())
	}

	next, _ = model.Update(mouseWheel(1, 2, tea.MouseWheelDown))
	model = next.(Model)
	if model.scrollbackView.YOffset() != bottom {
		t.Fatalf("expected wheel down to return to bottom, got %d", model.scrollbackView.YOffset())
	}

	next, _ = model.Update(keyRune('g'))
	model = next.(Model)
	if model.scrollbackView.YOffset() != 0 {
		t.Fatalf("expected g to jump to top, got %d", model.scrollbackView.YOffset())
	}

	next, _ = model.Update(keyRune('j'))
	model = next.(Model)
	if model.scrollbackView.YOffset() != 1 {
		t.Fatalf("expected j to scroll down one line, got %d", model.scrollbackView.YOffset())
	}

	next, _ = model.Update(keyRune('k'))
	model = next.(Model)
	if model.scrollbackView.YOffset() != 0 {
		t.Fatalf("expected k to scroll up one line, got %d", model.scrollbackView.YOffset())
	}

	next, _ = model.Update(keySpecial(tea.KeyDown))
	model = next.(Model)
	if model.scrollbackCursorLine != len(model.scrollbackLines)-1 {
		t.Fatalf("expected down arrow to keep cursor at bottom line, got %d", model.scrollbackCursorLine)
	}

	next, _ = model.Update(keySpecial(tea.KeyUp))
	model = next.(Model)
	if model.scrollbackCursorLine != len(model.scrollbackLines)-2 {
		t.Fatalf("expected up arrow to move cursor up one line, got %d", model.scrollbackCursorLine)
	}

	next, _ = model.Update(keyRune('G'))
	model = next.(Model)
	if model.scrollbackView.YOffset() != bottom {
		t.Fatalf("expected G to jump to bottom, got %d", model.scrollbackView.YOffset())
	}
}

func TestScrollbackMouseSelectionCopiesAndSkipsEmptyLines(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 20
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("alpha", "", "   ", "beta", "gamma")
	}

	var copied string
	model.copySelection = func(text string) error {
		copied = text
		return nil
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	pw := model.scrollbackLineNumberPrefixWidth
	startX := pw + 1 // text col 0
	endX := pw + 4   // text col 3
	next, _ = model.Update(mouseLeftClick(startX, 2))
	model = next.(Model)
	next, _ = model.Update(mouseMotion(endX, 5, tea.MouseLeft))
	model = next.(Model)
	next, _ = model.Update(mouseRelease(endX, 5, tea.MouseLeft))
	model = next.(Model)

	if model.selections[0].Source != selectSourceHistory {
		t.Fatalf("expected history selection source, got %v", model.selections[0].Source)
	}
	if got := model.currentSelectionText(); got != "alpha\nbeta" {
		t.Fatalf("expected history selection text, got %q", got)
	}

	next, _ = model.Update(keyRune('y'))
	model = next.(Model)
	if copied != "alpha\nbeta" {
		t.Fatalf("expected copied history text, got %q", copied)
	}

	copied = ""
	next, _ = model.Update(keySpecial(tea.KeyEnter))
	model = next.(Model)
	if copied != "alpha\nbeta" {
		t.Fatalf("expected Enter to copy history text, got %q", copied)
	}
}

func TestResizeUpdatesScrollbackViewport(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.panelRunning = func(*process.Panel) bool { return true }
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("0", "1", "2", "3", "4", "5", "6", "7", "8", "9")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)
	next, _ = model.Update(keyRune('g'))
	model = next.(Model)

	next, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 8})
	model = next.(Model)

	if got, want := model.scrollbackView.Width(), 58; got != want {
		t.Fatalf("scrollback width = %d, want %d", got, want)
	}
	if got, want := model.scrollbackView.Height(), 4; got != want {
		t.Fatalf("scrollback height = %d, want %d", got, want)
	}
	if model.scrollbackView.YOffset() < 0 || model.scrollbackView.YOffset() > model.scrollbackView.TotalLineCount()-model.scrollbackView.Height() {
		t.Fatalf("scrollback offset out of range: %d", model.scrollbackView.YOffset())
	}
}

func TestStatusModeLabelShowsScrollback(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.width = 80
	model.height = 12
	model.historyLines = func(*process.Panel) []process.HistoryLine {
		return historyLinesOf("0", "1", "2")
	}

	next, _ := model.Update(keyRune('s'))
	model = next.(Model)

	if got := model.statusModeLabel(); got != "SCROLLBACK" {
		t.Fatalf("expected SCROLLBACK when scrollback is open, got %q", got)
	}
}

func TestXShortcutKillsPanel(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "", "."),
	})
	model.activePanel = 0
	model.panelRunning = func(p *process.Panel) bool { return true }

	// Press 'x'
	next, cmd := model.Update(keyRune('x'))
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
	msg := exitProgressMsg{panelIdx: 0, errText: ""}
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
