//go:build linux || darwin || freebsd || openbsd || netbsd

package pty

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// UnixMaster is the Unix-specific view of a PTY master.
//
// Windows has no equivalent: ConPTY is a pseudoconsole plus two pipes and
// exposes neither terminal attributes nor a process group.
type UnixMaster interface {
	Master

	// Termios returns the terminal attributes of the master.
	Termios() (*unix.Termios, error)

	// Pgrp returns the foreground process group of the PTY.
	Pgrp() (int, error)

	// SyscallConn returns a raw connection for issuing ioctls.
	//
	// Prefer this over (*os.File).Fd(): Fd clears O_NONBLOCK, which is shared
	// by every dup of the same open file description, and will degrade all
	// handles of this PTY to blocking mode.
	SyscallConn() (syscall.RawConn, error)
}
