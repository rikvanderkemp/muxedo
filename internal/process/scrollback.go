package process

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type scrollbackWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	prev     []string
}

func newScrollbackWriter(dir, panelName string, maxBytes int64) *scrollbackWriter {
	name := sanitizeName(panelName)
	return &scrollbackWriter{
		path:     filepath.Join(dir, name+".log"),
		maxBytes: maxBytes,
	}
}

func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "" {
		s = "panel"
	}
	return s
}

// Capture compares the current screen against the previous snapshot and appends
// any lines that scrolled off the top to the on-disk log file.
func (sw *scrollbackWriter) Capture(screen string) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	cur := normalizeScreen(screen)

	if sw.prev != nil {
		if k := detectScrollUp(sw.prev, cur); k > 0 {
			sw.appendLines(sw.prev[:k])
		}
	}

	sw.prev = cur
}

// Reset clears the in-memory snapshot (e.g. after a resize) but keeps the
// on-disk file intact.
func (sw *scrollbackWriter) Reset() {
	sw.mu.Lock()
	sw.prev = nil
	sw.mu.Unlock()
}

// Clear truncates the on-disk file and resets the snapshot.
func (sw *scrollbackWriter) Clear() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.prev = nil
	_ = os.Remove(sw.path)
}

// Path returns the absolute path to the scrollback file.
func (sw *scrollbackWriter) Path() string {
	return sw.path
}

func normalizeScreen(screen string) []string {
	lines := strings.Split(screen, "\n")
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(l, " ")
	}
	return out
}

// detectScrollUp returns how many lines scrolled off the top: the largest k
// such that cur[i] == prev[i+k] for all i in 0..len-k-1.
func detectScrollUp(prev, cur []string) int {
	n := len(prev)
	if n == 0 || len(cur) != n {
		return 0
	}
	for k := n - 1; k >= 1; k-- {
		match := true
		for i := 0; i < n-k; i++ {
			if cur[i] != prev[i+k] {
				match = false
				break
			}
		}
		if match {
			return k
		}
	}
	return 0
}

func (sw *scrollbackWriter) appendLines(lines []string) {
	if len(lines) == 0 {
		return
	}

	if err := os.MkdirAll(filepath.Dir(sw.path), 0o755); err != nil {
		return
	}

	f, err := os.OpenFile(sw.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}

	var buf bytes.Buffer
	for _, l := range lines {
		buf.WriteString(l)
		buf.WriteByte('\n')
	}
	_, _ = f.Write(buf.Bytes())
	f.Close()

	sw.trimFile()
}

func (sw *scrollbackWriter) trimFile() {
	if sw.maxBytes <= 0 {
		return
	}
	info, err := os.Stat(sw.path)
	if err != nil || info.Size() <= sw.maxBytes {
		return
	}

	data, err := os.ReadFile(sw.path)
	if err != nil {
		return
	}

	cut := int64(len(data)) - sw.maxBytes
	if cut <= 0 {
		return
	}
	// Advance past the next newline so the file starts at a line boundary.
	idx := bytes.IndexByte(data[cut:], '\n')
	if idx < 0 {
		return
	}
	trimmed := data[cut+int64(idx)+1:]
	_ = os.WriteFile(sw.path, trimmed, 0o644)
}
