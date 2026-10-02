package pty_test

import (
	"fmt"
	"io"
	"os"

	pty "github.com/qiuzhanghua/portable-pty"
)

// The examples below deliberately carry no // Output: comment, so the testing
// package compiles them without running them: spawning a shell and printing a
// tty path cannot produce stable output across platforms. Compiling them is the
// point. README.md shows the same code, and this is what keeps the API usage in
// it honest.

func Example() {
	// Allocate a PTY.
	m, err := pty.Native().OpenPty(pty.DefaultSize)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open pty:", err)
		return
	}
	defer m.Close()

	// Spawn a command on it; the PTY becomes its controlling terminal.
	child, err := m.Spawn(pty.Command("sh", "-c", "printf 'the child sees '; tty"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "spawn:", err)
		return
	}

	// Drain the PTY while the child runs, the way a terminal would.
	done := make(chan struct{})
	go func() {
		io.Copy(os.Stdout, m)
		close(done)
	}()

	status, err := child.Wait()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wait:", err)
		return
	}
	<-done

	fmt.Printf("\n[%s]\n", status)
}

func ExampleSystem_OpenPty() {
	m, err := pty.Native().OpenPty(pty.Size{Cols: 80, Rows: 24})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	defer m.Close()

	// /dev/pts/3 on Linux, /dev/ttys003 on macOS, or the placeholder "conpty"
	// on Windows, where there is no slave device to name.
	fmt.Println(m.Name())

	if err := m.Resize(pty.Size{Cols: 120, Rows: 40}); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func ExampleCommand() {
	// Command inherits an environment with SHELL set and starts in the home
	// directory; EnvSet replaces or appends a single variable.
	cmd := pty.Command("sh", "-c", "echo $GREETING")
	cmd.Env = pty.EnvSet(cmd.Env, "GREETING", "hello")

	fmt.Println(cmd.Args)
}
