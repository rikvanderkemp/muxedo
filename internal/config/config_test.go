// SPDX-License-Identifier: MIT
package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadMissingConfigReturnsDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg != (Config{}) {
		t.Fatalf("Load() = %#v, want zero-value config", cfg)
	}
}

func TestLoadWithoutHomeReturnsDefaults(t *testing.T) {
	t.Setenv("HOME", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg != (Config{}) {
		t.Fatalf("Load() = %#v, want zero-value config", cfg)
	}
}

func TestLoadParsesPresentConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".config", "muxedo", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg != (Config{}) {
		t.Fatalf("Load() = %#v, want zero-value config", cfg)
	}
}

func TestLoadParsesThemeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".config", "muxedo", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`
[theme]
inactive_border = "#5f87af"
status_mode_normal_bg = "208"
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Theme.InactiveBorder != "#5f87af" {
		t.Fatalf("Theme.InactiveBorder = %q", cfg.Theme.InactiveBorder)
	}
	if cfg.Theme.StatusModeNormalBG != "208" {
		t.Fatalf("Theme.StatusModeNormalBG = %q", cfg.Theme.StatusModeNormalBG)
	}
	if cfg.Theme.StatusBarBG != "" {
		t.Fatalf("Theme.StatusBarBG = %q, want empty for unspecified field", cfg.Theme.StatusBarBG)
	}
}

func TestLoadParsesUIConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".config", "muxedo", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`
[ui]
show_exit_message = false
check_updates_on_start = false
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.UI.ShowExitMessage == nil {
		t.Fatal("UI.ShowExitMessage = nil, want parsed false")
	}
	if cfg.ExitMessageEnabled() {
		t.Fatal("ExitMessageEnabled() = true, want false")
	}
	if cfg.UI.CheckUpdatesOnStart == nil {
		t.Fatal("UI.CheckUpdatesOnStart = nil, want parsed false")
	}
	if cfg.CheckUpdatesOnStartEnabled() {
		t.Fatal("CheckUpdatesOnStartEnabled() = true, want false")
	}
}

func TestFeatureFlagsDefaultTrueWhenUnset(t *testing.T) {
	cfg := Config{}

	tests := []struct {
		name string
		got  func() bool
	}{
		{
			name: "exit message",
			got:  cfg.ExitMessageEnabled,
		},
		{
			name: "startup update check",
			got:  cfg.CheckUpdatesOnStartEnabled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.got() {
				t.Fatalf("%s = false, want true when unset", tt.name)
			}
		})
	}
}

func TestWriteDefaultCreatesConfigAndDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := WriteDefault(false)
	if err != nil {
		t.Fatalf("WriteDefault() error = %v", err)
	}

	wantPath := filepath.Join(home, ".config", "muxedo", "config.toml")
	if path != wantPath {
		t.Fatalf("WriteDefault() path = %q, want %q", path, wantPath)
	}

	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("Stat(config dir) error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("WriteDefault() wrote empty file")
	}
	if string(data) == "" || !containsAll(string(data), "[ui]", "show_exit_message = true", "check_updates_on_start = true") {
		t.Fatalf("WriteDefault() data = %q, want default ui config", string(data))
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("Load() = %#v, want %#v", cfg, Default())
	}
}

func TestWriteDefaultRefusesOverwriteWithoutForce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".config", "muxedo", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := WriteDefault(false)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("WriteDefault() error = %v, want os.ErrExist", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "existing" {
		t.Fatalf("file contents = %q, want existing", string(data))
	}
}

func TestWriteDefaultOverwritesWithForce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".config", "muxedo", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := WriteDefault(true); err != nil {
		t.Fatalf("WriteDefault(true) error = %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("Load() = %#v, want %#v", cfg, Default())
	}
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}
