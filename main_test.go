// SPDX-License-Identifier: MIT
package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rikvanderkemp/muxedo/internal/config"
	"github.com/rikvanderkemp/muxedo/internal/update"
)

func TestResolveProfilePath(t *testing.T) {
	tests := []struct {
		name         string
		useExplicit  bool
		writeDotFile bool
		wantDefault  bool
		wantErr      string
	}{
		{
			name:        "uses explicit flag value",
			useExplicit: true,
		},
		{
			name:         "uses dot muxedo in working directory",
			writeDotFile: true,
			wantDefault:  true,
		},
		{
			name:    "requires flag when dot muxedo missing",
			wantErr: "no profile found: pass -profile or run in an interactive terminal to use the first-run wizard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			explicit := filepath.Join(dir, "profile.toml")
			dotMuxedo := filepath.Join(dir, ".muxedo")

			if tt.writeDotFile {
				if err := os.WriteFile(dotMuxedo, []byte("profile"), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			}

			var flagValue string
			if tt.useExplicit {
				flagValue = explicit
			}

			got, err := withWorkingDir(dir, func() (string, error) {
				return resolveProfilePath(flagValue)
			})
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("resolveProfilePath() error = nil, want error")
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("resolveProfilePath() error = %q, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveProfilePath() error = %v", err)
			}

			want := explicit
			if tt.wantDefault {
				want = dotMuxedo
			}
			if got != want {
				t.Fatalf("resolveProfilePath() = %q, want %q", got, want)
			}
		})
	}
}

