// SPDX-License-Identifier: MIT
package welcome

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rikvanderkemp/muxedo/internal/profile"
	"github.com/rikvanderkemp/muxedo/internal/ui"
)

func typeKeys(t *testing.T, m tea.Model, s string) tea.Model {
	t.Helper()
	for _, r := range s {
		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func sendKey(t *testing.T, m tea.Model, key tea.KeyMsg) tea.Model {
	t.Helper()
	next, _ := m.Update(key)
	return next
}

func pressEnter(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	return sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
}

func pressEsc(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	return sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
}

func TestWizardHappyPathWritesLoadableProfile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	m := tea.Model(New(tmp, ui.DefaultTheme()))

	m = pressEnter(t, m)

	m = typeKeys(t, m, "My Profile")
	m = pressEnter(t, m)

	m = pressEnter(t, m)

	m = typeKeys(t, m, "sleep 2")
	m = pressEnter(t, m)

	m = pressEnter(t, m)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	m = typeKeys(t, m, "Dev Server")
	m = pressEnter(t, m)

	m = pressEnter(t, m)

	m = typeKeys(t, m, "npm run dev -f --arguments=2123")
	m = pressEnter(t, m)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	m = pressEnter(t, m)

	m = pressEnter(t, m)

	wizard, ok := m.(Model)
	if !ok {
		t.Fatalf("model type = %T, want Model", m)
	}
	if wizard.aborted {
		t.Fatalf("wizard aborted unexpectedly")
	}
	if wizard.step != stepSaved {
		t.Fatalf("final step = %v, want stepSaved", wizard.step)
	}
	if wizard.savedPath == "" {
		t.Fatalf("savedPath empty after save")
	}

	wantPath := filepath.Join(tmp, DefaultSaveFile)
	if wizard.savedPath != wantPath {
		t.Fatalf("savedPath = %q, want %q", wizard.savedPath, wantPath)
	}

	loaded, err := profile.Load(wizard.savedPath)
	if err != nil {
		raw, _ := os.ReadFile(wizard.savedPath)
		t.Fatalf("profile.Load: %v\nfile:\n%s", err, raw)
	}

	if loaded.Title != "My Profile" {
		t.Fatalf("title = %q, want %q", loaded.Title, "My Profile")
	}
	if len(loaded.Startup) != 1 {
		t.Fatalf("startup count = %d, want 1", len(loaded.Startup))
	}
	if loaded.Startup[0].Command.Program != "sleep" {
		t.Fatalf("startup program = %q, want sleep", loaded.Startup[0].Command.Program)
	}
	if !reflect.DeepEqual(loaded.Startup[0].Command.Args, []string{"2"}) {
		t.Fatalf("startup args = %v, want [2]", loaded.Startup[0].Command.Args)
	}
	if loaded.Startup[0].Mode != profile.StartupModeAsync {
		t.Fatalf("startup mode = %q, want async", loaded.Startup[0].Mode)
	}

	if len(loaded.Panels) != 1 {
		t.Fatalf("panel count = %d, want 1", len(loaded.Panels))
	}
	panel := loaded.Panels[0]
	if panel.Name != "dev-server" {
		t.Fatalf("panel name = %q, want dev-server", panel.Name)
	}
	if panel.Command.Program != "npm" {
		t.Fatalf("panel program = %q, want npm", panel.Command.Program)
	}
	wantArgs := []string{"run", "dev", "-f", "--arguments=2123"}
	if !reflect.DeepEqual(panel.Command.Args, wantArgs) {
		t.Fatalf("panel args = %v, want %v", panel.Command.Args, wantArgs)
	}
}

func TestWizardSmallBehaviors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		drive  func(t *testing.T, m tea.Model) tea.Model
		assert func(t *testing.T, w Model)
	}{
		{
			name: "abort on esc from intro",
			drive: func(t *testing.T, m tea.Model) tea.Model {
				return pressEsc(t, m)
			},
			assert: func(t *testing.T, w Model) {
				if !w.aborted {
					t.Fatalf("aborted = false, want true")
				}
				if w.savedPath != "" {
					t.Fatalf("savedPath = %q, want empty", w.savedPath)
				}
			},
		},
		{
			name: "abort on ctrl-c mid-entry",
			drive: func(t *testing.T, m tea.Model) tea.Model {
				m = pressEnter(t, m)
				m = typeKeys(t, m, "Half way")
				return sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
			},
			assert: func(t *testing.T, w Model) {
				if !w.aborted {
					t.Fatalf("aborted = false, want true")
				}
			},
		},
		{
			name: "rejects empty panel name",
			drive: func(t *testing.T, m tea.Model) tea.Model {
				// intro -> title (random default) -> workingDir (default ".")
				// -> startupCmd (empty skips to panelName) -> panelName (empty)
				m = pressEnter(t, m)
				m = pressEnter(t, m)
				m = pressEnter(t, m)
				m = pressEnter(t, m)
				return pressEnter(t, m)
			},
			assert: func(t *testing.T, w Model) {
				if w.step != stepPanelName {
					t.Fatalf("step = %v, want stepPanelName", w.step)
				}
				if !strings.Contains(w.errMsg, "panel name is required") {
					t.Fatalf("errMsg = %q, want required error", w.errMsg)
				}
			},
		},
		{
			name: "rejects duplicate panel slug",
			drive: func(t *testing.T, m tea.Model) tea.Model {
				m = pressEnter(t, m)
				m = pressEnter(t, m)
				m = pressEnter(t, m)
				m = pressEnter(t, m)

				m = typeKeys(t, m, "Dev")
				m = pressEnter(t, m)
				m = pressEnter(t, m)
				m = typeKeys(t, m, "ls")
				m = pressEnter(t, m)

				m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

				m = typeKeys(t, m, "dev")
				return pressEnter(t, m)
			},
			assert: func(t *testing.T, w Model) {
				if w.step != stepPanelName {
					t.Fatalf("step = %v, want stepPanelName after duplicate", w.step)
				}
				if !strings.Contains(w.errMsg, "already used") {
					t.Fatalf("errMsg = %q, want dedupe error", w.errMsg)
				}
			},
		},
		{
			name: "startup mode toggles with tab",
			drive: func(t *testing.T, m tea.Model) tea.Model {
				m = pressEnter(t, m)
				m = pressEnter(t, m)
				m = pressEnter(t, m)

				m = typeKeys(t, m, "sleep 1")
				m = pressEnter(t, m)

				m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
				w := m.(Model)
				if w.modeChoice != profile.StartupModeSync {
					t.Fatalf("mode = %q after tab, want sync", w.modeChoice)
				}
				return sendKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
			},
			assert: func(t *testing.T, w Model) {
				if w.modeChoice != profile.StartupModeAsync {
					t.Fatalf("mode = %q after second tab, want async", w.modeChoice)
				}
			},
		},
		{
			name: "backspace on empty field goes back",
			drive: func(t *testing.T, m tea.Model) tea.Model {
				m = pressEnter(t, m)
				m = typeKeys(t, m, "Foo")
				m = pressEnter(t, m)

				w := m.(Model)
				if w.step != stepWorkingDir {
					t.Fatalf("setup: step = %v, want stepWorkingDir", w.step)
				}
				return sendKey(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
			},
			assert: func(t *testing.T, w Model) {
				if w.step != stepTitle {
					t.Fatalf("step after backspace = %v, want stepTitle", w.step)
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmp := t.TempDir()
			m := tea.Model(New(tmp, ui.DefaultTheme()))
			m = tc.drive(t, m)

			wizard, ok := m.(Model)
			if !ok {
				t.Fatalf("model type = %T, want Model", m)
			}
			tc.assert(t, wizard)
		})
	}
}

func TestWizardPanelWorkingDirOverride(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	override := filepath.Join(tmp, "sub")
	if err := os.MkdirAll(override, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	m := tea.Model(New(tmp, ui.DefaultTheme()))

	m = pressEnter(t, m)
	m = typeKeys(t, m, "Overrides")
	m = pressEnter(t, m)

	m = typeKeys(t, m, tmp)
	m = pressEnter(t, m)

	m = pressEnter(t, m)

	m = typeKeys(t, m, "inherits")
	m = pressEnter(t, m)
	m = pressEnter(t, m)
	m = typeKeys(t, m, "ls")
	m = pressEnter(t, m)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	m = typeKeys(t, m, "custom")
	m = pressEnter(t, m)
	m = typeKeys(t, m, override)
	m = pressEnter(t, m)
	m = typeKeys(t, m, "ls")
	m = pressEnter(t, m)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	m = pressEnter(t, m)

	m = pressEnter(t, m)

	wizard := m.(Model)
	if wizard.step != stepSaved {
		t.Fatalf("step = %v, want stepSaved", wizard.step)
	}

	loaded, err := profile.Load(wizard.savedPath)
	if err != nil {
		raw, _ := os.ReadFile(wizard.savedPath)
		t.Fatalf("profile.Load: %v\nfile:\n%s", err, raw)
	}

	if len(loaded.Panels) != 2 {
		t.Fatalf("panel count = %d, want 2", len(loaded.Panels))
	}

	byName := map[string]string{}
	for _, p := range loaded.Panels {
		byName[p.Name] = p.WorkingDir
	}

	wantInherited, err := filepath.Abs(tmp)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if got := byName["inherits"]; got != wantInherited {
		t.Fatalf("inherits workingdir = %q, want %q", got, wantInherited)
	}

	wantOverride, err := filepath.Abs(override)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if got := byName["custom"]; got != wantOverride {
		t.Fatalf("custom workingdir = %q, want %q", got, wantOverride)
	}
}

func TestWizardRefusesToOverwriteExistingProfile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	existing := filepath.Join(tmp, DefaultSaveFile)
	original := []byte("title = \"existing\"\nworkingdir = \".\"\n\n[panel.keep]\nprogram = \"echo\"\nargs = [\"ok\"]\n")
	if err := os.WriteFile(existing, original, 0o644); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	m := tea.Model(New(tmp, ui.DefaultTheme()))

	m = pressEnter(t, m) // intro -> title
	m = pressEnter(t, m) // accept random title
	m = pressEnter(t, m) // accept working dir
	m = pressEnter(t, m) // skip startup cmd

	m = typeKeys(t, m, "dev")
	m = pressEnter(t, m)
	m = pressEnter(t, m)
	m = typeKeys(t, m, "ls")
	m = pressEnter(t, m)

	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	m = pressEnter(t, m) // save path (default)
	m = pressEnter(t, m) // confirm (should fail due to existing)

	w := m.(Model)
	if w.step != stepConfirm {
		t.Fatalf("step = %v, want stepConfirm after overwrite refusal", w.step)
	}
	if w.savedPath != existing {
		t.Fatalf("savedPath = %q, want %q", w.savedPath, existing)
	}
	if !strings.Contains(w.errMsg, "refusing to overwrite existing profile") {
		t.Fatalf("errMsg = %q, want overwrite refusal", w.errMsg)
	}

	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read existing: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("existing file changed:\n%s\nwant:\n%s", got, original)
	}
}

