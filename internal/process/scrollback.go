// SPDX-License-Identifier: MIT
package process

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type HistoryLine struct {
	ID   uint64
	Text string
}

type scrollbackWriter struct {
	mu            sync.Mutex
	scrollbackDir string
	relFile       string // single path segment: sanitizeName(panel)+".log"
	maxBytes      int64
	prev          []HistoryLine
	lines         []HistoryLine
	loaded        bool
	nextID        uint64
}

func newScrollbackWriter(dir, panelName string, maxBytes int64) *scrollbackWriter {
	name := sanitizeName(panelName)
	return &scrollbackWriter{
		scrollbackDir: dir,
		relFile:       name + ".log",
		maxBytes:      maxBytes,
	}
}

// withRoot ensures scrollbackDir exists, opens an [os.Root] for it, and runs fn.
// All file names are resolved under the root only (see [os.Root]). Caller must hold sw.mu.
func (sw *scrollbackWriter) withRoot(fn func(*os.Root) error) error {
	if err := os.MkdirAll(sw.scrollbackDir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(sw.scrollbackDir)
	if err != nil {
		return err
	}
	defer root.Close()
	return fn(root)
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

	sw.prev = sw.syncScreenLocked(normalizeScreen(screen))
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
	sw.lines = nil
	sw.loaded = true
	// Best-effort: remove persisted file; ignore errors (e.g. missing file).
	_ = sw.withRoot(func(root *os.Root) error {
		err := root.Remove(sw.relFile)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	})
}

// Path returns the path to the scrollback file (under scrollbackDir).
func (sw *scrollbackWriter) Path() string {
	return filepath.Join(sw.scrollbackDir, sw.relFile)
}

// History merges persisted scrollback with the current screen. The screen
// argument must be normalized the same way as normalizeScreen (trimmed
// trailing spaces per line, split on "\n").
func (sw *scrollbackWriter) History(screen []string) []HistoryLine {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	cur := sw.syncScreenLocked(screen)
	if len(sw.lines) == 0 {
		return append([]HistoryLine(nil), cur...)
	}
	if len(cur) == 0 {
		return append([]HistoryLine(nil), sw.lines...)
	}
	return mergeHistoryLineRecords(sw.lines, cur)
}

func (sw *scrollbackWriter) Lines() []string {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.ensureLinesLoadedLocked()
	if len(sw.lines) == 0 {
		return nil
	}
	return historyLineTexts(sw.lines)
}

func normalizeScreen(screen string) []string {
	lines := strings.Split(screen, "\n")
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(l, " ")
	}
	return out
}

// trimTrailingEmptyStrings drops trailing "" entries (common when Split leaves
// a final newline). Mismatched trailing empties make len(prev)!=len(cur) and
// cause detectScrollUp to bail, which then mis-assigns stable row IDs and
// shifts scroll marks onto the wrong lines.
func trimTrailingEmptyStrings(a []string) []string {
	n := len(a)
	for n > 0 && a[n-1] == "" {
		n--
	}
	return a[:n]
}

func trimTrailingEmptyHistory(a []HistoryLine) []HistoryLine {
	n := len(a)
	for n > 0 && a[n-1].Text == "" {
		n--
	}
	return a[:n]
}

