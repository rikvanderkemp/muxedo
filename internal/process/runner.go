// SPDX-License-Identifier: MIT
package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

// Panel manages one running PTY-backed process and its rendered history.
type Panel struct {
	Name    string
	Command CommandSpec
	Kill    CommandSpec
	Dir     string
	// killTimeout bounds optional kill-command execution during stop/quit flows.
	killTimeout time.Duration

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
	waitDone   chan error

	displayMu    sync.Mutex
	displayCache DisplayState
	displayDirty bool
}

type CursorState struct {
	Visible bool
	X       int
	Y       int
}

type DisplayState struct {
	Output string
	Cursor CursorState
}

// ExitError returns panel process exit error recorded after shutdown.
func (p *Panel) ExitError() error {
	p.runtimeMu.RLock()
	defer p.runtimeMu.RUnlock()
	return p.exitErr
}

const maxReplayBytes = 1 << 20 // 1 MiB of recent PTY stream for resize reflow

const defaultRunCmdKillTimeout = 2 * time.Second

// New creates panel from shell command strings.
func New(name, cmd, cmdKill, dir string) *Panel {
	return NewWithCommandSpec(name, CommandSpec{Shell: cmd}, CommandSpec{Shell: cmdKill}, dir)
}

// NewWithCommandSpec creates panel from validated command specs.
func NewWithCommandSpec(name string, command, kill CommandSpec, dir string) *Panel {
	return &Panel{
		Name:         name,
		Command:      command,
		Kill:         kill,
		Dir:          dir,
		killTimeout:  defaultRunCmdKillTimeout,
		term:         vt10x.New(vt10x.WithSize(80, 24)),
		displayDirty: true,
	}
}

// NewWithScrollback creates panel with persisted scrollback using shell command strings.
func NewWithScrollback(name, cmd, cmdKill, dir, scrollbackDir string, maxBytes int64) *Panel {
	return NewWithScrollbackCommandSpec(name, CommandSpec{Shell: cmd}, CommandSpec{Shell: cmdKill}, dir, scrollbackDir, maxBytes)
}

// NewWithScrollbackCommandSpec creates panel with persisted scrollback using command specs.
func NewWithScrollbackCommandSpec(name string, command, kill CommandSpec, dir, scrollbackDir string, maxBytes int64) *Panel {
	return &Panel{
		Name:         name,
		Command:      command,
		Kill:         kill,
		Dir:          dir,
		killTimeout:  defaultRunCmdKillTimeout,
		term:         vt10x.New(vt10x.WithSize(80, 24)),
		sb:           newScrollbackWriter(scrollbackDir, name, maxBytes),
		displayDirty: true,
	}
}

// ResetScrollback clears the persisted scrollback file and in-memory snapshot.
// It is used at app startup so each muxedo run starts with a clean history.
func (p *Panel) ResetScrollback() {
	if p.sb != nil {
		p.sb.Clear()
	}
}

// Start launches panel process and begins PTY capture.
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
	p.waitDone = make(chan error, 1)
	p.runtimeMu.Unlock()

	p.running.Store(true)
	p.markDisplayDirty()

	go p.readLoop(ptmx)
	go p.waitLoop(ptmx, c)
	return nil
}

func (p *Panel) readLoop(ptmx *os.File) {
	buf := make([]byte, 4096)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			p.appendHistory(buf[:n])
			p.termMu.Lock()
			p.term.Write(buf[:n]) //nolint:errcheck
			termSnap := p.viewStateLocked().Output
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
}

func (p *Panel) waitLoop(ptmx *os.File, cmd *exec.Cmd) {
	exitErr := cmd.Wait()
	p.markStoppedForCmd(cmd)

	p.runtimeMu.Lock()
	waitDone := p.waitDone
	if p.cmd == cmd {
		p.cmd = nil
		p.waitDone = nil
	}
	if p.ptmx == ptmx {
		p.ptmx = nil
	}
	if p.exitErr == nil {
		p.exitErr = exitErr
	}
	p.runtimeMu.Unlock()

	if waitDone != nil {
		waitDone <- exitErr
		close(waitDone)
	}
	if ptmx != nil {
		_ = ptmx.Close()
	}
}

// Output returns current terminal snapshot as plain text.
func (p *Panel) Output() string {
	return p.viewState().Output
}

// Resize resizes panel terminal and reflows buffered history.
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

// Running reports whether panel process is still active.
func (p *Panel) Running() bool {
	return p.running.Load()
}