func TestRunUpdateCheckDoesNotRequireProfile(t *testing.T) {
	original := newUpdater
	t.Cleanup(func() { newUpdater = original })

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
	t.Cleanup(func() { newUpdater = original })

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

func TestRunStartupSkipsUpdateWhenConfigDisabled(t *testing.T) {
	restore := stubStartupEnv(t)
	defer restore()

	tempDir := t.TempDir()
	writeProfile(t, tempDir)
	writeConfig(t, tempDir, "[ui]\ncheck_updates_on_start = false\n")

	called := false
	newUpdater = func() updaterAPI {
		called = true
		return updaterStub{}
	}

	result := withWorkingDirValue(t, tempDir, func() runResult {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := run(nil, &stdout, &stderr)
		return runResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
	})

	if result.exitCode != 0 {
		t.Fatalf("run(nil) exitCode = %d, stderr = %q", result.exitCode, result.stderr)
	}
	if called {
		t.Fatal("newUpdater() called, want skipped when config disables startup checks")
	}
}

func TestRunStartupContinuesWhenNoUpdateAvailable(t *testing.T) {
	restore := stubStartupEnv(t)
	defer restore()

	tempDir := t.TempDir()
	writeProfile(t, tempDir)
	writeConfig(t, tempDir, "")

	checkCalls := 0
	newUpdater = func() updaterAPI {
		return updaterStub{
			check: func(currentVersion string) (update.CheckResult, error) {
				checkCalls++
				return update.CheckResult{
					CurrentVersion:  currentVersion,
					LatestVersion:   currentVersion,
					UpdateAvailable: false,
				}, nil
			},
		}
	}

	result := withWorkingDirValue(t, tempDir, func() runResult {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := run(nil, &stdout, &stderr)
		return runResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
	})

	if result.exitCode != 0 {
		t.Fatalf("run(nil) exitCode = %d, stderr = %q", result.exitCode, result.stderr)
	}
	if checkCalls != 1 {
		t.Fatalf("checkCalls = %d, want 1", checkCalls)
	}
}

func TestRunStartupPromptsAndSkipsWhenUserDeclines(t *testing.T) {
	restore := stubStartupEnv(t)
	defer restore()

	tempDir := t.TempDir()
	writeProfile(t, tempDir)
	writeConfig(t, tempDir, "")

	promptInput = strings.NewReader("n\n")
	var prompt bytes.Buffer
	promptOutput = &prompt

	checkCalls := 0
	applyCalls := 0
	newUpdater = func() updaterAPI {
		return updaterStub{
			check: func(currentVersion string) (update.CheckResult, error) {
				checkCalls++
				return update.CheckResult{
					CurrentVersion:  currentVersion,
					LatestVersion:   "v9.9.9",
					UpdateAvailable: true,
				}, nil
			},
			apply: func(string, string) (update.ApplyResult, error) {
				applyCalls++
				return update.ApplyResult{}, nil
			},
		}
	}

	result := withWorkingDirValue(t, tempDir, func() runResult {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := run(nil, &stdout, &stderr)
		return runResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
	})

	if result.exitCode != 0 {
		t.Fatalf("run(nil) exitCode = %d, stderr = %q", result.exitCode, result.stderr)
	}
	if checkCalls != 1 {
		t.Fatalf("checkCalls = %d, want 1", checkCalls)
	}
	if applyCalls != 0 {
		t.Fatalf("applyCalls = %d, want 0", applyCalls)
	}
	if !strings.Contains(prompt.String(), "Apply update now? [Y/n]") {
		t.Fatalf("prompt = %q, want update confirmation", prompt.String())
	}
}

func TestRunStartupAppliesUpdateAndExecsSelf(t *testing.T) {
	restore := stubStartupEnv(t)
	defer restore()

	tempDir := t.TempDir()
	writeProfile(t, tempDir)
	writeConfig(t, tempDir, "")

	promptInput = strings.NewReader("y\n")
	promptOutput = io.Discard

	applyCalls := 0
	execCalls := 0
	newUpdater = func() updaterAPI {
		return updaterStub{
			check: func(currentVersion string) (update.CheckResult, error) {
				return update.CheckResult{
					CurrentVersion:  currentVersion,
					LatestVersion:   "v9.9.9",
					UpdateAvailable: true,
				}, nil
			},
			apply: func(currentVersion string, executablePath string) (update.ApplyResult, error) {
				applyCalls++
				if executablePath == "" {
					t.Fatal("Apply() executablePath empty")
				}
				return update.ApplyResult{
					PreviousVersion: currentVersion,
					Version:         "v9.9.9",
				}, nil
			},
		}
	}
	execSelf = func(executablePath string, args []string) error {
		execCalls++
		if executablePath == "" {
			t.Fatal("execSelf() executablePath empty")
		}
		if len(args) != 0 {
			t.Fatalf("execSelf() args = %#v, want nil", args)
		}
		return nil
	}

	result := withWorkingDirValue(t, tempDir, func() runResult {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := run(nil, &stdout, &stderr)
		return runResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
	})

	if result.exitCode != 0 {
		t.Fatalf("run(nil) exitCode = %d, stderr = %q", result.exitCode, result.stderr)
	}
	if applyCalls != 1 {
		t.Fatalf("applyCalls = %d, want 1", applyCalls)
	}
	if execCalls != 1 {
		t.Fatalf("execCalls = %d, want 1", execCalls)
	}
}

func TestRunStartupWarnsAndContinuesOnCheckError(t *testing.T) {
	restore := stubStartupEnv(t)
	defer restore()

	tempDir := t.TempDir()
	writeProfile(t, tempDir)
	writeConfig(t, tempDir, "")

	newUpdater = func() updaterAPI {
		return updaterStub{
			check: func(string) (update.CheckResult, error) {
				return update.CheckResult{}, os.ErrPermission
			},
		}
	}

	result := withWorkingDirValue(t, tempDir, func() runResult {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := run(nil, &stdout, &stderr)
		return runResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
	})

	if result.exitCode != 0 {
		t.Fatalf("run(nil) exitCode = %d, stderr = %q", result.exitCode, result.stderr)
	}
	if !strings.Contains(result.stderr, "warning: startup update check failed") {
		t.Fatalf("stderr = %q, want startup update warning", result.stderr)
	}
}

func TestRunStartupSkipsPromptWhenNonInteractive(t *testing.T) {
	restore := stubStartupEnv(t)
	defer restore()

	tempDir := t.TempDir()
	writeProfile(t, tempDir)
	writeConfig(t, tempDir, "")

	isInteractiveTTY = func(io.Reader, io.Writer) bool { return false }
	promptOutput = io.Discard

	promptInput = strings.NewReader("y\n")
	applyCalls := 0
	newUpdater = func() updaterAPI {
		return updaterStub{
			check: func(currentVersion string) (update.CheckResult, error) {
				return update.CheckResult{
					CurrentVersion:  currentVersion,
					LatestVersion:   "v9.9.9",
					UpdateAvailable: true,
				}, nil
			},
			apply: func(string, string) (update.ApplyResult, error) {
				applyCalls++
				return update.ApplyResult{}, nil
			},
		}
	}

	result := withWorkingDirValue(t, tempDir, func() runResult {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := run(nil, &stdout, &stderr)
		return runResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
	})

	if result.exitCode != 0 {
		t.Fatalf("run(nil) exitCode = %d, stderr = %q", result.exitCode, result.stderr)
	}
	if applyCalls != 0 {
		t.Fatalf("applyCalls = %d, want 0", applyCalls)
	}
	if !strings.Contains(result.stderr, "skipping prompt in non-interactive session") {
		t.Fatalf("stderr = %q, want non-interactive warning", result.stderr)
	}
}

func TestRunHelpPrintsCommands(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"-help"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("run(-help) exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Commands:") {
		t.Fatalf("run(-help) stderr = %q, want commands section", stderr.String())
	}
	if !strings.Contains(stderr.String(), "update apply") {
		t.Fatalf("run(-help) stderr = %q, want update subcommands", stderr.String())
	}
}

func TestRunWithoutProfilePrintsUsage(t *testing.T) {
	originalTTY := isInteractiveTTY
	defer func() { isInteractiveTTY = originalTTY }()
	isInteractiveTTY = func(io.Reader, io.Writer) bool { return false }

	tempDir := t.TempDir()

	result := withWorkingDirValue(t, tempDir, func() runResult {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := run(nil, &stdout, &stderr)
		return runResult{
			stdout:   stdout.String(),
			stderr:   stderr.String(),
			exitCode: exitCode,
		}
	})

	if result.exitCode != 1 {
		t.Fatalf("run(nil) exitCode = %d, want 1", result.exitCode)
	}
	if !strings.Contains(result.stderr, "no profile found") {
		t.Fatalf("run(nil) stderr = %q, want missing profile error", result.stderr)
	}
	if !strings.Contains(result.stderr, "Commands:") {
		t.Fatalf("run(nil) stderr = %q, want usage with commands", result.stderr)
	}
}

func TestRunWithoutProfileSkipsWizardInNonInteractiveSession(t *testing.T) {
	originalTTY := isInteractiveTTY
	defer func() { isInteractiveTTY = originalTTY }()
	isInteractiveTTY = func(io.Reader, io.Writer) bool { return false }

	originalRunWizard := runWizard
	defer func() { runWizard = originalRunWizard }()
	runWizard = func(tea.Model) (tea.Model, error) {
		t.Errorf("runWizard invoked in non-interactive session")
		return nil, nil
	}

	tempDir := t.TempDir()
	result := withWorkingDirValue(t, tempDir, func() runResult {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := run(nil, &stdout, &stderr)
		return runResult{
			stdout:   stdout.String(),
			stderr:   stderr.String(),
			exitCode: exitCode,
		}
	})

	if result.exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", result.exitCode)
	}
	if !strings.Contains(result.stderr, "no profile found") {
		t.Fatalf("stderr = %q, want missing profile error", result.stderr)
	}
}

