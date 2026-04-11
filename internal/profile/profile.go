package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"muxedo/internal/process"
)

type PanelSpec struct {
	Name        string
	WorkingDir  string
	Command     process.CommandSpec
	KillCommand process.CommandSpec
}

type StartupSpec struct {
	WorkingDir string
	Command    process.CommandSpec
	Mode       StartupMode
}

type StartupMode string

const (
	StartupModeAsync StartupMode = "async"
	StartupModeSync  StartupMode = "sync"
)

type ScrollbackConfig struct {
	Dir      string
	MaxBytes int64
}

type Profile struct {
	Panels     []PanelSpec
	Startup    []StartupSpec
	Scrollback ScrollbackConfig
	WorkingDir string
}

type rawProfile struct {
	WorkingDir string              `toml:"workingdir"`
	Startup    []rawStartup        `toml:"startup"`
	Panel      map[string]rawPanel `toml:"panel"`
	Scrollback rawScrollback       `toml:"scrollback"`
}

type rawStartup struct {
	Program    string   `toml:"program"`
	Args       []string `toml:"args"`
	Shell      string   `toml:"shell"`
	Cmd        string   `toml:"cmd"`
	WorkingDir string   `toml:"workingdir"`
	Mode       string   `toml:"mode"`
}

type rawPanel struct {
	WorkingDir  string   `toml:"workingdir"`
	Program     string   `toml:"program"`
	Args        []string `toml:"args"`
	Shell       string   `toml:"shell"`
	KillProgram string   `toml:"kill_program"`
	KillArgs    []string `toml:"kill_args"`
	ShellKill   string   `toml:"shell_kill"`
	Cmd         string   `toml:"cmd"`
	CmdKill     string   `toml:"cmd_kill"`
}

type rawScrollback struct {
	Dir      string `toml:"dir"`
	MaxBytes *int64 `toml:"max_bytes"`
}

func Load(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, fmt.Errorf("reading profile: %w", err)
	}

	var raw rawProfile
	if err := toml.Unmarshal(data, &raw); err != nil {
		return Profile{}, fmt.Errorf("parsing profile: %w", err)
	}

	globalWorkingDir := expandHome(raw.WorkingDir)
	if globalWorkingDir != "" {
		abs, err := filepath.Abs(globalWorkingDir)
		if err != nil {
			return Profile{}, fmt.Errorf("resolving global workingdir: %w", err)
		}
		globalWorkingDir = abs
	}

	if len(raw.Panel) == 0 {
		return Profile{}, fmt.Errorf("profile has no [panel.*] sections")
	}

	names := make([]string, 0, len(raw.Panel))
	for name := range raw.Panel {
		names = append(names, name)
	}
	sort.Strings(names)

	panels := make([]PanelSpec, 0, len(names))
	for _, name := range names {
		p := raw.Panel[name]
		if p.Cmd != "" || p.CmdKill != "" {
			return Profile{}, fmt.Errorf("panel %q: legacy cmd/cmd_kill fields are no longer supported; use program/args or shell/shell_kill", name)
		}

		dir := p.WorkingDir
		if dir == "" {
			dir = globalWorkingDir
		}
		if dir == "" {
			return Profile{}, fmt.Errorf("panel %q: workingdir is required (none specified for panel or globally)", name)
		}

		dir = expandHome(dir)
		abs, err := filepath.Abs(dir)
		if err != nil {
			return Profile{}, fmt.Errorf("panel %q: resolving workingdir: %w", name, err)
		}

		cmd := process.CommandSpec{
			Program: p.Program,
			Args:    p.Args,
			Shell:   p.Shell,
		}
		if err := cmd.Validate(fmt.Sprintf("panel %q", name)); err != nil {
			return Profile{}, err
		}

		kill := process.CommandSpec{
			Program: p.KillProgram,
			Args:    p.KillArgs,
			Shell:   p.ShellKill,
		}
		if !kill.IsZero() {
			if err := kill.Validate(fmt.Sprintf("panel %q kill command", name)); err != nil {
				return Profile{}, err
			}
		}

		panels = append(panels, PanelSpec{
			Name:        name,
			WorkingDir:  abs,
			Command:     cmd,
			KillCommand: kill,
		})
	}

	startup := make([]StartupSpec, 0, len(raw.Startup))
	for i, s := range raw.Startup {
		if s.Cmd != "" {
			return Profile{}, fmt.Errorf("startup command at index %d: legacy cmd field is no longer supported; use program/args or shell", i)
		}

		dir := s.WorkingDir
		if dir == "" {
			dir = globalWorkingDir
		}
		if dir == "" {
			dir = "."
		}

		dir = expandHome(dir)
		abs, err := filepath.Abs(dir)
		if err != nil {
			return Profile{}, fmt.Errorf("startup command at index %d: resolving workingdir: %w", i, err)
		}

		cmd := process.CommandSpec{
			Program: s.Program,
			Args:    s.Args,
			Shell:   s.Shell,
		}
		if err := cmd.Validate(fmt.Sprintf("startup command at index %d", i)); err != nil {
			return Profile{}, err
		}

		mode := StartupModeAsync
		if s.Mode != "" {
			mode = StartupMode(s.Mode)
		}
		switch mode {
		case StartupModeAsync, StartupModeSync:
		default:
			return Profile{}, fmt.Errorf("startup command at index %d: invalid mode %q (want async or sync)", i, s.Mode)
		}

		startup = append(startup, StartupSpec{
			WorkingDir: abs,
			Command:    cmd,
			Mode:       mode,
		})
	}

	sb, err := resolveScrollback(raw.Scrollback)
	if err != nil {
		return Profile{}, err
	}

	return Profile{
		Panels:     panels,
		Startup:    startup,
		Scrollback: sb,
		WorkingDir: globalWorkingDir,
	}, nil
}

const defaultMaxBytes int64 = 1 << 20 // 1 MiB per panel

func resolveScrollback(raw rawScrollback) (ScrollbackConfig, error) {
	dir := raw.Dir
	if dir == "" {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			return ScrollbackConfig{}, fmt.Errorf("scrollback: determining cache dir: %w", err)
		}
		dir = filepath.Join(cacheDir, "muxedo", "scrollback")
	} else {
		dir = expandHome(dir)
		var err error
		dir, err = filepath.Abs(dir)
		if err != nil {
			return ScrollbackConfig{}, fmt.Errorf("scrollback: resolving dir: %w", err)
		}
	}

	maxBytes := defaultMaxBytes
	if raw.MaxBytes != nil {
		maxBytes = *raw.MaxBytes
	}

	return ScrollbackConfig{
		Dir:      dir,
		MaxBytes: maxBytes,
	}, nil
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
