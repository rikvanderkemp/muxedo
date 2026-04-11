# Objective
Modify process termination behavior so that `ctrl+c` is never passed to the terminal by the user in insert mode. Processes can only be stopped manually via the 'x' shortcut in NORMAL mode. Stopping a process this way will execute its `cmd_kill` command (if configured), display a progress dialog, send a proper termination sequence (Ctrl+C, then SIGKILL if needed), and then deactivate the window.

# Key Files & Context
- `internal/ui/model.go`: Update input handling, UI state, and rendering to support single-panel process termination and ignore `ctrl+c` in insert mode.
- `internal/process/runner.go`: Update `Stop()` method to explicitly send a `Ctrl+c` byte to the pseudo-terminal and a `SIGKILL` to ensure the process quits properly.

# Implementation Steps
1. **Prevent `ctrl+c` Passthrough (`model.go`)**:
   - In the `Update()` method, within the `m.panelInsertMode` block, explicitly check if `msg.String() == "ctrl+c"`.
   - If true, return `m, nil` immediately to prevent `ctrl+c` from being sent to the running process by the user.

2. **UI State Update (`model.go`)**:
   - Add new fields to `Model`: `killingPanel bool`, `killingPanelIdx int`, and `killStatus string`.

3. **Handle 'x' Shortcut (`model.go`)**:
   - In `Update()`, within the block handling `msg.Type == tea.KeyRunes` for an active, running panel (when not in insert or scroll mode), add handling for `'x'` and `'X'`.
   - When pressed:
     - Set `m.killingPanel = true` and `m.killingPanelIdx = m.activePanel`.
     - Set `m.killStatus = fmt.Sprintf("exiting panel %s....", p.Name)`.
     - Return `m, killPanelCmd(m.activePanel, p)`.

4. **Coordinate Single Kill Progress (`model.go`)**:
   - Near the top of `Update()`, add a block to handle messages when `m.killingPanel` is true.
   - When an `exitProgressMsg` is received for `m.killingPanelIdx`:
     - Reset `m.killingPanel = false` and `m.killingPanelIdx = -1`.
     - Set `m.activePanel = -1` (deactivate window).
     - Return `m, nil`.

5. **Render Single Kill Dialog (`model.go`)**:
   - Update `wrapExiting()` to also render the overlay dialog if `m.killingPanel` is true.
   - If `m.killingPanel` is true, the dialog content should be `m.killStatus` instead of the joined `m.exitStatuses`.

6. **Graceful Process Termination (`runner.go`)**:
   - Modify the `Stop()` method of `Panel` to ensure the process quits properly.
   - If `p.ptmx` is not nil, write the `Ctrl+c` byte sequence (`\x03`) to it to gracefully terminate terminal-bound applications.
   - If `p.cmd.Process` exists, immediately attempt to send `os.Interrupt` (SIGINT).
   - If the process does not terminate quickly after the `os.Interrupt` (implement a brief timeout/wait), send `os.Kill` (SIGKILL) to forcefully quit it.
   - Ensure `p.ptmx.Close()` is called after these signals, followed by cleaning up `p.cmd`.

# Verification
- Enter insert mode and press `ctrl+c`; verify it does not kill the running process.
- In NORMAL mode with an active, running panel, press `x`; verify a dialog appears, the process stops, and the panel is deselected (`activePanel` becomes `-1`).
- Verify global quit via `q` (with no active panels) still functions correctly.
- Ensure stopped processes don't linger in the background and are successfully killed via the `Ctrl+c` byte and/or `SIGKILL`.