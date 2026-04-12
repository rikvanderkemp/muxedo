// SPDX-License-Identifier: MIT

// Profiling: from repo root,
//
//	go test -run=^$ -bench=. -benchmem -benchtime=2s \
//	  -cpuprofile=/tmp/muxedo-cpu.prof -memprofile=/tmp/muxedo-mem.prof ./internal/process/...
//
//	go tool pprof -http=:0 /tmp/muxedo-cpu.prof
//	go tool pprof -http=:0 -sample_index=alloc_space /tmp/muxedo-mem.prof
//
// See also scripts/profile_muxedo.sh.
package process

import (
	"strings"
	"testing"
)

func BenchmarkNormalizeScreen_40Lines(b *testing.B) {
	raw := strings.Repeat("hello world  \n", 40)
	b.ReportAllocs()
	for b.Loop() {
		_ = normalizeScreen(raw)
	}
}

func BenchmarkPanelOutput(b *testing.B) {
	p := New("bench", "true", "", ".")
	p.termMu.Lock()
	_, _ = p.term.Write([]byte(strings.Repeat("x", 80) + "\r\n"))
	p.termMu.Unlock()

	b.ReportAllocs()
	for b.Loop() {
		_ = p.Output()
	}
}

func BenchmarkPanelDisplayForView_cached(b *testing.B) {
	p := New("bench", "true", "", ".")
	p.termMu.Lock()
	_, _ = p.term.Write([]byte("hello\r\n"))
	p.termMu.Unlock()
	p.markDisplayDirty()

	// Prime cache.
	_ = p.DisplayForView()

	b.ReportAllocs()
	for b.Loop() {
		_ = p.DisplayForView()
	}
}

func BenchmarkPanelDisplayState_dirty_small(b *testing.B) {
	p := New("bench", "true", "", ".")
	p.termMu.Lock()
	_, _ = p.term.Write([]byte("\x1b[31mhello\x1b[0m\r\nworld\r\n"))
	p.termMu.Unlock()

	b.ReportAllocs()
	for b.Loop() {
		p.markDisplayDirty()
		_ = p.DisplayState()
	}
}

func BenchmarkPanelDisplayState_dirty_large(b *testing.B) {
	p := New("bench", "true", "", ".")
	p.Resize(160, 48)

	var screen strings.Builder
	for row := 0; row < 48; row++ {
		for col := 0; col < 160; col++ {
			if col%16 == 0 {
				screen.WriteString("\x1b[3")
				screen.WriteByte(byte('1' + (col/16)%7))
				screen.WriteByte('m')
			}
			screen.WriteByte('a' + byte((row+col)%26))
		}
		if row < 47 {
			screen.WriteString("\r\n")
		}
	}

	p.termMu.Lock()
	_, _ = p.term.Write([]byte(screen.String()))
	p.termMu.Unlock()

	b.ReportAllocs()
	for b.Loop() {
		p.markDisplayDirty()
		_ = p.DisplayState()
	}
}

func BenchmarkScrollbackWriterHistory(b *testing.B) {
	b.StopTimer()
	dir := b.TempDir()
	sw := newScrollbackWriter(dir, "panel", 1<<20)
	screen := normalizeScreen(strings.Repeat("line\n", 24))
	b.StartTimer()
	b.ReportAllocs()

	for b.Loop() {
		_ = sw.History(append([]string(nil), screen...))
	}
}

func BenchmarkMergeHistoryLineRecords_WorstCaseBoundaryScan(b *testing.B) {
	scrollback := make([]HistoryLine, 4096)
	for i := range scrollback {
		scrollback[i] = HistoryLine{ID: uint64(i + 1), Text: "line"}
	}
	screen := make([]HistoryLine, 256)
	copy(screen, scrollback[len(scrollback)-len(screen)+1:])
	screen[len(screen)-1] = HistoryLine{ID: uint64(len(scrollback) + 1), Text: "tail"}

	b.ReportAllocs()
	for b.Loop() {
		_ = mergeHistoryLineRecords(scrollback, screen)
	}
}
