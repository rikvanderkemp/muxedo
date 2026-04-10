package process

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

type Panel struct {
	Name string
	Cmd  string
	Dir  string

	ptmx    *os.File
	cmd     *exec.Cmd
	term    vt10x.Terminal
	termMu  sync.RWMutex
	running atomic.Bool
	sb      *scrollbackWriter
	historyMu       sync.Mutex
	rawHistory      []byte
}

const maxReplayBytes = 1 << 20 // 1 MiB of recent PTY stream for resize reflow

func New(name, cmd, dir string) *Panel {
	return &Panel{
		Name: name,
		Cmd:  cmd,
		Dir:  dir,
		term: vt10x.New(vt10x.WithSize(80, 24)),
	}
}

func NewWithScrollback(name, cmd, dir, scrollbackDir string, maxBytes int64) *Panel {
	return &Panel{
		Name: name,
		Cmd:  cmd,
		Dir:  dir,
		term: vt10x.New(vt10x.WithSize(80, 24)),
		sb:   newScrollbackWriter(scrollbackDir, name, maxBytes),
	}
}

func (p *Panel) Start() error {
	c := exec.Command("sh", "-lc", p.Cmd)
	c.Dir = p.Dir
	c.Env = os.Environ()

	ptmx, err := pty.Start(c)
	if err != nil {
		return err
	}

	p.cmd = c
	p.ptmx = ptmx
	p.running.Store(true)

	go p.readLoop()
	return nil
}

func (p *Panel) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := p.ptmx.Read(buf)
		if n > 0 {
			p.appendHistory(buf[:n])
			p.termMu.Lock()
			p.term.Write(buf[:n]) //nolint:errcheck
			termSnap := p.term.String()
			p.termMu.Unlock()
			if p.sb != nil {
				p.sb.Capture(termSnap)
			}
		}
		if err != nil {
			p.running.Store(false)
			return
		}
	}
}

func (p *Panel) Output() string {
	p.termMu.RLock()
	defer p.termMu.RUnlock()
	return p.term.String()
}

func (p *Panel) Resize(cols, rows int) {
	replay := p.historySnapshot()
	p.termMu.Lock()
	p.term = vt10x.New(vt10x.WithSize(cols, rows))
	if len(replay) > 0 {
		p.term.Write(replay) //nolint:errcheck
	}
	p.termMu.Unlock()
	if p.ptmx != nil {
		_ = pty.Setsize(p.ptmx, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
		})
	}
	if p.sb != nil {
		p.sb.Reset()
	}
}

func (p *Panel) Running() bool {
	return p.running.Load()
}

func (p *Panel) Stop() {
	p.running.Store(false)
	if p.ptmx != nil {
		p.ptmx.Close()
		p.ptmx = nil
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(os.Interrupt)
		_ = p.cmd.Wait()
		p.cmd = nil
	}
}

func (p *Panel) Restart() error {
	p.Stop()
	p.termMu.Lock()
	p.term = vt10x.New(vt10x.WithSize(80, 24))
	p.termMu.Unlock()
	p.historyMu.Lock()
	p.rawHistory = nil
	p.historyMu.Unlock()
	if p.sb != nil {
		p.sb.Clear()
	}
	return p.Start()
}

// ScrollbackPath returns the path to the scrollback log file, or empty if
// scrollback is not configured.
func (p *Panel) ScrollbackPath() string {
	if p.sb == nil {
		return ""
	}
	return p.sb.Path()
}

func (p *Panel) WriteInput(data []byte) error {
	if p.ptmx == nil {
		return errors.New("panel process not started")
	}
	_, err := p.ptmx.Write(data)
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