func TestRunWithoutProfileLaunchesWizardWhenInteractiveAndContinues(t *testing.T) {
	restore := stubStartupEnv(t)
	defer restore()

	originalLaunch := launchWelcomeWizardFn
	t.Cleanup(func() { launchWelcomeWizardFn = originalLaunch })

	tempDir := t.TempDir()
	writeConfig(t, tempDir, "")

	wizardProfilePath := filepath.Join(tempDir, "wizard.muxedo")
	if err := os.WriteFile(wizardProfilePath, []byte("workingdir = \".\"\n\n[panel.test]\nshell = \"printf ok\\n\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(wizard profile) error = %v", err)
	}

	launchCalls := 0
	launchWelcomeWizardFn = func(config.Config, io.Writer) (string, error) {
		launchCalls++
		return wizardProfilePath, nil
	}

	result := withWorkingDirValue(t, tempDir, func() runResult {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := run(nil, &stdout, &stderr)
		return runResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
	})

	if result.exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", result.exitCode, result.stderr)
	}
	if launchCalls != 1 {
		t.Fatalf("launchCalls = %d, want 1", launchCalls)
	}
	if result.stderr != "" {
		t.Fatalf("stderr = %q, want empty", result.stderr)
	}
}

func TestRunWithoutProfileLaunchesWizardWhenInteractiveAndAborts(t *testing.T) {
	restore := stubStartupEnv(t)
	defer restore()

	originalLaunch := launchWelcomeWizardFn
	t.Cleanup(func() { launchWelcomeWizardFn = originalLaunch })

	tempDir := t.TempDir()
	writeConfig(t, tempDir, "")

	launchCalls := 0
	launchWelcomeWizardFn = func(config.Config, io.Writer) (string, error) {
		launchCalls++
		return "", nil
	}

	result := withWorkingDirValue(t, tempDir, func() runResult {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := run(nil, &stdout, &stderr)
		return runResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
	})

	if result.exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", result.exitCode, result.stderr)
	}
	if launchCalls != 1 {
		t.Fatalf("launchCalls = %d, want 1", launchCalls)
	}
}

