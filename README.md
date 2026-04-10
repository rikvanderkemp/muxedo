# muxedo

```text
+---------------------------+
| .--.   muxedo    .--.     |
| |[]|   .-\/-.    |[]|     |
| |  |   \_  _/    |  |     |
| '--'     \/      '--'     |
+---------------------------+
```

A terminal multiplexer TUI that runs commands from a TOML profile in a live auto-grid layout.

## Quick start

```bash
go build -o muxedo .
./muxedo -profile profile.toml
./muxedo -dump-config
```

Or run directly:

```bash
go run . -profile profile.toml
```

## Profile format

Each `[panel.<name>]` section defines a pane:

```toml
[panel.api]
workingdir = "~/code/myapi"
cmd = "go run ."

[panel.frontend]
workingdir = "~/code/frontend"
cmd = "npm run dev"
```

- `workingdir` — working directory for the command (`~` is expanded).
- `cmd` — shell command to run (executed via `sh -lc`).

Panels are arranged in an auto-grid (near-square) layout that fills the terminal and resizes when the window changes.

## Controls

- Left click a panel — activate/focus that panel.
- **Vim-style tab motion** (recommended — works in plain terminals without special key maps):
  - **No panel focused:** press **`g`** then **`t`** (like Vim’s **`gt`**, next tab) or **`g`** then **`T`** (like **`gT`**, previous tab). Either sequence focuses the **first** panel when you were unfocused; the pending **`g`** times out after two seconds. Plain **`g`** / **`t`** are not sent to any process while nothing is focused.
  - **Panel focused:** On many Linux terminals, **`Alt+t`** / **`Alt+T`** (Meta+t) cycles next/prev. On **macOS**, **Option+t** usually inserts **†** (dagger) and **Option+Shift+t** inserts **‡** instead of sending Meta+t — muxedo treats **†** / **‡** as next/prev so nothing is passed through to the pane and you don’t get stray symbols in the shell.
- **Other panel chords** (wrap; from unfocused, first activation selects the first panel): **`Alt+[`** / **`Alt+Ctrl+]`** (prev), **`Ctrl+]`** / **`Alt+]`** (next), **`Alt+Ctrl+←/→`** (prev/next xterm `CSI 1;7`). **Cmd+bracket** only works if the terminal is set to send **`Esc`+`[`** / **`Esc`+`]`** (e.g. iTerm “Send Escape Sequence”, Ghostty `super+bracket_*`).
- **Vim-style panel modes** (after you focus a panel, you start in **normal** mode):
  - **`i`** or **`I`** — **insert** mode: keys (including `q`, `Ctrl+C`, etc.) are sent to the panel process, like a typical focused terminal.
  - **`Esc`** — **trickle**: from insert, first **`Esc`** returns to **normal**; from **normal**, **`Esc`** unfocuses the panel. (**`Ctrl+[`** is the same byte as **`Esc`** in a TTY, so it follows the same rule — it cannot mean “previous panel”.)
  - In **normal** mode: **`r`** / **`R`** reloads (restarts) the panel command; **`s`** / **`S`** or **`Ctrl+O`** opens the scrollback log in your editor when scrollback is enabled (see below). Other keys are not sent to the panel.
- When a panel process exits, the panel shows a "Press R to reload" overlay. In **normal** mode, press **`R`** (or **`r`**) to restart the command.
- `q` or `Ctrl+C` — quit and stop all subprocesses (only when no panel is active).

## Scrollback

Each panel's output history is captured to a log file on disk. When the terminal scrolls, lines that leave the top of the screen are appended to the panel's scrollback file. With a focused panel, open that file from **normal** mode with **`s`** / **`S`** or **`Ctrl+O`** (in **insert** mode, **`Ctrl+O`** still opens the log instead of sending it to the process).

The editor is chosen from (in order): the `editor` field in config, the `EDITOR` environment variable, or `vi` as a fallback.

Add an optional `[scrollback]` section to your config to customise behaviour:

```toml
[scrollback]
dir = "~/.cache/muxedo/scrollback"   # where log files are stored (default: OS cache dir)
max_bytes = 1048576                   # max size per panel file in bytes; 0 = unlimited (default: 1 MiB)
editor = "vim"                        # override $EDITOR for scrollback viewing
```

Restarting a panel (`R`) clears its scrollback file. Resizing the terminal resets the internal snapshot used for scroll detection but keeps the existing file.

Note: scrollback capture works best with log-style and shell output. Full-screen TUI programs that redraw the entire screen may not produce meaningful scrollback history.

## Muxedo config

Muxedo also looks for an optional app-level config at `~/.config/muxedo/config.toml`.

If that file is missing, muxedo still starts normally. The process/panel definition does not belong in this file; that stays in the required profile passed via `-profile`.

To generate a complete config file with every app-level option set to its default value, run `./muxedo -dump-config`. This creates the parent directory if needed and refuses to overwrite an existing file unless you also pass `-force`.

You can also add a `[theme]` section to override the UI colors. Hex values are the intended format for themers, and muxedo will let the terminal renderer degrade them automatically on lower-color terminals. ANSI numeric strings still work too.

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
