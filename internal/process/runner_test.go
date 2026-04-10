package process

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewPanelIsNotRunning(t *testing.T) {
	p := New("test", "echo hi", ".")
	if p.Running() {
		t.Fatal("new panel should not be running")
	}
}

func TestPanelRunningAfterStart(t *testing.T) {
	p := New("test", "sleep 60", ".")
	if err := p.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer p.Stop()

	if !p.Running() {
		t.Fatal("panel should be running after Start()")
	}
}

func TestPanelStoppedAfterProcessExits(t *testing.T) {
	p := New("test", "true", ".")
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
	p := New("test", "true", ".")
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
	p := New("test", "sleep 60", ".")
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
	p := New("test", "sleep 60", ".")
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
	p := New("test", "sleep 60", ".")
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

func TestResetScrollbackClearsPersistedHistory(t *testing.T) {
	dir := t.TempDir()
	p := NewWithScrollback("test", "echo hi", ".", dir, 0)

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
	p := NewWithScrollback("test", "echo hi", ".", dir, 0)

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
