package ui

import (
	"muxedo/internal/config"

	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	InactiveBorder      string
	InactiveTitleFG     string
	InactiveTitleBG     string
	ActiveNormalBorder  string
	ActiveNormalTitleFG string
	ActiveNormalTitleBG string
	ActiveInsertBorder  string
	ActiveInsertTitleFG string
	ActiveInsertTitleBG string
	StoppedBorder       string
	StoppedTitleFG      string
	StoppedTitleBG      string
	EmptyBorder         string
	OverlayFG           string
	OverlayBG           string
	StatusBarFG         string
	StatusBarBG         string
	StatusTimeFG        string
	StatusTimeBG        string
	StatusActivePanelFG string
	StatusActivePanelBG string
	StatusModeNoneFG    string
	StatusModeNoneBG    string
	StatusModeNormalFG  string
	StatusModeNormalBG  string
	StatusModeInsertFG  string
	StatusModeInsertBG  string
	StatusHintFG        string
	StatusHintBG        string
}

func DefaultTheme() Theme {
	return Theme{
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

func ResolveTheme(cfg config.ThemeConfig) Theme {
	theme := DefaultTheme()
	mergeThemeString(&theme.InactiveBorder, cfg.InactiveBorder)
	mergeThemeString(&theme.InactiveTitleFG, cfg.InactiveTitleFG)
	mergeThemeString(&theme.InactiveTitleBG, cfg.InactiveTitleBG)
	mergeThemeString(&theme.ActiveNormalBorder, cfg.ActiveNormalBorder)
	mergeThemeString(&theme.ActiveNormalTitleFG, cfg.ActiveNormalTitleFG)
	mergeThemeString(&theme.ActiveNormalTitleBG, cfg.ActiveNormalTitleBG)
	mergeThemeString(&theme.ActiveInsertBorder, cfg.ActiveInsertBorder)
	mergeThemeString(&theme.ActiveInsertTitleFG, cfg.ActiveInsertTitleFG)
	mergeThemeString(&theme.ActiveInsertTitleBG, cfg.ActiveInsertTitleBG)
	mergeThemeString(&theme.StoppedBorder, cfg.StoppedBorder)
	mergeThemeString(&theme.StoppedTitleFG, cfg.StoppedTitleFG)
	mergeThemeString(&theme.StoppedTitleBG, cfg.StoppedTitleBG)
	mergeThemeString(&theme.EmptyBorder, cfg.EmptyBorder)
	mergeThemeString(&theme.OverlayFG, cfg.OverlayFG)
	mergeThemeString(&theme.OverlayBG, cfg.OverlayBG)
	mergeThemeString(&theme.StatusBarFG, cfg.StatusBarFG)
	mergeThemeString(&theme.StatusBarBG, cfg.StatusBarBG)
	mergeThemeString(&theme.StatusTimeFG, cfg.StatusTimeFG)
	mergeThemeString(&theme.StatusTimeBG, cfg.StatusTimeBG)
	mergeThemeString(&theme.StatusActivePanelFG, cfg.StatusActivePanelFG)
	mergeThemeString(&theme.StatusActivePanelBG, cfg.StatusActivePanelBG)
	mergeThemeString(&theme.StatusModeNoneFG, cfg.StatusModeNoneFG)
	mergeThemeString(&theme.StatusModeNoneBG, cfg.StatusModeNoneBG)
	mergeThemeString(&theme.StatusModeNormalFG, cfg.StatusModeNormalFG)
	mergeThemeString(&theme.StatusModeNormalBG, cfg.StatusModeNormalBG)
	mergeThemeString(&theme.StatusModeInsertFG, cfg.StatusModeInsertFG)
	mergeThemeString(&theme.StatusModeInsertBG, cfg.StatusModeInsertBG)
	mergeThemeString(&theme.StatusHintFG, cfg.StatusHintFG)
	mergeThemeString(&theme.StatusHintBG, cfg.StatusHintBG)
	return theme
}

func mergeThemeString(dst *string, src string) {
	if src != "" {
		*dst = src
	}
}

func (t Theme) color(value string) lipgloss.Color {
	return lipgloss.Color(value)
}
