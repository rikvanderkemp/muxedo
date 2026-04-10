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

func defaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "muxedo", "config.toml"), nil
}
