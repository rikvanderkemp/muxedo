package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProfileParsesPanelsAndDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte(`
[panel.api]
workingdir = "."
cmd = "go test ./..."
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(got.Panels) != 1 {
		t.Fatalf("len(Panels) = %d, want 1", len(got.Panels))
	}
	if got.Panels[0].Name != "api" {
		t.Fatalf("Panels[0].Name = %q, want api", got.Panels[0].Name)
	}
	if got.Panels[0].Cmd != "go test ./..." {
		t.Fatalf("Panels[0].Cmd = %q", got.Panels[0].Cmd)
	}
	if !filepath.IsAbs(got.Panels[0].WorkingDir) {
		t.Fatalf("Panels[0].WorkingDir = %q, want absolute path", got.Panels[0].WorkingDir)
	}
	if got.Scrollback.MaxBytes != defaultMaxBytes {
		t.Fatalf("Scrollback.MaxBytes = %d, want %d", got.Scrollback.MaxBytes, defaultMaxBytes)
	}
}

func TestLoadProfileRequiresPanels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "profile has no [panel.*] sections") {
		t.Fatalf("Load() error = %q", err)
	}
}
