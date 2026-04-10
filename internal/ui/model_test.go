package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"muxedo/internal/process"
)

func historyLinesOf(lines ...string) []process.HistoryLine {
	out := make([]process.HistoryLine, len(lines))
	for i, line := range lines {
		out[i] = process.HistoryLine{ID: uint64(i + 1), Text: line}
	}
	return out
}

func TestUpdateClickActivatesPanel(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "."),
	}, "vi")

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
		process.New("one", "echo one", "."),
	}, "vi")

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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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
		t.Fatalf("expected ctrl+c to be captured by active panel")
	}

	if len(writes) != 2 {
		t.Fatalf("expected 2 key writes, got %d", len(writes))
	}
	if string(writes[0]) != "q" {
		t.Fatalf("expected first write to be q, got %q", string(writes[0]))
	}
	if len(writes[1]) != 1 || writes[1][0] != 0x03 {
		t.Fatalf("expected second write to be ctrl+c (0x03), got %v", writes[1])
	}
}

func TestStoppedPanelIgnoresNonReloadKeys(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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

func TestRunningPanelNormalSwallowsUnknownRune(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "."),
	}, "vi")
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

func TestSOpensScrollbackLikeCtrlO(t *testing.T) {
	panel := process.NewWithScrollback("one", "echo one", ".", t.TempDir(), 0)
	model := NewModel([]*process.Panel{panel}, "vi")
	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	var writes [][]byte
	model.sendInput = func(_ *process.Panel, data []byte) error {
		writes = append(writes, data)
		return nil
	}

	var editorCalled bool
	model.openEditor = func(editor, path string) tea.Cmd {
		editorCalled = true
		return nil
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	_ = next.(Model)

	if len(writes) != 0 {
		t.Fatalf("expected S not forwarded in normal mode, got %d writes", len(writes))
	}
	if !editorCalled {
		t.Fatal("expected openEditor on S")
	}
}

func TestClickResetsInsertMode(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "."),
	}, "vi")
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

func TestCtrlOOpensEditorInsteadOfForwarding(t *testing.T) {
	panel := process.NewWithScrollback("one", "echo one", ".", t.TempDir(), 0)
	model := NewModel([]*process.Panel{panel}, "vi")
	model.activePanel = 0
	model.panelRunning = func(p *process.Panel) bool { return true }

	var writes [][]byte
	model.sendInput = func(_ *process.Panel, data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		writes = append(writes, cp)
		return nil
	}

	var editorCalled bool
	model.openEditor = func(editor, path string) tea.Cmd {
		editorCalled = true
		return nil
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	_ = next.(Model)

	if len(writes) != 0 {
		t.Fatalf("expected ctrl+o NOT to be forwarded to panel, got %d writes", len(writes))
	}
	if !editorCalled {
		t.Fatal("expected openEditor to be called on ctrl+o")
	}
}

func TestCtrlONoopWithoutActivePanel(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "."),
	}, "vi")

	var editorCalled bool
	model.openEditor = func(editor, path string) tea.Cmd {
		editorCalled = true
		return nil
	}

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	_ = next.(Model)

	if editorCalled {
		t.Fatal("expected openEditor NOT to be called when no panel is active")
	}
	if cmd != nil {
		t.Fatal("expected nil cmd when no panel is active")
	}
}

func TestMWithoutActivePanelDoesNothing(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "."),
	}, "vi")

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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
		process.New("two", "echo two", "."),
	}
	model := NewModel(panels, "vi")
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

func TestStoppedPanelNormalMTogglesMaximize(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatalf("expected quit command when no panel is active")
	}

	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from quit command")
	}
}

func TestVimGTWhenUnfocused(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "."),
		process.New("b", "echo b", "."),
	}
	model := NewModel(panels, "vi")
	model.activePanel = -1

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	model = next.(Model)
	if !model.afterGForTab {
		t.Fatal("expected pending g for gt")
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("gt: want first panel, got %d", model.activePanel)
	}
	if model.afterGForTab {
		t.Fatal("expected pending g cleared")
	}

	model.activePanel = -1
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("gT from unfocused: want first panel, got %d", model.activePanel)
	}
}

func TestVimGThenQuit(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("a", "echo a", "."),
	}, "vi")
	model.activePanel = -1

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	model = next.(Model)
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("expected quit after g then q")
	}
	if model.afterGForTab {
		t.Fatal("pending g should clear before quit")
	}
}

func TestMacOSDaggerRunesCycleWhenFocused(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "."),
		process.New("b", "echo b", "."),
	}
	model := NewModel(panels, "vi")
	model.activePanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	var writes [][]byte
	model.sendInput = func(_ *process.Panel, data []byte) error {
		writes = append(writes, append([]byte(nil), data...))
		return nil
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\u2020'}})
	model = next.(Model)
	if model.activePanel != 1 {
		t.Fatalf("† (macOS Opt+t): want panel 1, got %d", model.activePanel)
	}
	if len(writes) != 0 {
		t.Fatalf("† must not forward to panel, got %q", writes)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\u2021'}})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("‡ (macOS Opt+T): want panel 0, got %d", model.activePanel)
	}
}

