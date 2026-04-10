package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

const DefaultPath = "~/.config/muxedo/config.toml"

type Config struct {
	Theme ThemeConfig `toml:"theme"`
}

type ThemeConfig struct {
	InactiveBorder      string `toml:"inactive_border"`
	InactiveTitleFG     string `toml:"inactive_title_fg"`
	InactiveTitleBG     string `toml:"inactive_title_bg"`
	ActiveNormalBorder  string `toml:"active_normal_border"`
	ActiveNormalTitleFG string `toml:"active_normal_title_fg"`
	ActiveNormalTitleBG string `toml:"active_normal_title_bg"`
	ActiveInsertBorder  string `toml:"active_insert_border"`
	ActiveInsertTitleFG string `toml:"active_insert_title_fg"`
	ActiveInsertTitleBG string `toml:"active_insert_title_bg"`
	StoppedBorder       string `toml:"stopped_border"`
	StoppedTitleFG      string `toml:"stopped_title_fg"`
	StoppedTitleBG      string `toml:"stopped_title_bg"`
	EmptyBorder         string `toml:"empty_border"`
	OverlayFG           string `toml:"overlay_fg"`
	OverlayBG           string `toml:"overlay_bg"`
	StatusBarFG         string `toml:"status_bar_fg"`
	StatusBarBG         string `toml:"status_bar_bg"`
	StatusTimeFG        string `toml:"status_time_fg"`
	StatusTimeBG        string `toml:"status_time_bg"`
	StatusActivePanelFG string `toml:"status_active_panel_fg"`
	StatusActivePanelBG string `toml:"status_active_panel_bg"`
	StatusModeNoneFG    string `toml:"status_mode_none_fg"`
	StatusModeNoneBG    string `toml:"status_mode_none_bg"`
	StatusModeNormalFG  string `toml:"status_mode_normal_fg"`
	StatusModeNormalBG  string `toml:"status_mode_normal_bg"`
	StatusModeInsertFG  string `toml:"status_mode_insert_fg"`
	StatusModeInsertBG  string `toml:"status_mode_insert_bg"`
	StatusHintFG        string `toml:"status_hint_fg"`
	StatusHintBG        string `toml:"status_hint_bg"`
}

func Default() Config {
	return Config{
		Theme: DefaultTheme(),
	}
}

func DefaultTheme() ThemeConfig {
	return ThemeConfig{
		InactiveBorder:      "#5f87af",
		InactiveTitleFG:     "#d0d0d0",
		InactiveTitleBG:     "#5f5f87",
		ActiveNormalBorder:  "#ff8700",
		ActiveNormalTitleFG: "#ffffd7",
		ActiveNormalTitleBG: "#ff8700",
		ActiveInsertBorder:  "#00ff00",
		ActiveInsertTitleFG: "#ffffd7",
		ActiveInsertTitleBG: "#00af00",
		StoppedBorder:       "#585858",
		StoppedTitleFG:      "#8a8a8a",
		StoppedTitleBG:      "#444444",
		EmptyBorder:         "#303030",
		OverlayFG:           "#ffffd7",
		OverlayBG:           "#444444",
		StatusBarFG:         "#d0d0d0",
		StatusBarBG:         "#262626",
		StatusTimeFG:        "#ffffd7",
		StatusTimeBG:        "#5f5f87",
		StatusActivePanelFG: "#ffffd7",
		StatusActivePanelBG: "#5f5fd7",
		StatusModeNoneFG:    "#ffffd7",
		StatusModeNoneBG:    "#585858",
		StatusModeNormalFG:  "#ffffd7",
		StatusModeNormalBG:  "#ff8700",
		StatusModeInsertFG:  "#ffffd7",
		StatusModeInsertBG:  "#00af00",
		StatusHintFG:        "#d0d0d0",
		StatusHintBG:        "#444444",
	}
}

func Load() (Config, error) {
	path, err := defaultPath()
	if err != nil {
		return Config{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("reading muxedo config: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing muxedo config: %w", err)
	}

	return cfg, nil
}

func Path() (string, error) {
	return defaultPath()
}

func WriteDefault(force bool) (string, error) {
	path, err := defaultPath()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating muxedo config directory: %w", err)
	}

	if !force {
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("muxedo config already exists at %s: %w", path, os.ErrExist)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("checking muxedo config: %w", err)
		}
	}

	data, err := toml.Marshal(Default())
	if err != nil {
		return "", fmt.Errorf("serializing muxedo config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("writing muxedo config: %w", err)
	}
	return path, nil
}

func defaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "muxedo", "config.toml"), nil
}
