package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"muxedo/internal/config"
	"muxedo/internal/profile"
	"muxedo/internal/ui"
	"muxedo/internal/update"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"

	newUpdater = func() updaterAPI {
		return update.NewUpdater("rikvanderkemp", "muxedo")
	}
)

type updaterAPI interface {
	Check(string) (update.CheckResult, error)
	Apply(string, string) (update.ApplyResult, error)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "update" {
		if err := runUpdate(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			printUsage(stderr)
			return 1
		}
		return 0
	}

	fs := flag.NewFlagSet("muxedo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		printUsage(stderr)
	}

	profilePath := fs.String("profile", "", "path to TOML profile file (defaults to ./.muxedo when omitted)")
	dumpConfig := fs.Bool("dump-config", false, "write the default app config and exit")
	force := fs.Bool("force", false, "overwrite existing files when used with dump commands")
	showVersion := fs.Bool("version", false, "print version information and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	if *showVersion {
		fmt.Fprintf(stdout, "muxedo %s\ncommit: %s\nbuilt: %s\n", version, commit, buildDate)
		return 0
	}

	if *dumpConfig {
		path, err := config.WriteDefault(*force)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "wrote default config to %s\n", path)
		return 0
	}

	appConfig, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	resolvedProfilePath, err := resolveProfilePath(*profilePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		printUsage(stderr)
		return 1
	}

	cfg, err := profile.Load(resolvedProfilePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	model := ui.NewModelWithSpecs(cfg.Startup, cfg.Panels, cfg.Scrollback, ui.ResolveTheme(appConfig.Theme))
	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	return 0
}

func runUpdate(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing update subcommand (want check or apply)")
	}

	updater := newUpdater()

	switch args[0] {
	case "check":
		if len(args) != 1 {
			return fmt.Errorf("update check does not accept extra arguments")
		}
		result, err := updater.Check(version)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "current: %s\nlatest: %s\n", result.CurrentVersion, result.LatestVersion)
		if result.UpdateAvailable {
			fmt.Fprintf(stdout, "update available\n")
		} else {
			fmt.Fprintf(stdout, "up to date\n")
		}
		return nil
	case "apply":
		if len(args) != 1 {
			return fmt.Errorf("update apply does not accept extra arguments")
		}
		executablePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locating current executable: %w", err)
		}
		result, err := updater.Apply(version, executablePath)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "updated muxedo from %s to %s at %s\n", result.PreviousVersion, result.Version, executablePath)
		fmt.Fprintln(stdout, "restart muxedo to use new version")
		return nil
	default:
		return fmt.Errorf("unknown update subcommand %q (want check or apply)", args[0])
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

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `muxedo runs commands from TOML profile in live auto-grid TUI.

Usage:
  muxedo [flags]
  muxedo update <check|apply>

Flags:
  -profile string
        path to TOML profile file (defaults to ./.muxedo when omitted)
  -dump-config
        write default app config and exit
  -force
        overwrite existing files when used with dump commands
  -version
        print version information and exit
  -help
        show this help

Commands:
  update check
        check latest published release
  update apply
        download, verify, install latest published release

Examples:
  muxedo
  muxedo -profile profile.toml
  muxedo -dump-config
  muxedo -version
  muxedo update check
  muxedo update apply
`)
}
