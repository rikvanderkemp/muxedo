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
program = "go"
args = ["test", "./..."]
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
	if got.Panels[0].Command.Program != "go" {
		t.Fatalf("Panels[0].Command.Program = %q", got.Panels[0].Command.Program)
	}
	if strings.Join(got.Panels[0].Command.Args, " ") != "test ./..." {
		t.Fatalf("Panels[0].Command.Args = %q", got.Panels[0].Command.Args)
	}
	if !filepath.IsAbs(got.Panels[0].WorkingDir) {
		t.Fatalf("Panels[0].WorkingDir = %q, want absolute path", got.Panels[0].WorkingDir)
	}
	if got.Scrollback.MaxBytes != defaultMaxBytes {
		t.Fatalf("Scrollback.MaxBytes = %d, want %d", got.Scrollback.MaxBytes, defaultMaxBytes)
	}
}

func TestLoadProfileParsesShellFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte(`
[[startup]]
shell = "docker compose up -d"

[panel.api]
workingdir = "."
shell = "go test ./..."
shell_kill = "pkill -f muxedo"
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.Startup[0].Command.Shell != "docker compose up -d" {
		t.Fatalf("Startup[0].Command.Shell = %q", got.Startup[0].Command.Shell)
	}
	if got.Panels[0].Command.Shell != "go test ./..." {
		t.Fatalf("Panels[0].Command.Shell = %q", got.Panels[0].Command.Shell)
	}
	if got.Panels[0].KillCommand.Shell != "pkill -f muxedo" {
		t.Fatalf("Panels[0].KillCommand.Shell = %q", got.Panels[0].KillCommand.Shell)
	}
}

func TestLoadProfileRejectsMixedCommandModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte(`
[panel.api]
workingdir = "."
program = "go"
args = ["test"]
shell = "go test"
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), `panel "api": specify exactly one of program or shell`) {
		t.Fatalf("Load() error = %q", err)
	}
}

func TestLoadProfileRejectsLegacyCmdFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte(`
[panel.api]
workingdir = "."
cmd = "go test ./..."
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "legacy cmd/cmd_kill fields") {
		t.Fatalf("Load() error = %q", err)
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
