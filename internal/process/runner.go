package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

type Panel struct {
	Name    string
	Command CommandSpec
	Kill    CommandSpec
	Dir     string

	ptmx       *os.File
	cmd        *exec.Cmd
	term       vt10x.Terminal
	termMu     sync.RWMutex
	running    atomic.Bool
	sb         *scrollbackWriter
	historyMu  sync.Mutex
	rawHistory []byte
	runtimeMu  sync.RWMutex
	startedAt  time.Time
	elapsed    time.Duration
	exitErr    error

	displayMu    sync.Mutex
	displayCache string
	displayDirty bool
}

func (p *Panel) ExitError() error {
	p.runtimeMu.RLock()
	defer p.runtimeMu.RUnlock()
	return p.exitErr
}

const maxReplayBytes = 1 << 20 // 1 MiB of recent PTY stream for resize reflow

func New(name, cmd, cmdKill, dir string) *Panel {
	return NewWithCommandSpec(name, CommandSpec{Shell: cmd}, CommandSpec{Shell: cmdKill}, dir)
}

func NewWithCommandSpec(name string, command, kill CommandSpec, dir string) *Panel {
	return &Panel{
		Name:    name,
		Command: command,
		Kill:    kill,
		Dir:     dir,
		term:    vt10x.New(vt10x.WithSize(80, 24)),
	}
}

func NewWithScrollback(name, cmd, cmdKill, dir, scrollbackDir string, maxBytes int64) *Panel {
	return NewWithScrollbackCommandSpec(name, CommandSpec{Shell: cmd}, CommandSpec{Shell: cmdKill}, dir, scrollbackDir, maxBytes)
}

func NewWithScrollbackCommandSpec(name string, command, kill CommandSpec, dir, scrollbackDir string, maxBytes int64) *Panel {
	return &Panel{
		Name:    name,
		Command: command,
		Kill:    kill,
		Dir:     dir,
		term:    vt10x.New(vt10x.WithSize(80, 24)),
		sb:      newScrollbackWriter(scrollbackDir, name, maxBytes),
	}
}

// ResetScrollback clears the persisted scrollback file and in-memory snapshot.
// It is used at app startup so each muxedo run starts with a clean history.
func (p *Panel) ResetScrollback() {
	if p.sb != nil {
		p.sb.Clear()
	}
}

func (p *Panel) Start() error {
	c, err := p.Command.Build(p.Dir, true)
	if err != nil {
		return err
	}
	ptmx, err := pty.Start(c)
	if err != nil {
		return err
	}

	p.termMu.RLock()
	cols, rows := p.term.Size()
	p.termMu.RUnlock()
	_ = pty.Setsize(ptmx, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})

	p.runtimeMu.Lock()
	p.cmd = c
	p.ptmx = ptmx
	p.startedAt = time.Now()
	p.elapsed = 0
	p.exitErr = nil
	p.runtimeMu.Unlock()

	p.running.Store(true)
	p.markDisplayDirty()

	go p.readLoop(ptmx, c)
	return nil
}

func (p *Panel) readLoop(ptmx *os.File, cmd *exec.Cmd) {
	buf := make([]byte, 4096)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			p.appendHistory(buf[:n])
			p.termMu.Lock()
			p.term.Write(buf[:n]) //nolint:errcheck
			termSnap := p.viewSnapshotLocked()
			p.termMu.Unlock()
			p.markDisplayDirty()
			if p.sb != nil {
				p.sb.Capture(termSnap)
			}
		}
		if err != nil {
			break
		}
	}

	var exitErr error
	if cmd != nil {
		exitErr = cmd.Wait()
	}

	p.runtimeMu.Lock()
	if p.ptmx == ptmx && p.exitErr == nil {
		p.exitErr = exitErr
	}
	p.runtimeMu.Unlock()

	p.markStoppedFor(ptmx)
}

func (p *Panel) Output() string {
	return p.viewSnapshot()
}

