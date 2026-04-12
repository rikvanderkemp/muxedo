// SPDX-License-Identifier: MIT
package process

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewPanelIsNotRunning(t *testing.T) {
	p := New("test", "echo hi", "", ".")
	if p.Running() {
		t.Fatal("new panel should not be running")
	}
}

func TestPanelRunningAfterStart(t *testing.T) {
	p := New("test", "sleep 60", "", ".")
	if err := p.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer p.Stop()

	if !p.Running() {
		t.Fatal("panel should be running after Start()")
	}
}

func TestPanelStoppedAfterProcessExits(t *testing.T) {
	p := New("test", "true", "", ".")
	if err := p.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for p.Running() {
		select {
		case <-deadline:
			p.Stop()
			t.Fatal("panel should have stopped after process exited")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if got := p.Elapsed(); got < 0 {
		t.Fatalf("elapsed must not be negative, got %v", got)
	}
}

func TestPanelRestart(t *testing.T) {
	p := New("test", "true", "", ".")
	if err := p.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for p.Running() {
		select {
		case <-deadline:
			p.Stop()
			t.Fatal("panel should have stopped after process exited")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if err := p.Restart(); err != nil {
		t.Fatalf("restart failed: %v", err)
	}

	if !p.Running() {
		t.Fatal("panel should be running after Restart()")
	}
	p.Stop()
}

func TestRestartWhileRunningIsIdempotent(t *testing.T) {
	p := New("test", "sleep 60", "", ".")
	if err := p.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	if err := p.Restart(); err != nil {
		t.Fatalf("restart while running failed: %v", err)
	}

	if !p.Running() {
		t.Fatal("panel should still be running after restart")
	}
	p.Stop()
}

func TestPanelElapsedAdvancesWhileRunningAndFreezesAfterStop(t *testing.T) {
	p := New("test", "sleep 60", "", ".")
	if err := p.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	time.Sleep(120 * time.Millisecond)
	runningElapsed := p.Elapsed()
	if runningElapsed < 100*time.Millisecond {
		p.Stop()
		t.Fatalf("expected elapsed to advance while running, got %v", runningElapsed)
	}

	p.Stop()
	stoppedElapsed := p.Elapsed()
	if stoppedElapsed < runningElapsed {
		t.Fatalf("expected stopped elapsed >= running elapsed, got running=%v stopped=%v", runningElapsed, stoppedElapsed)
	}

	time.Sleep(120 * time.Millisecond)
	frozenElapsed := p.Elapsed()
	if frozenElapsed > stoppedElapsed+50*time.Millisecond {
		t.Fatalf("expected elapsed to freeze after stop, got stopped=%v frozen=%v", stoppedElapsed, frozenElapsed)
	}
}

func TestPanelRestartResetsElapsed(t *testing.T) {
	p := New("test", "sleep 60", "", ".")
	if err := p.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	time.Sleep(150 * time.Millisecond)
	beforeRestart := p.Elapsed()
	if beforeRestart < 100*time.Millisecond {
		p.Stop()
		t.Fatalf("expected elapsed before restart to advance, got %v", beforeRestart)
	}

	if err := p.Restart(); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	defer p.Stop()

	time.Sleep(20 * time.Millisecond)
	afterRestart := p.Elapsed()
	if afterRestart >= 100*time.Millisecond {
		t.Fatalf("expected elapsed to reset after restart, got %v", afterRestart)
	}
}

func TestPanelStopRecordsExitError(t *testing.T) {
	p := New("test", "sleep 60", "", ".")
	if err := p.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	p.Stop()

	deadline := time.After(2 * time.Second)
	for {
		if err := p.ExitError(); err != nil {
			return
		}
		select {
		case <-deadline:
			t.Fatal("ExitError() remained nil after Stop()")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestResetScrollbackClearsPersistedHistory(t *testing.T) {
	dir := t.TempDir()
	p := NewWithScrollback("test", "echo hi", "", ".", dir, 0)

	if err := os.WriteFile(p.ScrollbackPath(), []byte("stale\nhistory\n"), 0o644); err != nil {
		t.Fatalf("write stale scrollback: %v", err)
	}

	p.ResetScrollback()

	if _, err := os.Stat(p.ScrollbackPath()); !os.IsNotExist(err) {
		t.Fatalf("expected reset to remove scrollback file, got err=%v", err)
	}
}

func TestHistoryLinesExcludesStaleScrollbackAfterReset(t *testing.T) {
	dir := t.TempDir()
	p := NewWithScrollback("test", "echo hi", "", ".", dir, 0)

	if err := os.WriteFile(p.ScrollbackPath(), []byte("stale\nhistory\n"), 0o644); err != nil {
		t.Fatalf("write stale scrollback: %v", err)
	}

	p.ResetScrollback()

	p.termMu.Lock()
	p.term.Write([]byte("fresh\noutput")) //nolint:errcheck
	p.termMu.Unlock()
	if p.sb != nil {
		p.sb.Capture(p.Output())
	}

	got := p.HistoryLines()
	for _, line := range got {
		if line == "stale" || line == "history" {
			t.Fatalf("expected stale scrollback to be removed after reset, got %v", got)
		}
	}
	if len(got) < 2 {
		t.Fatalf("expected current screen lines after reset, got %v", got)
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "fresh") || !strings.Contains(joined, "output") {
		t.Fatalf("expected current output to remain after reset, got %v", got[:min(len(got), 2)])
	}
}

func TestDisplayStateIncludesCursor(t *testing.T) {
	p := New("test", "echo hi", "", ".")
	p.termMu.Lock()
	p.term.Write([]byte("ab\ncd")) //nolint:errcheck
	p.termMu.Unlock()

	got := p.DisplayState()
	if got.Output == "" {
		t.Fatal("expected display output")
	}
	if !got.Cursor.Visible {
		t.Fatal("expected cursor visible by default")
	}
	if got.Cursor.X < 0 || got.Cursor.Y < 0 {
		t.Fatalf("expected non-negative cursor coords, got %+v", got.Cursor)
	}
}

func TestDisplayStatePreservesCursorColumnPastText(t *testing.T) {
	p := New("test", "echo hi", "", ".")
	p.termMu.Lock()
	p.term.Write([]byte("ab")) //nolint:errcheck
	p.termMu.Unlock()

	got := p.DisplayState()
	lines := strings.Split(got.Output, "\n")
	if got.Cursor.X != 2 {
		t.Fatalf("expected cursor after typed text, got %+v", got.Cursor)
	}
	if len(lines) == 0 || len(lines[0]) < 3 {
		t.Fatalf("expected output to preserve cursor column, got %q", got.Output)
	}
}

func TestRunCmdKillReportsCommandError(t *testing.T) {
	p := NewWithCommandSpec("test", CommandSpec{Shell: "true"}, CommandSpec{Program: "__definitely_missing_muxedo_binary__"}, ".")

	err := p.RunCmdKill()
	if err == nil {
		t.Fatal("RunCmdKill() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "run kill command") {
		t.Fatalf("RunCmdKill() error = %v, want wrapped run error", err)
	}
}

func TestPanelConcurrentLifecycleReads(t *testing.T) {
	p := New("test", "sleep 1", "", ".")
	if err := p.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	stopReaders := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
					_ = p.Running()
					_ = p.Elapsed()
					_ = p.Output()
					_ = p.DisplayForView()
				}
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	if err := p.Restart(); err != nil {
		close(stopReaders)
		wg.Wait()
		t.Fatalf("restart failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	p.Stop()

	close(stopReaders)
	wg.Wait()
}

func TestPanelRepeatedStopAndRestart(t *testing.T) {
	p := New("test", "sleep 60", "", ".")
	if err := p.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	p.Stop()
	p.Stop()

	if err := p.Restart(); err != nil {
		t.Fatalf("restart after repeated stop failed: %v", err)
	}
	if !p.Running() {
		t.Fatal("panel should be running after restart")
	}

	p.Stop()
	p.Stop()
}
