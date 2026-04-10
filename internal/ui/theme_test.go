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