func (p *Panel) Resize(cols, rows int) {
	replay := p.historySnapshot()
	p.termMu.Lock()
	p.term = vt10x.New(vt10x.WithSize(cols, rows))
	if len(replay) > 0 {
		p.term.Write(replay) //nolint:errcheck
	}
	p.termMu.Unlock()

	p.runtimeMu.RLock()
	ptmx := p.ptmx
	p.runtimeMu.RUnlock()

	if ptmx != nil {
		_ = pty.Setsize(ptmx, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
		})
	}
	if p.sb != nil {
		p.sb.Reset()
	}
	p.markDisplayDirty()
}

func (p *Panel) Running() bool {
	return p.running.Load()
}

func (p *Panel) Stop() {
	p.runtimeMu.Lock()
	ptmx := p.ptmx
	cmd := p.cmd
	p.runtimeMu.Unlock()

	p.markStoppedFor(ptmx)

	if ptmx != nil {
		// Send Ctrl+C (0x03) to the pty
		_, _ = ptmx.Write([]byte{0x03})
		// Give it a tiny moment to process
		time.Sleep(10 * time.Millisecond)
		ptmx.Close()

		p.runtimeMu.Lock()
		if p.ptmx == ptmx {
			p.ptmx = nil
		}
		p.runtimeMu.Unlock()
	}

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)

		// Wait for the process to exit with a timeout
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		var exitErr error
		select {
		case err := <-done:
			exitErr = err
			// Process exited gracefully
		case <-time.After(200 * time.Millisecond):
			// Force kill if it didn't exit after 200ms
			_ = cmd.Process.Kill()
			exitErr = <-done // wait for the kill to complete
		}

		p.runtimeMu.Lock()
		if p.cmd == cmd {
			p.cmd = nil
			if p.exitErr == nil {
				p.exitErr = exitErr
			}
		}
		p.runtimeMu.Unlock()
	}
}

func (p *Panel) RunCmdKill() {
	if p.Kill.IsZero() {
		return
	}
	c, err := p.Kill.Build(p.Dir, true)
	if err != nil {
		return
	}
	_ = c.Run()
}

func (p *Panel) Restart() error {
	p.RunCmdKill()
	p.Stop()

	p.termMu.RLock()
	cols, rows := p.term.Size()
	p.termMu.RUnlock()

	p.termMu.Lock()
	p.term = vt10x.New(vt10x.WithSize(cols, rows))
	p.termMu.Unlock()

	p.historyMu.Lock()
	p.rawHistory = nil
	p.historyMu.Unlock()
	if p.sb != nil {
		p.sb.Clear()
	}
	p.markDisplayDirty()
	return p.Start()
}

func (p *Panel) Elapsed() time.Duration {
	p.runtimeMu.RLock()
	startedAt := p.startedAt
	elapsed := p.elapsed
	p.runtimeMu.RUnlock()

	if p.running.Load() && !startedAt.IsZero() {
		return time.Since(startedAt)
	}
	return elapsed
}

// ScrollbackPath returns the path to the scrollback log file, or empty if
// scrollback is not configured.
func (p *Panel) ScrollbackPath() string {
	if p.sb == nil {
		return ""
	}
	return p.sb.Path()
}

func (p *Panel) HistoryLines() []string {
	history := p.History()
	if len(history) == 0 {
		return nil
	}
	out := make([]string, len(history))
	for i, line := range history {
		out[i] = line.Text
	}
	return out
}

func (p *Panel) History() []HistoryLine {
	screen := normalizeScreen(p.Output())
	if p.sb == nil {
		out := make([]HistoryLine, len(screen))
		for i, line := range screen {
			out[i] = HistoryLine{ID: uint64(i + 1), Text: line}
		}
		return out
	}
	return p.sb.History(screen)
}

func (p *Panel) WriteInput(data []byte) error {
	p.runtimeMu.RLock()
	ptmx := p.ptmx
	p.runtimeMu.RUnlock()

	if ptmx == nil {
		return errors.New("panel process not started")
	}
	_, err := ptmx.Write(data)
	return err
}

// TerminalSize returns the current vt/PTY size in columns and rows.
func (p *Panel) TerminalSize() (cols, rows int) {
	p.termMu.RLock()
	defer p.termMu.RUnlock()
	return p.term.Size()
}

