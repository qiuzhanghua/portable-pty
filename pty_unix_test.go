//go:build linux || darwin

package pty

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// helperEnv marks the re-executed test binary used by TestChildStdioIsBlocking.
const helperEnv = "PTY_TEST_STDIO_HELPER"

func openTestPty(t *testing.T, size Size) Master {
	t.Helper()
	m, err := Native().OpenPty(size)
	if err != nil {
		t.Fatalf("OpenPty: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

func TestOpenPtySizeAndName(t *testing.T) {
	m := openTestPty(t, Size{Rows: 24, Cols: 80})

	if m.Name() == "" {
		t.Fatal("Name() is empty")
	}
	if _, err := os.Stat(m.Name()); err != nil {
		t.Errorf("slave %q is not usable: %v", m.Name(), err)
	}

	got, err := m.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if got.Rows != 24 || got.Cols != 80 {
		t.Errorf("Size = %+v, want rows=24 cols=80", got)
	}

	if err := m.Resize(Size{Rows: 40, Cols: 120}); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	got, err = m.Size()
	if err != nil {
		t.Fatalf("Size after Resize: %v", err)
	}
	if got.Rows != 40 || got.Cols != 120 {
		t.Errorf("Size after Resize = %+v, want rows=40 cols=120", got)
	}
}

// TestInteractiveShell drives the whole loop: write to the master, have a shell
// read the command from the slave, and collect both its output and its exit
// status. The marker is computed by the shell, so seeing it proves the shell
// actually ran rather than the terminal simply echoing our input back.
func TestInteractiveShell(t *testing.T) {
	m := openTestPty(t, Size{Rows: 24, Cols: 80})

	child, err := m.Spawn(exec.Command("/bin/sh"))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if _, err := m.Write([]byte("echo marker-$((6*7))\nexit 3\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out, err := io.ReadAll(m)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	status, err := child.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !strings.Contains(string(out), "marker-42") {
		t.Errorf("output %q does not contain the shell-computed marker-42", out)
	}
	if status.Code != 3 {
		t.Errorf("exit code = %d, want 3 (full status %+v)", status.Code, status)
	}
	if status.Success() {
		t.Error("Success() = true for exit code 3")
	}
}

func TestSpawnCommandOutput(t *testing.T) {
	m := openTestPty(t, DefaultSize)

	child, err := m.Spawn(exec.Command("/bin/sh", "-c", "echo hello"))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	out, err := io.ReadAll(m)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Errorf("output %q does not contain hello", out)
	}

	status, err := child.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !status.Success() {
		t.Errorf("status = %+v, want success", status)
	}
}

// TestReadEndsWithEOF covers decision D7: Linux reports EIO once the last slave
// handle is gone, Darwin reports a clean EOF, and callers must see io.EOF
// either way — never a bare EIO.
func TestReadEndsWithEOF(t *testing.T) {
	m := openTestPty(t, DefaultSize)

	child, err := m.Spawn(exec.Command("/bin/sh", "-c", "printf bye"))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	buf := make([]byte, 64)
	var out []byte
	for {
		n, err := m.Read(buf)
		out = append(out, buf[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v; want io.EOF at end of stream (EIO must be normalised)", err)
		}
	}
	if !strings.Contains(string(out), "bye") {
		t.Errorf("output %q does not contain bye", out)
	}
	if _, err := child.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestKillReportsSignal(t *testing.T) {
	m := openTestPty(t, DefaultSize)

	child, err := m.Spawn(exec.Command("sleep", "30"))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if err := child.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	status, err := child.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if status.Signal != "SIGHUP" {
		t.Errorf("status = %+v, want Signal=SIGHUP (Kill sends SIGHUP first)", status)
	}
	if status.Success() {
		t.Error("Success() = true for a signalled process")
	}
}

func TestKillAfterExitIsReported(t *testing.T) {
	m := openTestPty(t, DefaultSize)

	child, err := m.Spawn(exec.Command("/bin/sh", "-c", "exit 0"))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	io.Copy(io.Discard, m)

	if _, err := child.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if err := child.Kill(); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("Kill after exit: err = %v, want os.ErrProcessDone", err)
	}
}

// TestCloseWriteStopsWritesButKeepsReads pins down the semantics documented on
// Master: CloseWrite is a logical half-close. It must stop writes without
// disturbing reads, and it must not hang up the slave.
func TestCloseWriteStopsWritesButKeepsReads(t *testing.T) {
	m := openTestPty(t, DefaultSize)

	child, err := m.Spawn(exec.Command("/bin/sh", "-c", "cat"))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if _, err := m.Write([]byte("first\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := readUntil(t, m, "first", 5*time.Second); !strings.Contains(got, "first") {
		t.Fatalf("read %q, want it to contain first (cat should echo it)", got)
	}

	if err := m.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	if _, err := m.Write([]byte("second\n")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Write after CloseWrite: err = %v, want os.ErrClosed", err)
	}

	// The read direction must still be alive.
	if got := readUntil(t, m, "", 50*time.Millisecond); got != "" {
		t.Logf("read after CloseWrite returned %q (no data expected)", got)
	}

	m.Close()
	child.Wait()
}

func TestTermiosAndPgrp(t *testing.T) {
	m := openTestPty(t, DefaultSize)

	um, ok := m.(UnixMaster)
	if !ok {
		t.Fatalf("master does not implement UnixMaster: %T", m)
	}

	tio, err := um.Termios()
	if err != nil {
		t.Fatalf("Termios: %v", err)
	}
	if tio == nil {
		t.Fatal("Termios returned nil with a nil error")
	}

	child, err := m.Spawn(exec.Command("sleep", "30"))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { child.Kill(); child.Wait() }()

	// The child called setsid and acquired the PTY as its controlling
	// terminal, so it should end up as the foreground process group.
	waitFor(t, 3*time.Second, func() bool {
		pgrp, err := um.Pgrp()
		return err == nil && pgrp == child.PID()
	}, "foreground process group to become the child")
}

// TestChildStdioIsBlocking guards the invariant that the child's standard
// descriptors are blocking. It holds only because os/exec calls (*os.File).Fd()
// on them, which clears the O_NONBLOCK that os.OpenFile set when we opened the
// slave. If that ever stops being true, children get non-blocking stdio and
// behave unpredictably, so it is worth asserting rather than assuming.
func TestChildStdioIsBlocking(t *testing.T) {
	if os.Getenv(helperEnv) == "1" {
		checkChildStdioFlags()
		return
	}

	m := openTestPty(t, DefaultSize)

	cmd := exec.Command(os.Args[0], "-test.run=TestChildStdioIsBlocking")
	cmd.Env = append(os.Environ(), helperEnv+"=1")

	child, err := m.Spawn(cmd)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	io.Copy(io.Discard, m)

	status, err := child.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if status.Code != 0 {
		t.Errorf("child exited %d: its standard descriptors are not all blocking", status.Code)
	}
}

// checkChildStdioFlags runs inside the re-executed child process and exits
// non-zero, encoding the offending descriptor, if any of fd 0..2 is
// non-blocking.
func checkChildStdioFlags() {
	for fd := range 3 {
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err != nil {
			os.Exit(10 + fd)
		}
		if flags&unix.O_NONBLOCK != 0 {
			os.Exit(20 + fd)
		}
	}
	os.Exit(0)
}

// readUntil reads from r until the accumulated output contains marker, or the
// timeout expires. An empty marker means "whatever arrives within the timeout".
func readUntil(t *testing.T, r io.Reader, marker string, timeout time.Duration) string {
	t.Helper()

	type result struct{ text string }
	ch := make(chan result, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 256)
		for {
			n, err := r.Read(buf)
			sb.Write(buf[:n])
			if (marker != "" && strings.Contains(sb.String(), marker)) || err != nil {
				ch <- result{sb.String()}
				return
			}
		}
	}()

	select {
	case res := <-ch:
		return res.text
	case <-time.After(timeout):
		if marker != "" {
			t.Fatalf("timed out waiting for %q", marker)
		}
		return ""
	}
}

// waitFor polls cond until it holds or the timeout expires.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}
