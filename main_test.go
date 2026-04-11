package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProfilePathUsesExplicitFlagValue(t *testing.T) {
	tempDir := t.TempDir()
	explicit := filepath.Join(tempDir, "profile.toml")
	if err := os.WriteFile(filepath.Join(tempDir, ".muxedo"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := withWorkingDir(tempDir, func() (string, error) {
		return resolveProfilePath(explicit)
	})
	if err != nil {
		t.Fatalf("resolveProfilePath() error = %v", err)
	}
	if got != explicit {
		t.Fatalf("resolveProfilePath() = %q, want %q", got, explicit)
	}
}

func TestResolveProfilePathUsesDotMuxedoInWorkingDirectory(t *testing.T) {
	tempDir := t.TempDir()
	want := filepath.Join(tempDir, ".muxedo")
	if err := os.WriteFile(want, []byte("profile"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := withWorkingDir(tempDir, func() (string, error) {
		return resolveProfilePath("")
	})
	if err != nil {
		t.Fatalf("resolveProfilePath() error = %v", err)
	}
	if got != want {
		t.Fatalf("resolveProfilePath() = %q, want %q", got, want)
	}
}

func TestResolveProfilePathRequiresFlagWhenDotMuxedoMissing(t *testing.T) {
	tempDir := t.TempDir()

	_, err := withWorkingDir(tempDir, func() (string, error) {
		return resolveProfilePath("")
	})
	if err == nil {
		t.Fatal("resolveProfilePath() error = nil, want error")
	}
	if err.Error() != "-profile is required" {
		t.Fatalf("resolveProfilePath() error = %q, want %q", err, "-profile is required")
	}
}

func withWorkingDir[T any](dir string, fn func() (T, error)) (T, error) {
	original, err := os.Getwd()
	if err != nil {
		var zero T
		return zero, err
	}

	if err := os.Chdir(dir); err != nil {
		var zero T
		return zero, err
	}
	defer func() {
		_ = os.Chdir(original)
	}()

	return fn()
}