func TestWizardWorkingDirTabCompletesSingleMatch(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	m := tea.Model(New(tmp, ui.DefaultTheme()))
	m = pressEnter(t, m) // intro -> title
	m = pressEnter(t, m) // accept random title -> working dir

	m = typeKeys(t, m, "a")
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyTab})

	w := m.(Model)
	if w.step != stepWorkingDir {
		t.Fatalf("step = %v, want stepWorkingDir", w.step)
	}
	if got := w.input.Value(); got != "alpha/" {
		t.Fatalf("value after tab = %q, want %q", got, "alpha/")
	}
}

func TestWizardWorkingDirTabExtendsToLongestCommonPrefix(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "api"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "app"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	m := tea.Model(New(tmp, ui.DefaultTheme()))
	m = pressEnter(t, m)
	m = pressEnter(t, m)

	m = typeKeys(t, m, "a")
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyTab})

	w := m.(Model)
	if got := w.input.Value(); got != "ap" {
		t.Fatalf("value after first tab = %q, want %q", got, "ap")
	}
	if w.dirPickOpen {
		t.Fatalf("picker open after LCP extension, want closed")
	}
}

func TestWizardWorkingDirSecondTabOpensPicker(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "api"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "app"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	m := tea.Model(New(tmp, ui.DefaultTheme()))
	m = pressEnter(t, m)
	m = pressEnter(t, m)

	m = typeKeys(t, m, "a")
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyTab})

	w := m.(Model)
	if !w.dirPickOpen {
		t.Fatalf("picker not open after second tab")
	}
	if len(w.dirMatches) != 2 {
		t.Fatalf("dirMatches = %v, want 2 entries", w.dirMatches)
	}
}

