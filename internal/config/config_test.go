package config

import (
	"os"
	"path/filepath"
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
