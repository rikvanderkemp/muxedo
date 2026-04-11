package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"muxedo/internal/update"
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

func TestRunUpdateCheckDoesNotRequireProfile(t *testing.T) {
	original := newUpdater
	defer func() { newUpdater = original }()

	newUpdater = func() updaterAPI {
		return updaterStub{check: func(currentVersion string) (update.CheckResult, error) {
			return update.CheckResult{
				CurrentVersion:  currentVersion,
				LatestVersion:   "v1.2.4",
				UpdateAvailable: true,
			}, nil
		}}
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"update", "check"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("run(update check) exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("run(update check) stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "update available") {
		t.Fatalf("run(update check) stdout = %q", stdout.String())
	}
}

func TestRunUpdateApplyPrintsRestartMessage(t *testing.T) {
	original := newUpdater
	defer func() { newUpdater = original }()

	newUpdater = func() updaterAPI {
		return updaterStub{apply: func(currentVersion string, executablePath string) (update.ApplyResult, error) {
			return update.ApplyResult{
				PreviousVersion: currentVersion,
				Version:         "v1.2.4",
			}, nil
		}}
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"update", "apply"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("run(update apply) exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "restart muxedo") {
		t.Fatalf("run(update apply) stdout = %q", stdout.String())
	}
}

type updaterStub struct {
	check func(string) (update.CheckResult, error)
	apply func(string, string) (update.ApplyResult, error)
}

func (u updaterStub) Check(version string) (update.CheckResult, error) {
	if u.check == nil {
		return update.CheckResult{}, nil
	}
	return u.check(version)
}

func (u updaterStub) Apply(version string, executablePath string) (update.ApplyResult, error) {
	if u.apply == nil {
		return update.ApplyResult{}, nil
	}
	return u.apply(version, executablePath)
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