func TestRunWithoutProfileLaunchesWizardWhenInteractiveAndErrors(t *testing.T) {
	restore := stubStartupEnv(t)
	defer restore()

	originalLaunch := launchWelcomeWizardFn
	t.Cleanup(func() { launchWelcomeWizardFn = originalLaunch })

	tempDir := t.TempDir()
	writeConfig(t, tempDir, "")

	launchWelcomeWizardFn = func(config.Config, io.Writer) (string, error) {
		return "", os.ErrPermission
	}

	result := withWorkingDirValue(t, tempDir, func() runResult {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := run(nil, &stdout, &stderr)
		return runResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
	})

	if result.exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", result.exitCode)
	}
	if !strings.Contains(result.stderr, "error:") {
		t.Fatalf("stderr = %q, want error prefix", result.stderr)
	}
}

func TestPrintExitMessage(t *testing.T) {
	var stdout bytes.Buffer

	printExitMessage(&stdout)

	got := stdout.String()
	if !strings.Contains(got, "Thanks for using muxedo.") {
		t.Fatalf("printExitMessage() = %q, want thank-you message", got)
	}
	if !strings.Contains(got, "https://buymeacoffee.com/rikvanderkemp") {
		t.Fatalf("printExitMessage() = %q, want support link", got)
	}
	if !strings.Contains(got, "[ui] show_exit_message = false") {
		t.Fatalf("printExitMessage() = %q, want disable hint", got)
	}
}

func TestPrintExitMessageDisabledConfigSkipsOutput(t *testing.T) {
	var stdout bytes.Buffer

	cfg := config.Config{
		UI: config.UIConfig{
			ShowExitMessage: boolPtr(false),
		},
	}
	if cfg.ExitMessageEnabled() {
		printExitMessage(&stdout)
	}

	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty when exit message disabled", stdout.String())
	}
}

type runResult struct {
	stdout   string
	stderr   string
	exitCode int
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

// withWorkingDirValue temporarily chdirs into dir, runs fn, and restores cwd
// via t.Cleanup. Tests that use this helper (and stubStartupEnv, which swaps
// package-level globals) must NOT call t.Parallel: Chdir and the shared vars
// are process-global and would race across parallel subtests.
//
// t.Cleanup fires LIFO, so the cwd restore runs before any t.TempDir cleanup
// the caller registered, which prevents RemoveAll from tripping over a cwd
// still inside the doomed directory.
func withWorkingDirValue[T any](t *testing.T, dir string, fn func() T) T {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s) error = %v", dir, err)
	}
	t.Cleanup(func() {
		// t.Errorf (not Fatalf): cleanup must not call FailNow.
		if err := os.Chdir(original); err != nil {
			t.Errorf("restoring cwd to %s: %v", original, err)
		}
	})

	return fn()
}

func boolPtr(v bool) *bool {
	return &v
}

func stubStartupEnv(t *testing.T) func() {
	t.Helper()

	originalVersion := version
	originalNewUpdater := newUpdater
	originalNewStartupUpdater := newStartupUpdater
	originalRunProgram := runProgram
	originalPromptInput := promptInput
	originalPromptOutput := promptOutput
	originalTTY := isInteractiveTTY
	originalExecSelf := execSelf

	version = "v1.2.3"
	newStartupUpdater = func() updaterAPI { return newUpdater() }
	runProgram = func(tea.Model) error { return nil }
	promptInput = strings.NewReader("\n")
	promptOutput = io.Discard
	isInteractiveTTY = func(io.Reader, io.Writer) bool { return true }
	execSelf = func(string, []string) error { return nil }

	return func() {
		version = originalVersion
		newUpdater = originalNewUpdater
		newStartupUpdater = originalNewStartupUpdater
		runProgram = originalRunProgram
		promptInput = originalPromptInput
		promptOutput = originalPromptOutput
		isInteractiveTTY = originalTTY
		execSelf = originalExecSelf
	}
}

func writeProfile(t *testing.T, dir string) {
	t.Helper()
	data := "workingdir = \".\"\n\n[panel.test]\nshell = \"printf ok\\n\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".muxedo"), []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile(profile) error = %v", err)
	}
}

func writeConfig(t *testing.T, home string, body string) {
	t.Helper()
	path := filepath.Join(home, ".config", "muxedo", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(config) error = %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	t.Setenv("HOME", home)
}
