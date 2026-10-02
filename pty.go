package pty

import (
	"errors"
	"io"
	"os/exec"
)

// ErrUnsupported is returned on platforms this package does not implement.
var ErrUnsupported = errors.New("pty: unsupported platform")

// Size describes the visible display area of a PTY.
//
// PixelWidth and PixelHeight are the dimensions of a single cell in pixels.
// Some systems never set them and simply ignore the values.
type Size struct {
	Rows        uint16
	Cols        uint16
	PixelWidth  uint16
	PixelHeight uint16
}

// DefaultSize is the conventional 24x80 terminal.
var DefaultSize = Size{Rows: 24, Cols: 80}

// System creates PTYs.
//
// Unlike a direct constructor, a System can be selected at runtime, which is
// what allows alternative implementations (for example a serial-backed one) to
// be substituted for the native PTY.
type System interface {
	// OpenPty creates a new PTY with the given window size.
	OpenPty(size Size) (Master, error)
}

// Master is the controlling end of a PTY.
//
// Reading yields output produced by the slave; writing feeds input to it.
// A Master is safe for concurrent use.
//
// Detecting that the session is over differs by platform, and callers should
// not assume the Unix behaviour everywhere. On Unix, reads return io.EOF once
// every slave handle is gone. A ConPTY, by contrast, keeps the console's output
// pipe open until the pseudoconsole itself is closed, so on Windows the
// reliable signal that the child finished is Child.Wait returning, not EOF.
type Master interface {
	io.ReadWriteCloser

	// CloseWrite closes the write direction only.
	//
	// It does not guarantee that the slave immediately observes EOF: a PTY has
	// a single master descriptor, and the slave is only hung up once the last
	// master handle is closed. Use Close to end the session, or write the VEOF
	// character to signal end-of-input.
	CloseWrite() error

	// Resize informs the kernel, and therefore the child, that the window size
	// changed. It does not redraw anything; the child handles that when it
	// receives SIGWINCH.
	Resize(size Size) error

	// Size reports the window size as currently known by the kernel.
	Size() (Size, error)

	// Name returns the name of the slave side of the PTY, for example
	// "/dev/ttys003". On Windows it returns a fixed placeholder.
	Name() string

	// Spawn starts cmd attached to the PTY.
	//
	// On Unix cmd is started through os/exec. On Windows the process is created
	// with CreateProcess, because the standard library cannot attach a process
	// to a ConPTY (golang/go#62708); as a result cmd.Process, cmd.ProcessState,
	// cmd.Cancel and cmd.WaitDelay are not populated by the standard library,
	// and the returned Child must be used to wait for the process instead.
	//
	// The child's standard streams are platform-dependent. On Unix the slave
	// fills in whichever of cmd.Stdin, cmd.Stdout and cmd.Stderr the caller
	// left unset, and the caller's own files are honoured. On Windows the
	// pseudoconsole supplies all three, so any the caller set are ignored.
	Spawn(cmd *exec.Cmd, opts ...SpawnOption) (Child, error)
}

// Child is a process spawned into a PTY.
type Child interface {
	// Wait blocks until the process exits and reports its status.
	Wait() (ExitStatus, error)

	// TryWait polls the process without blocking. The bool reports whether the
	// process has already exited, in which case the ExitStatus is valid.
	TryWait() (ExitStatus, bool, error)

	// PID returns the process identifier, or 0 if not applicable.
	PID() int

	// Kill terminates the process.
	//
	// On Unix this mirrors portable-pty: SIGHUP first, then a grace period of
	// up to five 50ms polls, and only then a forceful kill.
	Kill() error

	// CloneKiller returns a Killer that can be used independently of this
	// Child, so that a signal can be sent from a goroutine other than one
	// blocked in Wait.
	CloneKiller() Killer
}

// Killer terminates a Child.
type Killer interface {
	// Kill terminates the process.
	Kill() error

	// CloneKiller returns an independent Killer with the same target.
	CloneKiller() Killer
}
