# muxedo

```text
   .--.
  |o_o |
  |:_/ |
 //   \ \
(| muxedo |)
'/'\_ _/`\
 \___)=(___/
```

`muxedo` is a terminal multiplexer TUI that runs commands from a TOML profile in a live auto-grid layout.

It is built for the "run a few long-lived commands and keep them visible" workflow: app servers, logs, watchers, shells, and project startup tasks in one terminal window.

## Demo

[![asciicast](https://asciinema.org/a/K752cVAJSuo99YQu.svg)](https://asciinema.org/a/K752cVAJSuo99YQu)

Recorded demo showing async startup progress in the Message Buffer and a focused panel in insert mode.

## Install

### Homebrew (macOS and Linux)

Requires [Homebrew](https://brew.sh).

```bash
brew tap rikvanderkemp/muxedo
brew install muxedo
```

Use `brew upgrade muxedo` to update later.

### Go

Install the latest tagged release:

```bash
go install github.com/rikvanderkemp/muxedo@latest
```

Install a specific release:

```bash
go install github.com/rikvanderkemp/muxedo@v0.1.0
```

### Install script

Install the latest Linux/macOS release to `~/.local/bin/muxedo`:

```bash
curl -fsSL https://raw.githubusercontent.com/rikvanderkemp/muxedo/main/scripts/install.sh | sh
```

Install a specific release or custom directory:

```bash
curl -fsSL https://raw.githubusercontent.com/rikvanderkemp/muxedo/main/scripts/install.sh | VERSION=v0.1.0 INSTALL_DIR="$HOME/bin" sh
```

The installer supports `linux` and `darwin` on `amd64` and `arm64`, verifies the published SHA-256 checksums, and prints a PATH hint if needed.

## First Run

1. Copy the example profile:

```bash
cp profile.toml.example .muxedo
```

2. Start muxedo from that directory:

```bash
muxedo
```

Or point at a profile explicitly:

```bash
muxedo -profile profile.toml.example
```

When `-profile` is omitted, muxedo looks for `./.muxedo` in the current working directory. If no profile is found and the session is interactive, muxedo launches a first-run wizard that walks you through a title, a working directory, optional startup commands, and one or more panels, then writes a ready-to-use TOML profile (default path `./.muxedo`). In non-interactive sessions muxedo prints the missing-profile error with the full command help instead.

## Example Profile

The bundled [profile.toml.example](profile.toml.example) is a safe cross-platform demo. A profile defines optional startup tasks plus one or more panels:

```toml
workingdir = "."

[[startup]]
shell = "printf 'bootstrapping demo...\\n'; sleep 1"
mode = "sync"

[panel.clock]
order = 0
shell = "while true; do date; sleep 1; done"

[panel.system]
shell = "while true; do uname -a; sleep 5; done"

[panel.pong]
shell = "i=0 d=1; while true; do printf '\\r[%*s><%*s]' \"$i\" '' \"$((20-i))\" ''; sleep 0.08; [ \"$i\" -eq 20 ] && d=-1; [ \"$i\" -eq 0 ] && d=1; i=$((i+d)); done"

[panel.echo]
shell = "printf 'Insert mode demo: type something and press Enter.\\n\\n'; while IFS= read -r line; do printf 'you typed: %s\\n' \"$line\"; done"
```

Profile rules:

- Use `[[startup]]` for one-off setup commands that run before or alongside the UI.
- Use `[panel.<name>]` for long-lived commands you want visible in the grid.
- Set exactly one of `program` or `shell` for each startup item and panel.
- Use `workingdir` at the top level as the default directory, then override per startup item or panel when needed.
- Use `order` to pin a panel earlier in the grid without rearranging the file.

Shell fields execute via `sh`, so treat profile files as trusted local automation.

## Controls

The shortest path to using muxedo:

- Click a panel to focus it.
- Press `i` to enter insert mode and send keys to the running process.
- Press `Esc` once to return to normal mode, then `Esc` again to unfocus the panel.
- Press `h` `j` `k` `l` in normal mode to move between panels.
- Press `1` to `9` in normal mode to jump to the first nine panels.
- Press `z` in a focused panel to inspect scrollback.
- Press `v` in a focused panel to select and copy text.
- Press `r` in normal mode to restart the focused panel.
- Press `x` in normal mode to stop the focused panel and run its optional kill command.
- Press `m` in normal mode to maximize or restore the focused panel.
- Press `Ctrl+B` to toggle the Message Buffer.
- Press `q` or `Ctrl+C` to quit when no panel is focused.

## Scrollback and Clipboard

Muxedo captures each panel's output to a scrollback file for the current run. Scrollback starts empty on launch, grows while the panel runs, and is cleared when that panel is restarted with `R`.

Scrollback mode is best-effort for full-screen TUIs and works best with shells, logs, and line-oriented output.

Select mode copies text to the system clipboard when one of these tools is available: `pbcopy`, `wl-copy`, `xclip`, or `xsel`. If none are available, muxedo falls back to OSC52 terminal clipboard copy when supported.

Optional scrollback settings:

```toml
[scrollback]
dir = "~/.cache/muxedo/scrollback"
max_bytes = 1048576
```

`dir` defaults to the OS cache directory. `max_bytes` defaults to `1 MiB` per panel, and `0` means unlimited.

Restarting a panel with `R` clears that panel's scrollback file for the current run.

## App Config

Muxedo also looks for an optional app-level config at `~/.config/muxedo/config.toml`.

That file is for muxedo behavior, not for your panel layout. To generate a complete config file with defaults, run:

```bash
muxedo -dump-config
```

Useful app-level options:

```toml
[ui]
show_exit_message = true
check_updates_on_start = true
```

- `show_exit_message` controls the support message printed after muxedo exits.
- `check_updates_on_start` controls the default-on startup update check. In non-interactive sessions, muxedo skips the prompt and starts normally.

You can also override UI colors with a `[theme]` section:

```toml
[theme]
inactive_border = "#5f87af"
inactive_title_fg = "#d0d0d0"
inactive_title_bg = "#5f5f87"
active_normal_border = "#ff8700"
active_normal_title_fg = "#ffffd7"
active_normal_title_bg = "#ff8700"
active_insert_border = "#00ff00"
active_insert_title_fg = "#ffffd7"
active_insert_title_bg = "#00af00"
stopped_border = "#585858"
stopped_title_fg = "#8a8a8a"
stopped_title_bg = "#444444"
empty_border = "#303030"
overlay_fg = "#ffffd7"
overlay_bg = "#444444"
status_bar_fg = "#d0d0d0"
status_bar_bg = "#262626"
status_time_fg = "#ffffd7"
status_time_bg = "#5f5f87"
status_active_panel_fg = "#ffffd7"
status_active_panel_bg = "#5f5fd7"
status_mode_none_fg = "#ffffd7"
status_mode_none_bg = "#585858"
status_mode_normal_fg = "#ffffd7"
status_mode_normal_bg = "#ff8700"
status_mode_insert_fg = "#ffffd7"
status_mode_insert_bg = "#00af00"
status_hint_fg = "#d0d0d0"
status_hint_bg = "#444444"
```

## Self-Update

Official release builds can check for newer GitHub releases and replace themselves in place:

```bash
muxedo update check
muxedo update apply
```

- `update check` prints the current and latest release versions.
- `update apply` downloads the matching release tarball, verifies `checksums.txt`, replaces the current executable, then exits.
- Startup checks for updates by default in interactive terminals and prompts before continuing.
- Self-update is unavailable for `dev` builds.
- Package-manager installs may not be writable or may be managed externally.

## Contributing

Pull request titles and commit messages must use Conventional Commit format:

- `feat(ui): add panel maximize toggle`
- `fix(process): stabilize scrollback IDs`
- `docs(readme): improve first-run guide`

This repository uses squash merges on `main` for release automation.

## License

MIT - see [LICENSE](LICENSE) for details.

## Support

If muxedo saves you time, you can support development here:

<a href="https://www.buymeacoffee.com/rikvanderkemp" target="_blank"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-yellow.png" alt="Buy Me a Coffee" style="height: 60px !important;width: 217px !important;"></a>
