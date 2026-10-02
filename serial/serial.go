//go:build linux || darwin || freebsd || openbsd || windows || js

package serial

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	bugst "go.bug.st/serial"

	"github.com/qiuzhanghua/portable-pty"
)

// The build constraint above mirrors what go.bug.st/serial actually implements,
// plus js, where it compiles though a serial port is meaningless. netbsd,
// dragonfly, solaris, aix, illumos and plan9 are not implemented by that
// library; unsupported.go covers those, so that this package always exists and
// reports pty.ErrUnsupported rather than vanishing and leaving an importer with
// "build constraints exclude all Go files".

// serialReadTimeout mirrors portable-pty. It has to be short: on Windows a long
// read timeout blocks a concurrent write from making progress, which matters
// when one goroutine is reading while another occasionally writes.
//
// There is no counterpart for writes: go.bug.st/serial offers no write timeout.
const serialReadTimeout = 50 * time.Millisecond

// System returns a pty.System that opens the named serial port.
//
// The pty.Size passed to OpenPty is ignored, because a serial line has no
// notion of a window size.
func System(port string, cfg Config) pty.System {
	return serialSystem{port: port, cfg: cfg}
}

type serialSystem struct {
	port string
	cfg  Config
}

func (s serialSystem) OpenPty(pty.Size) (pty.Master, error) { return Open(s.port, s.cfg) }

// Open opens the named serial port as a pty.Master.
//
// A zero Config means DefaultConfig, since a config with no baud rate could
// never be opened anyway.
func Open(port string, cfg Config) (pty.Master, error) {
	if port == "" {
		return nil, errors.New("serial: empty port name")
	}
	if cfg == (Config{}) {
		cfg = DefaultConfig()
	}

	handle, err := bugst.Open(port, &bugst.Mode{
		BaudRate: cfg.BaudRate,
		DataBits: cfg.DataBits,
		Parity:   toParity(cfg.Parity),
		StopBits: toStopBits(cfg.StopBits),
	})
	if err != nil {
		return nil, fmt.Errorf("serial: open %s: %w", port, err)
	}

	if err := handle.SetReadTimeout(serialReadTimeout); err != nil {
		handle.Close()
		return nil, fmt.Errorf("serial: set read timeout on %s: %w", port, err)
	}

	return &master{port: handle, name: port, cfg: cfg}, nil
}

// master is the pty.Master implementation for a serial port.
type master struct {
	port bugst.Port
	name string
	cfg  Config

	mu       sync.RWMutex
	writeErr error
}

var _ pty.Master = (*master)(nil)

// Read blocks until at least one byte arrives.
//
// The underlying library reports a read timeout as a zero-length read with a
// nil error. Passing that on would look like EOF to io.ReadAll and io.Copy,
// truncating the stream, so it is treated as "nothing yet" and retried. That
// also means a Read parks until data arrives, or until Close makes it fail.
func (m *master) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		n, err := m.port.Read(p)
		if err != nil {
			return n, err
		}
		if n > 0 {
			return n, nil
		}
	}
}

func (m *master) Write(p []byte) (int, error) {
	m.mu.RLock()
	err := m.writeErr
	m.mu.RUnlock()
	if err != nil {
		return 0, err
	}
	return m.port.Write(p)
}

// CloseWrite marks the write direction closed.
//
// Unlike a pseudo-terminal there is nothing to shut down: a serial line has no
// half-close and no EOF to propagate, so this is only a local convention that
// makes later writes fail. Reads are unaffected.
func (m *master) CloseWrite() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeErr = os.ErrClosed
	return nil
}

func (m *master) Close() error { return m.port.Close() }

func (m *master) Name() string { return m.name }

// Resize does nothing and reports success: a serial line has no window size.
func (m *master) Resize(pty.Size) error { return nil }

// Size reports the default size. portable-pty returns PtySize::default() for
// serial ports, and there is nothing better to report.
func (m *master) Size() (pty.Size, error) { return pty.DefaultSize, nil }

// Spawn always fails with pty.ErrNoProcess.
//
// portable-pty instead hands back a dummy Child whose wait polls carrier detect
// until the device errors, so that a removed USB adapter looks like a process
// exiting. That is a liveness check wearing a process's clothes, and a caller
// gets the same signal more directly by reading: a removed adapter makes Read
// fail. Inventing a fake child would also put the error far from its cause.
func (m *master) Spawn(*exec.Cmd, ...pty.SpawnOption) (pty.Child, error) {
	return nil, fmt.Errorf("serial: %s: %w", m.name, pty.ErrNoProcess)
}

func toParity(p Parity) bugst.Parity {
	switch p {
	case ParityOdd:
		return bugst.OddParity
	case ParityEven:
		return bugst.EvenParity
	default:
		return bugst.NoParity
	}
}

func toStopBits(s StopBits) bugst.StopBits {
	if s == StopBitsTwo {
		return bugst.TwoStopBits
	}
	return bugst.OneStopBit
}
