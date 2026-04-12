// SPDX-License-Identifier: MIT
package ui

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func copyTextToClipboard(text string) error {
	if text == "" {
		return fmt.Errorf("no selection to copy")
	}

	if err := copyTextWithCommand(text); err == nil {
		return nil
	}
	if err := copyTextWithOSC52(os.Stdout, text); err == nil {
		return nil
	}
	return fmt.Errorf("clipboard unavailable")
}

func copyTextWithCommand(text string) error {
	candidates := clipboardCommands()
	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate.name); err != nil {
			continue
		}
		cmd := exec.Command(candidate.name, candidate.args...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no clipboard command succeeded")
}

func clipboardCommands() []struct {
	name string
	args []string
} {
	switch runtime.GOOS {
	case "darwin":
		return []struct {
			name string
			args []string
		}{{name: "pbcopy"}}
	default:
		return []struct {
			name string
			args []string
		}{
			{name: "wl-copy"},
			{name: "xclip", args: []string{"-selection", "clipboard"}},
			{name: "xsel", args: []string{"--clipboard", "--input"}},
		}
	}
}

func copyTextWithOSC52(w io.Writer, text string) error {
	if w == nil {
		return fmt.Errorf("missing osc52 writer")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	_, err := fmt.Fprintf(w, "\x1b]52;c;%s\a", encoded)
	return err
}
