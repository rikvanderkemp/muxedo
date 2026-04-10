package process

import (
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
