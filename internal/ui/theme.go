// SPDX-License-Identifier: MIT
package ui

import (
	"image/color"

	"github.com/rikvanderkemp/muxedo/internal/config"

	"charm.land/lipgloss/v2"
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
	cfg := config.DefaultTheme()
	return Theme{
		InactiveBorder:      cfg.InactiveBorder,
		InactiveTitleFG:     cfg.InactiveTitleFG,
		InactiveTitleBG:     cfg.InactiveTitleBG,
		ActiveNormalBorder:  cfg.ActiveNormalBorder,
		ActiveNormalTitleFG: cfg.ActiveNormalTitleFG,
		ActiveNormalTitleBG: cfg.ActiveNormalTitleBG,
		ActiveInsertBorder:  cfg.ActiveInsertBorder,
		ActiveInsertTitleFG: cfg.ActiveInsertTitleFG,
		ActiveInsertTitleBG: cfg.ActiveInsertTitleBG,
		StoppedBorder:       cfg.StoppedBorder,
		StoppedTitleFG:      cfg.StoppedTitleFG,
		StoppedTitleBG:      cfg.StoppedTitleBG,
		EmptyBorder:         cfg.EmptyBorder,
		OverlayFG:           cfg.OverlayFG,
		OverlayBG:           cfg.OverlayBG,
		StatusBarFG:         cfg.StatusBarFG,
		StatusBarBG:         cfg.StatusBarBG,
		StatusTimeFG:        cfg.StatusTimeFG,
		StatusTimeBG:        cfg.StatusTimeBG,
		StatusActivePanelFG: cfg.StatusActivePanelFG,
		StatusActivePanelBG: cfg.StatusActivePanelBG,
		StatusModeNoneFG:    cfg.StatusModeNoneFG,
		StatusModeNoneBG:    cfg.StatusModeNoneBG,
		StatusModeNormalFG:  cfg.StatusModeNormalFG,
		StatusModeNormalBG:  cfg.StatusModeNormalBG,
		StatusModeInsertFG:  cfg.StatusModeInsertFG,
		StatusModeInsertBG:  cfg.StatusModeInsertBG,
		StatusHintFG:        cfg.StatusHintFG,
		StatusHintBG:        cfg.StatusHintBG,
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

func (t Theme) color(value string) color.Color {
	return lipgloss.Color(value)
}