func TestWizardWorkingDirPickerDownArrowAndTabAccepts(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "api"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "app"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	m := tea.Model(New(tmp, ui.DefaultTheme()))
	m = pressEnter(t, m)
	m = pressEnter(t, m)

	m = typeKeys(t, m, "a")
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyTab})

	w := m.(Model)
	if w.dirPickOpen {
		t.Fatalf("picker still open after accept")
	}
	if got := w.input.Value(); got != "app/" {
		t.Fatalf("value = %q, want %q", got, "app/")
	}
}

func TestWizardWorkingDirPickerClosesOnTyping(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "api"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "app"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	m := tea.Model(New(tmp, ui.DefaultTheme()))
	m = pressEnter(t, m)
	m = pressEnter(t, m)

	m = typeKeys(t, m, "a")
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	w := m.(Model)
	if !w.dirPickOpen {
		t.Fatalf("setup: picker not open")
	}

	m = typeKeys(t, m, "i")
	w = m.(Model)
	if w.dirPickOpen {
		t.Fatalf("picker still open after typing")
	}
}

func TestWizardWorkingDirTabWithNoMatchSetsError(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	m := tea.Model(New(tmp, ui.DefaultTheme()))
	m = pressEnter(t, m)
	m = pressEnter(t, m)

	m = typeKeys(t, m, "nope")
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyTab})

	w := m.(Model)
	if !strings.Contains(w.errMsg, "no matching directory") {
		t.Fatalf("errMsg = %q, want no-match error", w.errMsg)
	}
	if w.dirPickOpen {
		t.Fatalf("picker opened on zero matches")
	}
}

