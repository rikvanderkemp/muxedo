package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectScrollUp(t *testing.T) {
	prev := []string{"line1", "line2", "line3", "line4"}
	cur := []string{"line3", "line4", "new1", "new2"}

	k := detectScrollUp(prev, cur)
	if k != 2 {
		t.Fatalf("expected scroll of 2, got %d", k)
	}
}

func TestDetectScrollUpNoScroll(t *testing.T) {
	lines := []string{"a", "b", "c"}
	if k := detectScrollUp(lines, lines); k != 0 {
		t.Fatalf("expected 0, got %d", k)
	}
}

func TestDetectScrollUpSingleLine(t *testing.T) {
	prev := []string{"line1", "line2", "line3"}
	cur := []string{"line2", "line3", "line4"}

	k := detectScrollUp(prev, cur)
	if k != 1 {
		t.Fatalf("expected scroll of 1, got %d", k)
	}
}

func TestDetectScrollUpLengthMismatch(t *testing.T) {
	prev := []string{"a", "b"}
	cur := []string{"a", "b", "c"}

	if k := detectScrollUp(prev, cur); k != 0 {
		t.Fatalf("expected 0 for mismatched lengths, got %d", k)
	}
}

func TestNormalizeScreen(t *testing.T) {
	screen := "hello   \nworld  \n   "
	got := normalizeScreen(screen)
	want := []string{"hello", "world", ""}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestScrollbackWriterCaptureAndFile(t *testing.T) {
	dir := t.TempDir()
	sw := newScrollbackWriter(dir, "test-panel", 0)

	screen1 := "line1\nline2\nline3"
	sw.Capture(screen1)

	screen2 := "line2\nline3\nline4"
	sw.Capture(screen2)

	data, err := os.ReadFile(sw.Path())
	if err != nil {
		t.Fatalf("reading scrollback: %v", err)
	}

	if got := strings.TrimRight(string(data), "\n"); got != "line1" {
		t.Fatalf("expected scrollback to contain 'line1', got %q", got)
	}
}

func TestScrollbackWriterMultiLineScroll(t *testing.T) {
	dir := t.TempDir()
	sw := newScrollbackWriter(dir, "test", 0)

	sw.Capture("a\nb\nc\nd")
	sw.Capture("c\nd\ne\nf")

	data, err := os.ReadFile(sw.Path())
	if err != nil {
		t.Fatalf("reading scrollback: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 || lines[0] != "a" || lines[1] != "b" {
		t.Fatalf("expected [a b], got %v", lines)
	}
}

func TestScrollbackWriterTrimFile(t *testing.T) {
	dir := t.TempDir()
	sw := newScrollbackWriter(dir, "trim", 30)

	path := sw.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	var content strings.Builder
	for i := 0; i < 20; i++ {
		content.WriteString("abcdefghij\n")
	}
	if err := os.WriteFile(path, []byte(content.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	sw.trimFile()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) > 30 {
		t.Fatalf("expected file <= 30 bytes after trim, got %d", len(data))
	}
	if len(data) > 0 && data[0] == '\n' {
		t.Fatal("trimmed file should not start with newline")
	}
}

func TestScrollbackWriterClear(t *testing.T) {
	dir := t.TempDir()
	sw := newScrollbackWriter(dir, "clear", 0)

	sw.Capture("a\nb\nc")
	sw.Capture("b\nc\nd")

	if _, err := os.Stat(sw.Path()); os.IsNotExist(err) {
		t.Fatal("expected scrollback file to exist after capture")
	}

	sw.Clear()

	if _, err := os.Stat(sw.Path()); !os.IsNotExist(err) {
		t.Fatal("expected scrollback file to be removed after clear")
	}
}

func TestScrollbackWriterReset(t *testing.T) {
	dir := t.TempDir()
	sw := newScrollbackWriter(dir, "reset", 0)

	sw.Capture("a\nb\nc")
	sw.Reset()
	sw.Capture("x\ny\nz")

	if _, err := os.Stat(sw.Path()); !os.IsNotExist(err) {
		data, _ := os.ReadFile(sw.Path())
		t.Fatalf("expected no scrollback file after reset + non-scroll capture, but found %q", string(data))
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"simple", "simple"},
		{"my-panel", "my-panel"},
		{"has spaces", "has_spaces"},
		{"../etc/passwd", "___etc_passwd"},
		{"", "panel"},
	}
	for _, tt := range tests {
		if got := sanitizeName(tt.in); got != tt.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
