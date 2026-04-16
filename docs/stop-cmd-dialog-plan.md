# Stop Panel Behavior

This note replaces the earlier implementation plan that used to live here.

The current UX is:

- `Ctrl+C` is ignored while a focused panel is in insert mode.
- `x` in normal mode stops the focused panel.
- If a panel defines `kill_program` / `kill_args` or `shell_kill`, muxedo runs that kill command before tearing the panel down.
- When no panel is focused, `q` and `Ctrl+C` still trigger the global quit flow.

The README documents the user-facing shortcut set. Keep this file short unless the stop flow changes again.
