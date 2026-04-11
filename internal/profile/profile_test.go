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
mode = "sync"

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
	if got.Startup[0].Mode != StartupModeSync {
		t.Fatalf("Startup[0].Mode = %q, want %q", got.Startup[0].Mode, StartupModeSync)
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

func TestLoadProfileDefaultsStartupModeToAsync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte(`
[[startup]]
program = "echo"
args = ["hello"]

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
	if got.Startup[0].Mode != StartupModeAsync {
		t.Fatalf("Startup[0].Mode = %q, want %q", got.Startup[0].Mode, StartupModeAsync)
	}
}

func TestLoadProfileRejectsInvalidStartupMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte(`
[[startup]]
program = "echo"
args = ["hello"]
mode = "later"

[panel.api]
workingdir = "."
program = "go"
args = ["test", "./..."]
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), `invalid mode "later"`) {
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

func TestLoadProfilePreservesPanelDeclarationOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte(`
[panel.zeta]
workingdir = "."
program = "echo"

[panel.alpha]
workingdir = "."
program = "echo"

[panel.mid]
workingdir = "."
program = "echo"
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	names := []string{got.Panels[0].Name, got.Panels[1].Name, got.Panels[2].Name}
	if strings.Join(names, ",") != "zeta,alpha,mid" {
		t.Fatalf("panel order = %v, want [zeta alpha mid]", names)
	}
}

func TestLoadProfileMovesExplicitOrdersAheadOfDeclarationOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte(`
[panel.api]
workingdir = "."
program = "echo"

[panel.logs]
workingdir = "."
program = "echo"
order = 1

[panel.frontend]
workingdir = "."
program = "echo"
order = 0

[panel.worker]
workingdir = "."
program = "echo"
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	names := []string{got.Panels[0].Name, got.Panels[1].Name, got.Panels[2].Name, got.Panels[3].Name}
	if strings.Join(names, ",") != "frontend,logs,api,worker" {
		t.Fatalf("panel order = %v, want [frontend logs api worker]", names)
	}
}

func TestLoadProfileRejectsDuplicatePanelOrders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte(`
[panel.api]
workingdir = "."
program = "echo"
order = 0

[panel.frontend]
workingdir = "."
program = "echo"
order = 0
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "duplicate order 0") {
		t.Fatalf("Load() error = %q", err)
	}
}

func TestLoadProfileRejectsNegativePanelOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte(`
[panel.api]
workingdir = "."
program = "echo"
order = -1
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "order must be non-negative") {
		t.Fatalf("Load() error = %q", err)
	}
}
