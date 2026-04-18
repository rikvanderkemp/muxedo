// SPDX-License-Identifier: MIT
package welcome

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rikvanderkemp/muxedo/internal/process"
	"github.com/rikvanderkemp/muxedo/internal/profile"
)

func TestSlugify(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"My Dev Server", "my-dev-server"},
		{"  weird/name!! ", "weird-name"},
		{"", "panel"},
		{"----", "panel"},
		{"npm run dev", "npm-run-dev"},
		{"Already-Slugged", "already-slugged"},
		{"__snake_case__", "snake-case"},
		{"Camel CASE 42", "camel-case-42"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got := Slugify(tc.in)
			if got != tc.want {
				t.Fatalf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseCommand(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want process.CommandSpec
	}{
		{
			name: "bare program",
			in:   "ls",
			want: process.CommandSpec{Program: "ls"},
		},
		{
			name: "flags and equals",
			in:   "npm run dev -f --arguments=2123",
			want: process.CommandSpec{
				Program: "npm",
				Args:    []string{"run", "dev", "-f", "--arguments=2123"},
			},
		},
		{
			name: "pipe triggers shell",
			in:   "tail -f log | grep err",
			want: process.CommandSpec{Shell: "tail -f log | grep err"},
		},
		{
			name: "quoted triggers shell",
			in:   `echo "hello world"`,
			want: process.CommandSpec{Shell: `echo "hello world"`},
		},
		{
			name: "redirect triggers shell",
			in:   "echo hi > file",
			want: process.CommandSpec{Shell: "echo hi > file"},
		},
		{
			name: "glob triggers shell",
			in:   "ls *.go",
			want: process.CommandSpec{Shell: "ls *.go"},
		},
		{
			name: "empty",
			in:   "   ",
			want: process.CommandSpec{},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ParseCommand(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseCommand(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestRenderTOMLLoadsBack(t *testing.T) {
	t.Parallel()

	wiz := WizardProfile{
		Title:      "My Profile",
		WorkingDir: ".",
		Startup: []Startup{
			{
				Command: process.CommandSpec{Shell: "printf 'ready\\n'"},
				Mode:    profile.StartupModeSync,
			},
			{
				Command: process.CommandSpec{Program: "sleep", Args: []string{"2"}},
				Mode:    profile.StartupModeAsync,
			},
		},
		Panels: []Panel{
			{
				Slug:    "clock",
				Command: process.CommandSpec{Shell: `while true; do date; sleep 1; done`},
			},
			{
				Slug:    "dev",
				Command: process.CommandSpec{Program: "npm", Args: []string{"run", "dev", "-f", "--arguments=2123"}},
			},
		},
	}

	out := RenderTOML(wiz)

	dir := t.TempDir()
	path := filepath.Join(dir, ".muxedo")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	loaded, err := profile.Load(path)
	if err != nil {
		t.Fatalf("profile.Load: %v\nrendered:\n%s", err, out)
	}

	if loaded.Title != "My Profile" {
		t.Fatalf("title = %q, want %q", loaded.Title, "My Profile")
	}
	if len(loaded.Startup) != 2 {
		t.Fatalf("startup count = %d, want 2", len(loaded.Startup))
	}
	if loaded.Startup[0].Mode != profile.StartupModeSync {
		t.Fatalf("startup[0].Mode = %q, want sync", loaded.Startup[0].Mode)
	}
	if loaded.Startup[0].Command.Shell == "" {
		t.Fatalf("startup[0] should be a shell command")
	}
	if loaded.Startup[1].Command.Program != "sleep" {
		t.Fatalf("startup[1].Program = %q, want sleep", loaded.Startup[1].Command.Program)
	}
	if len(loaded.Panels) != 2 {
		t.Fatalf("panel count = %d, want 2", len(loaded.Panels))
	}

	byName := map[string]process.CommandSpec{}
	for _, p := range loaded.Panels {
		byName[p.Name] = p.Command
	}
	if got := byName["clock"]; got.Shell == "" {
		t.Fatalf("clock panel missing shell: %+v", got)
	}
	if got := byName["dev"]; got.Program != "npm" || !reflect.DeepEqual(got.Args, []string{"run", "dev", "-f", "--arguments=2123"}) {
		t.Fatalf("dev panel command = %+v", got)
	}
}

func TestRenderTOMLEscapesSpecialChars(t *testing.T) {
	t.Parallel()

	wiz := WizardProfile{
		Title:      `Title with "quotes" and \backslashes\`,
		WorkingDir: ".",
		Panels: []Panel{
			{
				Slug:    "shell-panel",
				Command: process.CommandSpec{Shell: `echo "hi"` + "\t" + `\n done`},
			},
			{
				Slug:    "prog-panel",
				Command: process.CommandSpec{Program: "echo", Args: []string{`a "b"`, `c\d`}},
			},
		},
	}

	out := RenderTOML(wiz)

	dir := t.TempDir()
	path := filepath.Join(dir, ".muxedo")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	loaded, err := profile.Load(path)
	if err != nil {
		t.Fatalf("profile.Load: %v\nrendered:\n%s", err, out)
	}
	if loaded.Title != wiz.Title {
		t.Fatalf("title roundtrip: got %q want %q", loaded.Title, wiz.Title)
	}

	byName := map[string]process.CommandSpec{}
	for _, p := range loaded.Panels {
		byName[p.Name] = p.Command
	}
	if got, want := byName["prog-panel"].Args, []string{`a "b"`, `c\d`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("prog-panel args roundtrip: got %v want %v", got, want)
	}
}

func TestRenderTOMLEscapesDEL(t *testing.T) {
	t.Parallel()

	wiz := WizardProfile{
		Title:      "a\x7fb",
		WorkingDir: ".",
		Panels: []Panel{
			{
				Slug:    "dev",
				Command: process.CommandSpec{Program: "echo", Args: []string{"ok"}},
			},
		},
	}

	out := RenderTOML(wiz)

	dir := t.TempDir()
	path := filepath.Join(dir, ".muxedo")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	loaded, err := profile.Load(path)
	if err != nil {
		t.Fatalf("profile.Load: %v\nrendered:\n%s", err, out)
	}
	if loaded.Title != wiz.Title {
		t.Fatalf("title roundtrip: got %q want %q", loaded.Title, wiz.Title)
	}
}
