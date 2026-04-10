package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"muxedo/internal/config"
	"muxedo/internal/process"
	"muxedo/internal/profile"
	"muxedo/internal/ui"
)

func main() {
	profilePath := flag.String("profile", "", "path to TOML profile file")
	flag.Parse()

	if _, err := config.Load(); err != nil {
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

	sb := cfg.Scrollback
	panels := make([]*process.Panel, len(cfg.Panels))
	for i, spec := range cfg.Panels {
		panels[i] = process.NewWithScrollback(spec.Name, spec.Cmd, spec.WorkingDir, sb.Dir, sb.MaxBytes)
	}

	for _, p := range panels {
		if err := p.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "error starting %s: %v\n", p.Name, err)
			for _, started := range panels {
				started.Stop()
			}
			os.Exit(1)
		}
	}

	model := ui.NewModel(panels, sb.Editor)
	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		for _, p := range panels {
			p.Stop()
		}
		os.Exit(1)
	}
}