func TestWizardWorkingDirBlocksNonExistentDir(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	m := tea.Model(New(tmp, ui.DefaultTheme()))
	m = pressEnter(t, m)
	m = pressEnter(t, m)

	m = typeKeys(t, m, "does-not-exist")
	m = pressEnter(t, m)

	w := m.(Model)
	if w.step != stepWorkingDir {
		t.Fatalf("step = %v, want stepWorkingDir", w.step)
	}
	if !strings.Contains(w.errMsg, "working directory not found") {
		t.Fatalf("errMsg = %q, want not found error", w.errMsg)
	}
}

func TestWizardPanelWorkingDirBlocksNonExistentDir(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	m := tea.Model(New(tmp, ui.DefaultTheme()))
	m = pressEnter(t, m) // intro -> title
	m = pressEnter(t, m) // title -> working dir
	m = pressEnter(t, m) // accept working dir -> startup cmd
	m = pressEnter(t, m) // skip startup -> panel name

	m = typeKeys(t, m, "dev")
	m = pressEnter(t, m) // -> panel working dir

	m = typeKeys(t, m, "missingdir")
	m = pressEnter(t, m)

	w := m.(Model)
	if w.step != stepPanelWorkingDir {
		t.Fatalf("step = %v, want stepPanelWorkingDir", w.step)
	}
	if !strings.Contains(w.errMsg, "working directory not found") {
		t.Fatalf("errMsg = %q, want not found error", w.errMsg)
	}
}

func TestExpandHomeBareTilde(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	if got := expandHome("~"); got != home {
		t.Fatalf("expandHome(~) = %q, want %q", got, home)
	}
	wantFoo := filepath.Join(home, "foo")
	if got := expandHome("~/foo"); filepath.Clean(got) != filepath.Clean(wantFoo) {
		t.Fatalf("expandHome(~/foo) = %q, want %q", got, wantFoo)
	}
}