// Stop requests panel shutdown and waits briefly for process exit.
func (p *Panel) Stop() {
	p.runtimeMu.Lock()
	ptmx := p.ptmx
	cmd := p.cmd
	waitDone := p.waitDone
	p.runtimeMu.Unlock()

	p.markStoppedForCmd(cmd)

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
		p.waitForExit(cmd, waitDone)
	}
}

func (p *Panel) waitForExit(cmd *exec.Cmd, waitDone <-chan error) {
	if waitDone == nil {
		return
	}

	select {
	case <-waitDone:
		return
	case <-time.After(200 * time.Millisecond):
	}

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	<-waitDone
}

// RunCmdKill runs optional panel kill command.
func (p *Panel) RunCmdKill() error {
	if p.Kill.IsZero() {
		return nil
	}
	timeout := p.killTimeout
	if timeout <= 0 {
		timeout = defaultRunCmdKillTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c, err := p.Kill.BuildContext(ctx, p.Dir, true)
	if err != nil {
		return fmt.Errorf("build kill command: %w", err)
	}
	if err := c.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("run kill command: timed out after %s: %w", timeout, ctx.Err())
		}
		return fmt.Errorf("run kill command: %w", err)
	}
	return nil
}

// Restart stops panel, resets terminal state, and starts it again.
func (p *Panel) Restart() error {
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

// Elapsed returns runtime for running panels or cached runtime after exit.
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

// HistoryLines returns panel history as plain text lines.
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

// History returns panel history with stable line identifiers.
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

// WriteInput writes raw bytes to panel PTY input stream.
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

func (p *Panel) markStoppedForCmd(cmd *exec.Cmd) {
	p.runtimeMu.Lock()
	if cmd != nil && p.cmd != nil && p.cmd != cmd {
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
	return p.DisplayState().Output
}

// DisplayState returns terminal snapshot and cursor state for TUI rendering.
func (p *Panel) DisplayState() DisplayState {
	for {
		p.displayMu.Lock()
		if !p.displayDirty {
			s := p.displayCache
			p.displayMu.Unlock()
			return s
		}
		p.displayMu.Unlock()

		s := p.viewState()

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

func (p *Panel) viewStateLocked() DisplayState {
	cols, rows := p.term.Size()
	cur := p.term.Cursor()
	cursorVisible := p.term.CursorVisible()
	if cur.X < 0 {
		cur.X = 0
	}
	if cur.Y < 0 {
		cur.Y = 0
	}
	if cols > 0 && cur.X >= cols {
		cur.X = cols - 1
	}
	if rows > 0 && cur.Y >= rows {
		cur.Y = rows - 1
	}
	var b strings.Builder
	b.Grow(rows * (cols + 8))
	rowGlyphs := make([]vt10x.Glyph, cols)

	for y := 0; y < rows; y++ {
		// Find the last non-space/non-zero cell in this row for trimming
		lastNonEmpty := -1
		for x := 0; x < cols; x++ {
			glyph := p.term.Cell(x, y)
			rowGlyphs[x] = glyph
			if glyph.Char != 0 && glyph.Char != ' ' {
				lastNonEmpty = x
			}
		}
		if cursorVisible && y == cur.Y && cur.X > lastNonEmpty {
			lastNonEmpty = cur.X
		}

		var lastFG, lastBG vt10x.Color = vt10x.DefaultFG, vt10x.DefaultBG
		for x := 0; x <= lastNonEmpty; x++ {
			glyph := rowGlyphs[x]
			if glyph.FG != lastFG || glyph.BG != lastBG {
				writeSGR(&b, glyph.FG, glyph.BG)
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

	return DisplayState{
		Output: b.String(),
		Cursor: CursorState{
			Visible: cursorVisible,
			X:       cur.X,
			Y:       cur.Y,
		},
	}
}

func writeSGR(b *strings.Builder, fg, bg vt10x.Color) {
	b.WriteString("\x1b[0")
	if fg != vt10x.DefaultFG {
		b.WriteString(";38;5;")
		b.WriteString(strconv.Itoa(int(fg)))
	}
	if bg != vt10x.DefaultBG {
		b.WriteString(";48;5;")
		b.WriteString(strconv.Itoa(int(bg)))
	}
	b.WriteByte('m')
}

func (p *Panel) viewSnapshot() string {
	return p.viewState().Output
}

func (p *Panel) viewState() DisplayState {
	p.termMu.RLock()
	defer p.termMu.RUnlock()
	return p.viewStateLocked()
}
