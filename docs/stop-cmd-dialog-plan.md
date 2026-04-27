# Stop Panel Behavior

This note replaces the earlier implementation plan that used to live here.

The current UX is:

- `Ctrl+C` is ignored while a focused panel is in insert mode.
- `x` in normal mode stops the focused panel.
- Per-panel kill commands (`shell_kill`, `kill_program`, `kill_args`) removed. Use global `[[teardown]]` steps for shutdown automation.
- When no panel is focused, `q` and `Ctrl+C` still trigger the global quit flow.

The README documents the user-facing shortcut set. Keep this file short unless the stop flow changes again.