func TestAltTWhenFocusedCycles(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "."),
		process.New("b", "echo b", "."),
	}
	model := NewModel(panels, "vi")
	model.activePanel = 0
	model.panelInsertMode = true
	model.panelRunning = func(*process.Panel) bool { return true }

	var writes [][]byte
	model.sendInput = func(_ *process.Panel, data []byte) error {
		writes = append(writes, append([]byte(nil), data...))
		return nil
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}, Alt: true})
	model = next.(Model)
	if model.panelInsertMode {
		t.Fatal("panel switch should reset insert mode")
	}
	if model.activePanel != 1 {
		t.Fatalf("Alt+t: want panel 1, got %d", model.activePanel)
	}
	if len(writes) != 0 {
		t.Fatalf("Alt+t must not forward to panel, got %v", writes)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}, Alt: true})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("Alt+T: want panel 0, got %d", model.activePanel)
	}
}

func TestAltTWhenMaximizedKeepsSinglePanelMode(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "."),
		process.New("b", "echo b", "."),
	}
	model := NewModel(panels, "vi")
	model.activePanel = 0
	model.maximizedPanel = 0
	model.panelRunning = func(*process.Panel) bool { return true }

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}, Alt: true})
	model = next.(Model)
	if model.activePanel != 1 {
		t.Fatalf("Alt+t: want panel 1, got %d", model.activePanel)
	}
	if model.maximizedPanel != 1 {
		t.Fatalf("Alt+t: want maximized panel 1, got %d", model.maximizedPanel)
	}
}

func TestPanelShortcutInactiveAlwaysSelectsFirst(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "."),
		process.New("b", "echo b", "."),
		process.New("c", "echo c", "."),
	}
	model := NewModel(panels, "vi")
	model.activePanel = -1

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("Ctrl+] from inactive: want panel 0, got %d", model.activePanel)
	}

	model.activePanel = -1
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}, Alt: true})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("Meta+[ from inactive: want panel 0, got %d", model.activePanel)
	}
}

func TestPanelShortcutsCycleRightAndLeft(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "."),
		process.New("b", "echo b", "."),
		process.New("c", "echo c", "."),
	}
	model := NewModel(panels, "vi")
	model.activePanel = 0

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	model = next.(Model)
	if model.activePanel != 1 {
		t.Fatalf("after Ctrl+] from 0: want 1, got %d", model.activePanel)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}, Alt: true})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("after Meta+[ from 1: want 0, got %d", model.activePanel)
	}

	model.activePanel = 1
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket, Alt: true})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("after Alt+Ctrl+] from 1: want 0, got %d", model.activePanel)
	}

	model.activePanel = 0
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}, Alt: true})
	model = next.(Model)
	if model.activePanel != 2 {
		t.Fatalf("after Meta+[ from 0: want wrap to 2, got %d", model.activePanel)
	}

	model.activePanel = 2
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}, Alt: true})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("after Meta+] from 2: want wrap to 0, got %d", model.activePanel)
	}
}

func TestPanelShortcutsAltCtrlArrows(t *testing.T) {
	panels := []*process.Panel{
		process.New("a", "echo a", "."),
		process.New("b", "echo b", "."),
	}
	model := NewModel(panels, "vi")
	model.activePanel = 0

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlRight, Alt: true})
	model = next.(Model)
	if model.activePanel != 1 {
		t.Fatalf("Alt+Ctrl+Right from 0: want 1, got %d", model.activePanel)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlLeft, Alt: true})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("Alt+Ctrl+Left from 1: want 0, got %d", model.activePanel)
	}

	model.activePanel = -1
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlLeft, Alt: true})
	model = next.(Model)
	if model.activePanel != 0 {
		t.Fatalf("from inactive: want first panel, got %d", model.activePanel)
	}
}

func TestRunningPanelDoesNotForwardPanelSwitchKeys(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "."),
	}, "vi")
	model.activePanel = 0

	var writes [][]byte
	model.sendInput = func(_ *process.Panel, data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		writes = append(writes, cp)
		return nil
	}
	model.panelRunning = func(p *process.Panel) bool { return true }

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	_ = next.(Model)

	if len(writes) != 0 {
		t.Fatalf("expected Ctrl+] not forwarded to panel, got %v", writes)
	}
}

func TestStatusHintRunningNormalShowsNormalShortcuts(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "."),
	}, "vi")
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
}

func TestStatusHintMaximizedShowsRestoreShortcut(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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

func TestStatusModeLabel(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "."),
	}, "vi")

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
		process.New("one", "echo one", "."),
	}, "vi")
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
}

func TestZEntersScrollMode(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "."),
	}, "vi")
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

func TestScrollModeConsumesPgUpPgDownAndMouseWheel(t *testing.T) {
	model := NewModel([]*process.Panel{
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
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
		process.New("one", "echo one", "."),
	}, "vi")
	model.activePanel = 0
	model.panelScrollMode = true

	if got := model.statusModeLabel(); got != "SCROLL" {
		t.Fatalf("expected SCROLL when focused in scroll mode, got %q", got)
	}
}
