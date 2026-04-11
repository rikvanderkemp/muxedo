package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"muxedo/internal/config"
	"muxedo/internal/profile"
	"muxedo/internal/ui"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	profilePath := flag.String("profile", "", "path to TOML profile file (defaults to ./.muxedo when omitted)")
	dumpConfig := flag.Bool("dump-config", false, "write the default app config and exit")
	force := flag.Bool("force", false, "overwrite existing files when used with dump commands")
	showVersion := flag.Bool("version", false, "print version information and exit")
	flag.Parse()

	if *showVersion {
		fmt.Fprintf(os.Stdout, "muxedo %s\ncommit: %s\nbuilt: %s\n", version, commit, buildDate)
		return
	}

	if *dumpConfig {
		path, err := config.WriteDefault(*force)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "wrote default config to %s\n", path)
		return
	}

	appConfig, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	resolvedProfilePath, err := resolveProfilePath(*profilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	cfg, err := profile.Load(resolvedProfilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	model := ui.NewModelWithSpecs(cfg.Startup, cfg.Panels, cfg.Scrollback, ui.ResolveTheme(appConfig.Theme))
	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func resolveProfilePath(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("determining working directory: %w", err)
	}

	defaultPath := filepath.Join(cwd, ".muxedo")
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("checking default profile: %w", err)
	}

	return "", fmt.Errorf("-profile is required")
}
