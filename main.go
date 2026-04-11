package main

import (
	"flag"
	"fmt"
	"os"

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
	profilePath := flag.String("profile", "", "path to TOML profile file")
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

	if *profilePath == "" {
		fmt.Fprintln(os.Stderr, "error: -profile is required")
		os.Exit(1)
	}

	cfg, err := profile.Load(*profilePath)
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
