// SPDX-License-Identifier: MIT
package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// CommandSpec describes either a direct exec invocation or an explicit shell command.
type CommandSpec struct {
	Program string
	Args    []string
	// Shell, if set, is executed via /bin/sh -c or /bin/sh -lc (see Build).
	// It must come only from trusted configuration; it is not sanitized.
	Shell string
}

// Validate ensures the command is well-formed.
func (c CommandSpec) Validate(label string) error {
	hasProgram := c.Program != ""
	hasShell := c.Shell != ""

	switch {
	case hasProgram && hasShell:
		return fmt.Errorf("%s: specify exactly one of program or shell", label)
	case !hasProgram && !hasShell:
		return fmt.Errorf("%s: specify exactly one of program or shell", label)
	case !hasProgram && len(c.Args) > 0:
		return fmt.Errorf("%s: args requires program", label)
	default:
		return nil
	}
}

// Build creates an *exec.Cmd from the command spec.
func (c CommandSpec) Build(dir string, loginShell bool) (*exec.Cmd, error) {
	if err := c.Validate("command"); err != nil {
		return nil, err
	}

	var cmd *exec.Cmd
	if c.Shell != "" {
		shellArgs := []string{"-c", c.Shell}
		if loginShell {
			shellArgs = []string{"-lc", c.Shell}
		}
		cmd = exec.Command("sh", shellArgs...)
	} else {
		cmd = exec.Command(c.Program, c.Args...)
	}

	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	return cmd, nil
}

// mustBuild is a convenience for tests that expect a valid command spec.
func (c CommandSpec) mustBuild(dir string, loginShell bool) *exec.Cmd {
	cmd, err := c.Build(dir, loginShell)
	if err != nil {
		panic(err)
	}
	return cmd
}

// IsZero reports whether the command spec is unset.
func (c CommandSpec) IsZero() bool {
	return c.Program == "" && len(c.Args) == 0 && c.Shell == ""
}

// ErrEmptyCommand reports that a command spec was left unset.
var ErrEmptyCommand = errors.New("empty command")
