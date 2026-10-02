// Package pty provides a cross-platform interface to the pseudo-terminal
// (PTY) facilities of the host system.
//
// It is a Go port of the Rust crate portable-pty, keeping that crate's
// capabilities while following Go conventions. See DESIGN.md in the repository
// root for the full design, the decisions behind it, and what is still
// unverified.
//
// # Platforms
//
// Unix (linux, darwin, freebsd, openbsd, netbsd) and Windows (ConPTY,
// Windows 10 1809 / build 17763 and later) are supported. Other Unix variants
// return ErrUnsupported.
//
// # Handle ownership
//
// On Unix, the master end is opened through os.OpenFile so that the Go runtime
// places it in the netpoller: reads park the goroutine instead of pinning an OS
// thread, and Close reliably wakes a blocked Read.
//
// Do not call (*os.File).Fd() on a PTY master. Fd clears O_NONBLOCK, which is
// shared by every dup of the same open file description, so a single call
// silently degrades every handle to blocking mode and can leave a blocked Read
// stuck forever. Use SyscallConn instead.
//
// # Closing
//
// Closing the master hangs up the slave. CloseWrite exists for parity with
// portable-pty but does not give true half-close semantics on a PTY: the slave
// only sees EOF once the last master handle is closed. Writing the VEOF
// character (usually ^D) is the way to signal end-of-input without hanging up.
package pty
