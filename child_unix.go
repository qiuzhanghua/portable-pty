//go:build linux || darwin || freebsd || openbsd || netbsd

package pty

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// killGraceAttempts and killGraceInterval mirror portable-pty's kill
	// sequence: SIGHUP, then up to five polls 50ms apart, then SIGKILL.
	killGraceAttempts = 5
	killGraceInterval = 50 * time.Millisecond
)

// unixChild adapts *exec.Cmd to Child.
type unixChild struct {
	cmd *exec.Cmd

	once   sync.Once
	done   chan struct{}
	status ExitStatus
	err    error
}

var _ Child = (*unixChild)(nil)

func newChild(cmd *exec.Cmd) *unixChild {
	return &unixChild{cmd: cmd, done: make(chan struct{})}
}

// await reaps the child exactly once, in the background.
//
// os/exec has no try-wait, so TryWait needs a waiter already running. Doing it
// this way also means the child is reaped even if the caller only ever calls
// Kill, so no zombie is left behind.
func (c *unixChild) await() {
	c.once.Do(func() {
		go func() {
			c.status, c.err = c.reap()
			close(c.done)
		}()
	})
}

func (c *unixChild) reap() (ExitStatus, error) {
	err := c.cmd.Wait()

	// A non-zero exit is reported through ExitStatus rather than as an error,
	// matching portable-pty: ExitStatus.Success carries that information.
	// Only a genuine wait failure is an error.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitStatusFromProcessState(exitErr.ProcessState), nil
	}
	if err != nil {
		return ExitStatus{Code: 1}, err
	}
	return exitStatusFromProcessState(c.cmd.ProcessState), nil
}

// Wait blocks until the child exits.
//
// A non-zero exit is reported as a normal result, not as an error: check
// ExitStatus.Success. An error means the wait itself failed.
func (c *unixChild) Wait() (ExitStatus, error) {
	c.await()
	<-c.done
	return c.status, c.err
}

// TryWait reports whether the child has exited without blocking.
func (c *unixChild) TryWait() (ExitStatus, bool, error) {
	c.await()
	select {
	case <-c.done:
		return c.status, true, c.err
	default:
		return ExitStatus{}, false, nil
	}
}

func (c *unixChild) PID() int {
	if c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

// Kill terminates the child, mirroring portable-pty's sequence.
func (c *unixChild) Kill() error {
	if _, done, _ := c.TryWait(); done {
		return os.ErrProcessDone
	}
	pid := c.PID()
	if pid <= 0 {
		return os.ErrProcessDone
	}

	if err := unix.Kill(pid, unix.SIGHUP); err != nil {
		return err
	}

	// SIGHUP does not guarantee termination, so give the process a grace period
	// to act on it before resorting to SIGKILL.
	for attempt := 0; attempt < killGraceAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(killGraceInterval)
		}
		if _, done, _ := c.TryWait(); done {
			return nil
		}
	}
	return c.cmd.Process.Kill()
}

func (c *unixChild) CloneKiller() Killer { return unixKiller{pid: c.PID()} }

// unixKiller signals a process it does not own.
//
// It sends SIGHUP only, matching portable-pty's ProcessSignaller. It
// deliberately does not implement the grace period: observing exit without
// reaping is not possible once a pid belongs to the Child's waiter, and
// reaping here would break Child.Wait.
type unixKiller struct{ pid int }

var _ Killer = unixKiller{}

func (k unixKiller) Kill() error { return unix.Kill(k.pid, unix.SIGHUP) }

func (k unixKiller) CloneKiller() Killer { return k }

// exitStatusFromProcessState converts a Go process state into an ExitStatus.
func exitStatusFromProcessState(ps *os.ProcessState) ExitStatus {
	if ps == nil {
		return ExitStatus{Code: 1}
	}

	ws, ok := ps.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() {
		code := ps.ExitCode()
		if code < 0 {
			code = 1
		}
		return ExitStatus{Code: code}
	}

	// portable-pty uses strsignal here, whose text is locale-dependent
	// ("Hangup", or a translated string). A SIG* name is deterministic and
	// machine-parseable, so we deliberately diverge.
	name := unix.SignalName(ws.Signal())
	if name == "" {
		name = "Signal " + strconv.Itoa(int(ws.Signal()))
	}
	return ExitStatus{Code: 1, Signal: name}
}
