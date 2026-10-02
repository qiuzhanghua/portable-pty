//go:build linux || darwin || freebsd || openbsd || netbsd

package pty

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

// Spawn starts cmd attached to the PTY.
//
// On Unix this can lean on os/exec, because the standard library can already
// create a session and acquire a controlling terminal. Windows has no such
// luxury and needs its own CreateProcess (see DESIGN.md §3.2).
func (m *unixMaster) Spawn(cmd *exec.Cmd, opts ...SpawnOption) (Child, error) {
	if cmd == nil {
		return nil, errors.New("pty: nil command")
	}
	cfg := applySpawnOptions(opts)

	// Ctty indexes ProcAttr.Files, i.e. the child's descriptors 0, 1 and 2, so
	// we have to remember which slot ended up holding the slave.
	ctty := -1
	if cmd.Stdin == nil || cmd.Stdout == nil || cmd.Stderr == nil {
		slave, err := m.spawnSlave()
		if err != nil {
			return nil, fmt.Errorf("pty: open slave %s: %w", m.name, err)
		}
		// The child inherits its own description of the slave across the fork.
		// Keeping ours open would stop the master from ever observing the
		// slave go away, and the session would never appear to end.
		defer slave.Close()

		if cmd.Stdin == nil {
			cmd.Stdin = slave
			ctty = 0
		}
		if cmd.Stdout == nil {
			cmd.Stdout = slave
			if ctty < 0 {
				ctty = 1
			}
		}
		if cmd.Stderr == nil {
			cmd.Stderr = slave
			if ctty < 0 {
				ctty = 2
			}
		}
	}

	attr := cmd.SysProcAttr
	if attr == nil {
		attr = &syscall.SysProcAttr{}
		cmd.SysProcAttr = attr
	}
	// A new session is a precondition for acquiring a controlling terminal.
	attr.Setsid = true
	if cfg.controllingTTY && ctty >= 0 {
		attr.Setctty = true
		attr.Ctty = ctty
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("pty: start %s: %w", cmd.Path, err)
	}
	return newChild(cmd), nil
}