func (p *Panel) appendHistory(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	p.historyMu.Lock()
	p.rawHistory = append(p.rawHistory, chunk...)
	if len(p.rawHistory) > maxReplayBytes {
		p.rawHistory = p.rawHistory[len(p.rawHistory)-maxReplayBytes:]
	}
	p.historyMu.Unlock()
}

func (p *Panel) historySnapshot() []byte {
	p.historyMu.Lock()
	defer p.historyMu.Unlock()
	if len(p.rawHistory) == 0 {
		return nil
	}
	cp := make([]byte, len(p.rawHistory))
	copy(cp, p.rawHistory)
	return cp
}

func (p *Panel) markStoppedFor(ptmx *os.File) {
	p.runtimeMu.Lock()
	if ptmx != nil && p.ptmx != ptmx {
		p.runtimeMu.Unlock()
		return
	}
	p.runtimeMu.Unlock()

	if !p.running.Swap(false) {
		return
	}

	p.runtimeMu.Lock()
	if !p.startedAt.IsZero() {
		p.elapsed = time.Since(p.startedAt)
	}
	p.runtimeMu.Unlock()
}

func (p *Panel) markDisplayDirty() {
	p.displayMu.Lock()
	p.displayDirty = true
	p.displayMu.Unlock()
}

// DisplayDirty reports whether the terminal buffer changed since the last
// successful DisplayForView refresh (PTY read or resize).
func (p *Panel) DisplayDirty() bool {
	p.displayMu.Lock()
	d := p.displayDirty
	p.displayMu.Unlock()
	return d
}

// DisplayForView returns the terminal snapshot for TUI rendering. When the
// screen has not changed since the last call, it reuses the cached string and
// avoids vt10x.Terminal.String(). History and Output always read fresh state.
func (p *Panel) DisplayForView() string {
	for {
		p.displayMu.Lock()
		if !p.displayDirty {
			s := p.displayCache
			p.displayMu.Unlock()
			return s
		}
		p.displayMu.Unlock()

		s := p.viewSnapshot()

		p.displayMu.Lock()
		if !p.displayDirty {
			out := p.displayCache
			p.displayMu.Unlock()
			return out
		}
		p.displayCache = s
		p.displayDirty = false
		out := p.displayCache
		p.displayMu.Unlock()

		// A PTY read may have landed after term.String() but before we cleared
		// displayDirty; re-check so we do not return a stale cache for a frame.
		p.displayMu.Lock()
		again := p.displayDirty
		p.displayMu.Unlock()
		if again {
			continue
		}
		return out
	}
}

func (p *Panel) viewSnapshotLocked() string {
	cols, rows := p.term.Size()
	var b strings.Builder

	for y := 0; y < rows; y++ {
		// Find the last non-space/non-zero cell in this row for trimming
		lastNonEmpty := -1
		for x := cols - 1; x >= 0; x-- {
			glyph := p.term.Cell(x, y)
			if glyph.Char != 0 && glyph.Char != ' ' {
				lastNonEmpty = x
				break
			}
		}

		var lastFG, lastBG vt10x.Color = vt10x.DefaultFG, vt10x.DefaultBG
		for x := 0; x <= lastNonEmpty; x++ {
			glyph := p.term.Cell(x, y)
			if glyph.FG != lastFG || glyph.BG != lastBG {
				b.WriteString("\x1b[0")
				if glyph.FG != vt10x.DefaultFG {
					b.WriteString(fmt.Sprintf(";38;5;%d", glyph.FG))
				}
				if glyph.BG != vt10x.DefaultBG {
					b.WriteString(fmt.Sprintf(";48;5;%d", glyph.BG))
				}
				b.WriteString("m")
				lastFG, lastBG = glyph.FG, glyph.BG
			}
			char := glyph.Char
			if char == 0 {
				char = ' '
			}
			b.WriteRune(char)
		}
		b.WriteString("\x1b[0m")
		if y < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (p *Panel) viewSnapshot() string {
	p.termMu.RLock()
	defer p.termMu.RUnlock()
	return p.viewSnapshotLocked()
}
