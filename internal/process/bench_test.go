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
