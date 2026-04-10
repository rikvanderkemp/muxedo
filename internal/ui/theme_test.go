package ui

import (
	"testing"

	"muxedo/internal/config"
)

func TestResolveThemeDefaults(t *testing.T) {
	got := ResolveTheme(config.ThemeConfig{})
	if got != DefaultTheme() {
		t.Fatalf("ResolveTheme(empty) = %#v, want %#v", got, DefaultTheme())
	}
}

func TestResolveThemeOverridesSpecifiedFields(t *testing.T) {
	got := ResolveTheme(config.ThemeConfig{
		InactiveBorder:     "#123456",
		StatusModeNormalBG: "208",
	})

	if got.InactiveBorder != "#123456" {
		t.Fatalf("InactiveBorder = %q", got.InactiveBorder)
	}
	if got.StatusModeNormalBG != "208" {
		t.Fatalf("StatusModeNormalBG = %q", got.StatusModeNormalBG)
	}
	if got.StatusBarBG != DefaultTheme().StatusBarBG {
		t.Fatalf("StatusBarBG = %q, want default %q", got.StatusBarBG, DefaultTheme().StatusBarBG)
	}
}

func TestDefaultThemeMatchesConfigDefaults(t *testing.T) {
	got := DefaultTheme()
	want := config.DefaultTheme()

	if got.InactiveBorder != want.InactiveBorder {
		t.Fatalf("InactiveBorder = %q, want %q", got.InactiveBorder, want.InactiveBorder)
	}
	if got.InactiveTitleFG != want.InactiveTitleFG {
		t.Fatalf("InactiveTitleFG = %q, want %q", got.InactiveTitleFG, want.InactiveTitleFG)
	}
	if got.InactiveTitleBG != want.InactiveTitleBG {
		t.Fatalf("InactiveTitleBG = %q, want %q", got.InactiveTitleBG, want.InactiveTitleBG)
	}
	if got.ActiveNormalBorder != want.ActiveNormalBorder {
		t.Fatalf("ActiveNormalBorder = %q, want %q", got.ActiveNormalBorder, want.ActiveNormalBorder)
	}
	if got.ActiveNormalTitleFG != want.ActiveNormalTitleFG {
		t.Fatalf("ActiveNormalTitleFG = %q, want %q", got.ActiveNormalTitleFG, want.ActiveNormalTitleFG)
	}
	if got.ActiveNormalTitleBG != want.ActiveNormalTitleBG {
		t.Fatalf("ActiveNormalTitleBG = %q, want %q", got.ActiveNormalTitleBG, want.ActiveNormalTitleBG)
	}
	if got.ActiveInsertBorder != want.ActiveInsertBorder {
		t.Fatalf("ActiveInsertBorder = %q, want %q", got.ActiveInsertBorder, want.ActiveInsertBorder)
	}
	if got.ActiveInsertTitleFG != want.ActiveInsertTitleFG {
		t.Fatalf("ActiveInsertTitleFG = %q, want %q", got.ActiveInsertTitleFG, want.ActiveInsertTitleFG)
	}
	if got.ActiveInsertTitleBG != want.ActiveInsertTitleBG {
		t.Fatalf("ActiveInsertTitleBG = %q, want %q", got.ActiveInsertTitleBG, want.ActiveInsertTitleBG)
	}
	if got.StoppedBorder != want.StoppedBorder {
		t.Fatalf("StoppedBorder = %q, want %q", got.StoppedBorder, want.StoppedBorder)
	}
	if got.StoppedTitleFG != want.StoppedTitleFG {
		t.Fatalf("StoppedTitleFG = %q, want %q", got.StoppedTitleFG, want.StoppedTitleFG)
	}
	if got.StoppedTitleBG != want.StoppedTitleBG {
		t.Fatalf("StoppedTitleBG = %q, want %q", got.StoppedTitleBG, want.StoppedTitleBG)
	}
	if got.EmptyBorder != want.EmptyBorder {
		t.Fatalf("EmptyBorder = %q, want %q", got.EmptyBorder, want.EmptyBorder)
	}
	if got.OverlayFG != want.OverlayFG {
		t.Fatalf("OverlayFG = %q, want %q", got.OverlayFG, want.OverlayFG)
	}
	if got.OverlayBG != want.OverlayBG {
		t.Fatalf("OverlayBG = %q, want %q", got.OverlayBG, want.OverlayBG)
	}
	if got.StatusBarFG != want.StatusBarFG {
		t.Fatalf("StatusBarFG = %q, want %q", got.StatusBarFG, want.StatusBarFG)
	}
	if got.StatusBarBG != want.StatusBarBG {
		t.Fatalf("StatusBarBG = %q, want %q", got.StatusBarBG, want.StatusBarBG)
	}
	if got.StatusTimeFG != want.StatusTimeFG {
		t.Fatalf("StatusTimeFG = %q, want %q", got.StatusTimeFG, want.StatusTimeFG)
	}
	if got.StatusTimeBG != want.StatusTimeBG {
		t.Fatalf("StatusTimeBG = %q, want %q", got.StatusTimeBG, want.StatusTimeBG)
	}
	if got.StatusActivePanelFG != want.StatusActivePanelFG {
		t.Fatalf("StatusActivePanelFG = %q, want %q", got.StatusActivePanelFG, want.StatusActivePanelFG)
	}
	if got.StatusActivePanelBG != want.StatusActivePanelBG {
		t.Fatalf("StatusActivePanelBG = %q, want %q", got.StatusActivePanelBG, want.StatusActivePanelBG)
	}
	if got.StatusModeNoneFG != want.StatusModeNoneFG {
		t.Fatalf("StatusModeNoneFG = %q, want %q", got.StatusModeNoneFG, want.StatusModeNoneFG)
	}
	if got.StatusModeNoneBG != want.StatusModeNoneBG {
		t.Fatalf("StatusModeNoneBG = %q, want %q", got.StatusModeNoneBG, want.StatusModeNoneBG)
	}
	if got.StatusModeNormalFG != want.StatusModeNormalFG {
		t.Fatalf("StatusModeNormalFG = %q, want %q", got.StatusModeNormalFG, want.StatusModeNormalFG)
	}
	if got.StatusModeNormalBG != want.StatusModeNormalBG {
		t.Fatalf("StatusModeNormalBG = %q, want %q", got.StatusModeNormalBG, want.StatusModeNormalBG)
	}
	if got.StatusModeInsertFG != want.StatusModeInsertFG {
		t.Fatalf("StatusModeInsertFG = %q, want %q", got.StatusModeInsertFG, want.StatusModeInsertFG)
	}
	if got.StatusModeInsertBG != want.StatusModeInsertBG {
		t.Fatalf("StatusModeInsertBG = %q, want %q", got.StatusModeInsertBG, want.StatusModeInsertBG)
	}
	if got.StatusHintFG != want.StatusHintFG {
		t.Fatalf("StatusHintFG = %q, want %q", got.StatusHintFG, want.StatusHintFG)
	}
	if got.StatusHintBG != want.StatusHintBG {
		t.Fatalf("StatusHintBG = %q, want %q", got.StatusHintBG, want.StatusHintBG)
	}
}
