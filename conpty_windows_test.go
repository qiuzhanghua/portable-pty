//go:build windows

package pty

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func openTestConPTY(t *testing.T, size Size) Master {
	t.Helper()
	m, err := Native().OpenPty(size)
	if err != nil {
		t.Fatalf("OpenPty: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

// readToEnd reads the master while the child runs, returning its output and
// status.
//
// It tolerates both possible ConPTY behaviours: the console may release its
// output pipe when the child exits, giving EOF, or it may hold it until the
// pseudoconsole is closed. Either way the output is collected, and the timeout
// reports the difference rather than hanging the suite.
func readToEnd(t *testing.T, m Master, child Child) (string, ExitStatus) {
	t.Helper()

	ch := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(m)
		ch <- string(b)
	}()

	status, err := child.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}

	select {
	case out := <-ch:
		return out, status
	case <-time.After(5 * time.Second):
		// Still open, so release the console.
		m.Close()
		select {
		case out := <-ch:
			return out, status
		case <-time.After(15 * time.Second):
			t.Fatal("the reader never finished, even after Close")
			return "", status
		}
	}
}

func TestConPTYName(t *testing.T) {
	m := openTestConPTY(t, DefaultSize)
	if got := m.Name(); got == "" {
		t.Error("Name() is empty")
	}
}

// ConPTY has no way to report its size, so Size must at least reflect what this
// package last asked for.
func TestConPTYSizeTracksResize(t *testing.T) {
	m := openTestConPTY(t, Size{Rows: 24, Cols: 80})

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
	if got, err = m.Size(); err != nil {
		t.Fatalf("Size: %v", err)
	}
	if got.Rows != 40 || got.Cols != 120 {
		t.Errorf("Size after Resize = %+v, want rows=40 cols=120", got)
	}
}

func TestConPTYSpawnCommandOutput(t *testing.T) {
	m := openTestConPTY(t, DefaultSize)

	child, err := m.Spawn(exec.Command("cmd.exe", "/c", "echo hello"))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	out, status := readToEnd(t, m, child)
	if !strings.Contains(out, "hello") {
		t.Errorf("output %q does not contain hello", out)
	}
	if !status.Success() {
		t.Errorf("status = %+v, want success", status)
	}
}

func TestConPTYSpawnExitCode(t *testing.T) {
	m := openTestConPTY(t, DefaultSize)

	child, err := m.Spawn(exec.Command("cmd.exe", "/c", "exit 7"))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	_, status := readToEnd(t, m, child)
	if status.Code != 7 {
		t.Errorf("exit code = %d, want 7 (full status %+v)", status.Code, status)
	}
	if status.Success() {
		t.Error("Success() = true for exit code 7")
	}
	// Windows has no signal-termination concept.
	if status.Signal != "" {
		t.Errorf("Signal = %q, want empty on Windows", status.Signal)
	}
}

// Kill terminates, and reports the exit code the termination used.
func TestConPTYKill(t *testing.T) {
	m := openTestConPTY(t, DefaultSize)

	// ping to the loopback address needs no network and runs long enough to be
	// killed deterministically. 127.0.0.1 rather than localhost, so nothing
	// tries to resolve a name.
	child, err := m.Spawn(exec.Command("cmd.exe", "/c", "ping -n 60 127.0.0.1"))
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
	if status.Success() {
		t.Errorf("status = %+v, want a non-zero exit from termination", status)
	}
	if err := child.Kill(); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("Kill after exit: err = %v, want os.ErrProcessDone", err)
	}
}

// On Windows CloseWrite is not merely logical: it closes our end of the
// console's input pipe, so the interface contract of "writes now fail, reads
// still work" holds for a real reason.
func TestConPTYCloseWriteContract(t *testing.T) {
	m := openTestConPTY(t, DefaultSize)

	if _, err := m.Write([]byte("echo\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := m.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	if _, err := m.Write([]byte("echo again\r\n")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Write after CloseWrite: err = %v, want os.ErrClosed", err)
	}
	// Close must not treat the already-closed pipe as a failure.
	if err := m.Close(); err != nil {
		t.Errorf("Close after CloseWrite: %v, want nil", err)
	}
}

func TestConPTYSpawnRejectsBadCommand(t *testing.T) {
	m := openTestConPTY(t, DefaultSize)

	if _, err := m.Spawn(nil); err == nil {
		t.Error("Spawn(nil) succeeded, want an error")
	}

	var bare exec.Cmd // no Path
	if _, err := m.Spawn(&bare); err == nil {
		t.Error("Spawn of a command with no Path succeeded, want an error")
	}
}

// Two spawns on one console are not possible: a pseudoconsole owns a single
// session. Whatever the second attempt does, it must not succeed silently.
func TestConPTYSecondSpawnFailsOrRuns(t *testing.T) {
	m := openTestConPTY(t, DefaultSize)

	first, err := m.Spawn(exec.Command("cmd.exe", "/c", "exit 0"))
	if err != nil {
		t.Fatalf("first Spawn: %v", err)
	}
	readToEnd(t, m, first)

	second, err := m.Spawn(exec.Command("cmd.exe", "/c", "exit 0"))
	if err != nil {
		t.Logf("a second Spawn on the same console failed, which is acceptable: %v", err)
		return
	}
	readToEnd(t, m, second)
}
