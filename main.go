// SPDX-License-Identifier: MIT
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
	newStartupUpdater = func() updaterAPI {
		return update.NewUpdaterWithClient("rikvanderkemp", "muxedo", &http.Client{Timeout: 2 * time.Second})
	}
	runProgram = func(model tea.Model) error {
		prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
		_, err := prog.Run()
		return err
	}
	promptInput      io.Reader = os.Stdin
	promptOutput     io.Writer = os.Stdout
	isInteractiveTTY           = defaultIsInteractiveTTY
	execSelf                   = defaultExecSelf
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

	if err := maybeApplyStartupUpdate(appConfig, args, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "warning: %v\n", err)
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
	if err := runProgram(model); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	if appConfig.ExitMessageEnabled() {
		printExitMessage(stdout)
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

func maybeApplyStartupUpdate(appConfig config.Config, args []string, stdout io.Writer, stderr io.Writer) error {
	if !appConfig.CheckUpdatesOnStartEnabled() || version == "dev" {
		return nil
	}

	result, err := newStartupUpdater().Check(version)
	if err != nil {
		return fmt.Errorf("startup update check failed: %w", err)
	}
	if !result.UpdateAvailable {
		return nil
	}

	if !isInteractiveTTY(promptInput, promptOutput) {
		fmt.Fprintf(stderr, "warning: update available (%s -> %s); skipping prompt in non-interactive session\n", result.CurrentVersion, result.LatestVersion)
		return nil
	}

	apply, err := promptForStartupUpdate(result, promptInput, promptOutput)
	if err != nil {
		return fmt.Errorf("startup update prompt failed: %w", err)
	}
	if !apply {
		return nil
	}

	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("startup update failed locating current executable: %w", err)
	}

	if _, err := newUpdater().Apply(version, executablePath); err != nil {
		return fmt.Errorf("startup update apply failed: %w", err)
	}

	if err := execSelf(executablePath, args); err != nil {
		return fmt.Errorf("startup update applied but restart failed: %w", err)
	}
	return nil
}

func promptForStartupUpdate(result update.CheckResult, input io.Reader, output io.Writer) (bool, error) {
	reader := bufio.NewReader(input)
	fmt.Fprintf(output, "update available: %s -> %s\n", result.CurrentVersion, result.LatestVersion)
	for {
		fmt.Fprint(output, "Apply update now? [Y/n] ")
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				answer := strings.TrimSpace(line)
				if answer == "" {
					return true, nil
				}
				switch strings.ToLower(answer) {
				case "y", "yes":
					return true, nil
				case "n", "no":
					return false, nil
				default:
					return false, io.ErrUnexpectedEOF
				}
			}
			return false, err
		}

		switch strings.ToLower(strings.TrimSpace(line)) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
	}
}

func defaultIsInteractiveTTY(input io.Reader, output io.Writer) bool {
	inFile, ok := input.(*os.File)
	if !ok {
		return false
	}
	outFile, ok := output.(*os.File)
	if !ok {
		return false
	}
	return isCharDevice(inFile) && isCharDevice(outFile)
}

func isCharDevice(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func defaultExecSelf(executablePath string, args []string) error {
	return syscall.Exec(executablePath, append([]string{executablePath}, args...), os.Environ())
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

func printExitMessage(w io.Writer) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Thanks for using muxedo.")
	fmt.Fprintln(w, "Support development: https://buymeacoffee.com/rikvanderkemp")
	fmt.Fprintln(w, "Turn this off in ~/.config/muxedo/config.toml with [ui] show_exit_message = false")
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