// detectScrollUp returns how many lines scrolled off the top: the smallest k
// such that cur[i] == prev[i+k] for all i in 0..len-k-1.
func detectScrollUp(prev, cur []string) int {
	n := len(prev)
	if n == 0 || len(cur) != n {
		return 0
	}
	for k := 0; k < n; k++ {
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

func mergeHistoryLines(scrollback, screen []string) []string {
	if len(scrollback) == 0 {
		return append([]string(nil), screen...)
	}
	if len(screen) == 0 {
		return append([]string(nil), scrollback...)
	}

	maxOverlap := min(len(scrollback), len(screen))
	overlap := 0
	for k := maxOverlap; k >= 1; k-- {
		match := true
		for i := 0; i < k; i++ {
			if scrollback[len(scrollback)-k+i] != screen[i] {
				match = false
				break
			}
		}
		if match {
			overlap = k
			break
		}
	}

	merged := make([]string, 0, len(scrollback)+len(screen)-overlap)
	merged = append(merged, scrollback...)
	merged = append(merged, screen[overlap:]...)
	return merged
}

func mergeHistoryLineRecords(scrollback, screen []HistoryLine) []HistoryLine {
	if len(scrollback) == 0 {
		return append([]HistoryLine(nil), screen...)
	}
	if len(screen) == 0 {
		return append([]HistoryLine(nil), scrollback...)
	}

	maxOverlap := min(len(scrollback), len(screen))
	overlap := 0
	for k := maxOverlap; k >= 1; k-- {
		match := true
		for i := 0; i < k; i++ {
			if scrollback[len(scrollback)-k+i].ID != screen[i].ID {
				match = false
				break
			}
		}
		if match {
			overlap = k
			break
		}
	}

	// Drop the scrollback tail that duplicates the live screen prefix (matched by ID),
	// then append the full current screen so in-place row updates keep stable IDs
	// without leaving stale text from persisted scrollback.
	merged := make([]HistoryLine, 0, len(scrollback)+len(screen)-overlap)
	if overlap > 0 {
		merged = append(merged, scrollback[:len(scrollback)-overlap]...)
	} else {
		merged = append(merged, scrollback...)
	}
	merged = append(merged, screen...)
	return merged
}

func (sw *scrollbackWriter) appendLines(lines []HistoryLine) {
	if len(lines) == 0 {
		return
	}
	sw.ensureLinesLoadedLocked()

	// Best-effort scrollback persistence.
	if err := sw.withRoot(func(root *os.Root) error {
		f, err := root.OpenFile(sw.relFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		var buf bytes.Buffer
		for _, l := range lines {
			buf.WriteString(l.Text)
			buf.WriteByte('\n')
		}
		_, err = f.Write(buf.Bytes())
		return err
	}); err != nil {
		return
	}

	sw.lines = append(sw.lines, lines...)
	sw.trimFile()
}

func (sw *scrollbackWriter) trimFile() {
	if sw.maxBytes <= 0 {
		return
	}
	var trimmedForLines []byte
	// Best-effort scrollback persistence.
	err := sw.withRoot(func(root *os.Root) error {
		info, err := root.Stat(sw.relFile)
		if err != nil {
			return err
		}
		if info.Size() <= sw.maxBytes {
			return nil
		}
		data, err := root.ReadFile(sw.relFile)
		if err != nil {
			return err
		}
		cut := int64(len(data)) - sw.maxBytes
		if cut <= 0 {
			return nil
		}
		// Advance past the next newline so the file starts at a line boundary.
		idx := bytes.IndexByte(data[cut:], '\n')
		if idx < 0 {
			return nil
		}
		trimmed := data[cut+int64(idx)+1:]
		if err := root.WriteFile(sw.relFile, trimmed, 0o644); err != nil {
			return err
		}
		trimmedForLines = trimmed
		return nil
	})
	if err != nil || len(trimmedForLines) == 0 {
		return
	}
	sw.lines = sw.makeHistoryLines(parseScrollbackLines(trimmedForLines))
	sw.loaded = true
}

func (sw *scrollbackWriter) ensureLinesLoadedLocked() {
	if sw.loaded {
		return
	}
	var data []byte
	err := sw.withRoot(func(root *os.Root) error {
		var err error
		data, err = root.ReadFile(sw.relFile)
		return err
	})
	if err != nil {
		sw.lines = nil
		sw.loaded = true
		return
	}
	sw.lines = sw.makeHistoryLines(parseScrollbackLines(data))
	sw.loaded = true
}

func parseScrollbackLines(data []byte) []string {
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func (sw *scrollbackWriter) syncScreenLocked(screen []string) []HistoryLine {
	screen = trimTrailingEmptyStrings(screen)
	if len(screen) == 0 {
		sw.prev = nil
		return nil
	}
	if sw.prev == nil {
		sw.prev = sw.makeHistoryLines(screen)
		return append([]HistoryLine(nil), sw.prev...)
	}

	sw.prev = trimTrailingEmptyHistory(sw.prev)

	prevText := historyLineTexts(sw.prev)
	if k := detectScrollUp(prevText, screen); k > 0 {
		sw.appendLines(sw.prev[:k])

		cur := make([]HistoryLine, len(screen))
		copy(cur, sw.prev[k:])
		for i := len(screen) - k; i < len(screen); i++ {
			cur[i] = sw.newHistoryLine(screen[i])
		}
		sw.prev = cur
		return append([]HistoryLine(nil), sw.prev...)
	}

	cur := make([]HistoryLine, len(screen))
	for i, text := range screen {
		if i < len(sw.prev) && sw.prev[i].Text == text {
			cur[i] = sw.prev[i]
			continue
		}
		if i < len(sw.prev) {
			// Same terminal row index: keep stable ID so scroll marks survive
			// repaints, progress output, and empty↔non-empty line transitions.
			cur[i] = HistoryLine{ID: sw.prev[i].ID, Text: text}
			continue
		}
		cur[i] = sw.newHistoryLine(text)
	}
	sw.prev = cur
	return append([]HistoryLine(nil), sw.prev...)
}

func (sw *scrollbackWriter) makeHistoryLines(lines []string) []HistoryLine {
	if len(lines) == 0 {
		return nil
	}
	out := make([]HistoryLine, len(lines))
	for i, line := range lines {
		out[i] = sw.newHistoryLine(line)
	}
	return out
}

func (sw *scrollbackWriter) newHistoryLine(text string) HistoryLine {
	sw.nextID++
	return HistoryLine{ID: sw.nextID, Text: text}
}

func historyLineTexts(lines []HistoryLine) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = line.Text
	}
	return out
}
