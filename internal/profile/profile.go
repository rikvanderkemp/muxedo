// SPDX-License-Identifier: MIT
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"

	"github.com/rikvanderkemp/muxedo/internal/process"
)

// PanelSpec describes one configured panel command.
type PanelSpec struct {
	Name        string
	Order       *int
	WorkingDir  string
	Command     process.CommandSpec
	KillCommand process.CommandSpec
}

// StartupSpec describes command run before panel initialization.
type StartupSpec struct {
	WorkingDir string
	Command    process.CommandSpec
	Mode       StartupMode
}

// StartupMode controls whether startup command blocks later startup steps.
type StartupMode string

const (
	// StartupModeAsync runs command without waiting for completion.
	StartupModeAsync StartupMode = "async"
	// StartupModeSync runs command and waits for completion.
	StartupModeSync StartupMode = "sync"
)

// ScrollbackConfig configures persisted panel scrollback storage.
type ScrollbackConfig struct {
	Dir      string
	MaxBytes int64
}

// Profile is parsed muxedo profile configuration.
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
	Order       *int     `toml:"order"`
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

// Load parses muxedo profile from path and resolves derived defaults.
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

	declaredPanelNames, err := panelDeclarationOrder(data)
	if err != nil {
		return Profile{}, fmt.Errorf("parsing profile panel order: %w", err)
	}

	type panelEntry struct {
		name             string
		declarationIndex int
		order            *int
		spec             PanelSpec
	}

	entries := make([]panelEntry, 0, len(raw.Panel))
	explicitOrders := make(map[int]string, len(raw.Panel))
	for declarationIndex, name := range declaredPanelNames {
		p, ok := raw.Panel[name]
		if !ok {
			continue
		}
		if p.Cmd != "" || p.CmdKill != "" {
			return Profile{}, fmt.Errorf("panel %q: legacy cmd/cmd_kill fields are no longer supported; use program/args or shell/shell_kill", name)
		}
		if p.Order != nil {
			if *p.Order < 0 {
				return Profile{}, fmt.Errorf("panel %q: order must be non-negative", name)
			}
			if existing, exists := explicitOrders[*p.Order]; exists {
				return Profile{}, fmt.Errorf("panels %q and %q use duplicate order %d", existing, name, *p.Order)
			}
			explicitOrders[*p.Order] = name
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

		entries = append(entries, panelEntry{
			name:             name,
			declarationIndex: declarationIndex,
			order:            p.Order,
			spec: PanelSpec{
				Name:        name,
				Order:       p.Order,
				WorkingDir:  abs,
				Command:     cmd,
				KillCommand: kill,
			},
		})
	}
	if len(entries) != len(raw.Panel) {
		return Profile{}, fmt.Errorf("could not determine declaration order for all [panel.*] sections")
	}

	sort.SliceStable(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		switch {
		case left.order != nil && right.order != nil:
			if *left.order != *right.order {
				return *left.order < *right.order
			}
		case left.order != nil:
			return true
		case right.order != nil:
			return false
		}
		return left.declarationIndex < right.declarationIndex
	})

	panels := make([]PanelSpec, 0, len(entries))
	for _, entry := range entries {
		panels = append(panels, entry.spec)
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

func panelDeclarationOrder(data []byte) ([]string, error) {
	parser := unstable.Parser{}
	parser.Reset(data)

	var names []string
	seen := make(map[string]struct{})
	for parser.NextExpression() {
		expr := parser.Expression()
		if expr.Kind != unstable.Table {
			continue
		}

		key := make([]string, 0, 2)
		iter := expr.Key()
		for iter.Next() {
			key = append(key, string(iter.Node().Data))
		}
		if len(key) != 2 || key[0] != "panel" {
			continue
		}

		name := key[1]
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	return names, nil
}
