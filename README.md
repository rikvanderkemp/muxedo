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

A profile defines the global environment, startup commands, and the layout of panel processes:

```toml
# Global working directory fallback (optional; ~ is expanded)
workingdir = "~/code/project"

# Commands to run sequentially before the TUI starts (optional)
[[startup]]
cmd = "docker compose up -d"
# workingdir = "." # optional override for this command

[[startup]]
cmd = "make migrate"

[panel.api]
# Uses global workingdir fallback
cmd = "go run ."

[panel.frontend]
workingdir = "~/code/frontend" # overrides global workingdir
cmd = "npm run dev"
```

- `workingdir` (top-level) — optional global default for all panels and startup commands.
- `[[startup]]` — optional array of commands to execute before the panels start.
  - `cmd` — shell command to run (executed via `sh -c`).
  - `workingdir` — optional working directory for this specific startup command.
- `[panel.<name>]` — each section defines a pane:
  - `workingdir` — working directory for the command.
  - `cmd` — shell command to run (executed via `sh -lc`).
  - `cmd_kill` — optional shell command to run before restarting or exiting the panel.

Panels are arranged in an auto-grid (near-square) layout that fills the terminal and resizes when the window changes.

## Controls

- Left click a panel — activate/focus that panel.
- **Panel numbers** — each pane title shows **`[1] name`**, **`[2] name`**, … (1-based). The status bar shows the active panel the same way.
- **Jump by number** — with **no panel focused** or in **normal** mode (focused, not insert/scroll), press **`1`**–**`9`** to focus that panel (first nine only). In **insert** mode, digits are sent to the process.
- **Grid motion** — in **normal** mode, **`h`** **`j`** **`k`** **`l`** move focus to the adjacent panel in the auto-grid (left / down / up / right). No move if there is no neighbor in that direction. In **scroll** mode, **`j`** / **`k`** still move the scrollback line; use **Esc** to return to normal, then **`hjkl`** to change panels.
- **Vim-style panel modes** (after you focus a panel, you start in **normal** mode):
  - **`i`** or **`I`** — **insert** mode: keys (including `q`, `Ctrl+C`, etc.) are sent to the panel process, like a typical focused terminal.
  - **`z`** or **`Z`** — **scroll** mode: the panel becomes a read-only scrollback viewer with a line cursor and optional mark.
  - **`Esc`** — **trickle**: from insert, first **`Esc`** returns to **normal**; from **normal**, **`Esc`** unfocuses the panel. (**`Ctrl+[`** is the same byte as **`Esc`** in a TTY, so it follows the same rule.)
  - In **normal** mode: **`m`** / **`M`** toggles maximize for the focused panel; **`r`** / **`R`** reloads (restarts) the panel command. Other keys are not sent to the panel.
- In **scroll** mode:
  - **`PgUp`** / **`PgDn`** or mouse wheel — move the viewport.
  - **`j`** / **`k`** or **Up** / **Down** — move the selected line.
  - **`g`** / **`G`** — jump to oldest history / live bottom.
  - **`m`** — toggle a persistent mark on the selected line.
  - **`Esc`** — leave scroll mode and return to normal mode.
- While a panel is maximized, **`hjkl`** and **`1`**–**`9`** keep the single-panel view and switch which panel is shown.
- Pressing **`Esc`** from maximized **normal** mode restores the grid and clears focus.
- When a panel process exits, the panel shows a "Press R to reload" overlay. In **normal** mode, press **`R`** (or **`r`**) to restart the command.
- **`q`** or **`Ctrl+C`** — quit and stop all subprocesses (only when no panel is active).

## Scrollback

Each panel's output history is captured to a log file on disk. When the terminal scrolls, lines that leave the top of the screen are appended to the panel's scrollback file.

Scrollback starts empty on each muxedo launch, so in-panel scrolling only shows the current app run.

Focused panels can also enter **scroll** mode with `**z`** to inspect that history in place. Scroll mode merges the current visible screen with the existing file-backed scrollback, so it works best for shells and log output and remains best-effort for full-screen TUIs.

The editor is no longer used for scrollback viewing.

Add an optional `[scrollback]` section to your config to customise behaviour:

```toml
[scrollback]
dir = "~/.cache/muxedo/scrollback"   # where log files are stored (default: OS cache dir)
max_bytes = 1048576                   # max size per panel file in bytes; 0 = unlimited (default: 1 MiB)
```

Restarting a panel (`R`) clears its scrollback file. Resizing the terminal resets the internal snapshot used for scroll detection but keeps the existing file for the current run.

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

