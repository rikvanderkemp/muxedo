package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

type PanelSpec struct {
	Name       string
	WorkingDir string
	Cmd        string
}

type ScrollbackConfig struct {
	Dir      string
	MaxBytes int64
	Editor   string
}

type Profile struct {
	Panels     []PanelSpec
	Scrollback ScrollbackConfig
}

type rawProfile struct {
	Panel      map[string]rawPanel `toml:"panel"`
	Scrollback rawScrollback       `toml:"scrollback"`
}

type rawPanel struct {
	WorkingDir string `toml:"workingdir"`
	Cmd        string `toml:"cmd"`
}

type rawScrollback struct {
	Dir      string `toml:"dir"`
	MaxBytes *int64 `toml:"max_bytes"`
	Editor   string `toml:"editor"`
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
		if p.Cmd == "" {
			return Profile{}, fmt.Errorf("panel %q: cmd is required", name)
		}
		if p.WorkingDir == "" {
			return Profile{}, fmt.Errorf("panel %q: workingdir is required", name)
		}
		dir := expandHome(p.WorkingDir)
		abs, err := filepath.Abs(dir)
		if err != nil {
			return Profile{}, fmt.Errorf("panel %q: resolving workingdir: %w", name, err)
		}
		panels = append(panels, PanelSpec{
			Name:       name,
			WorkingDir: abs,
			Cmd:        p.Cmd,
		})
	}

	sb, err := resolveScrollback(raw.Scrollback)
	if err != nil {
		return Profile{}, err
	}

	return Profile{Panels: panels, Scrollback: sb}, nil
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

	editor := raw.Editor
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	return ScrollbackConfig{
		Dir:      dir,
		MaxBytes: maxBytes,
		Editor:   editor,
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
