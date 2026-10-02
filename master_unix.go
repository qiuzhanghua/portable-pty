//go:build linux || darwin || freebsd || openbsd || netbsd

package pty

import (
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// unixMaster is the Unix implementation of Master and UnixMaster.
type unixMaster struct {
	// file is the master side, opened with os.OpenFile so that the Go runtime
	// registers it with the netpoller: reads park the goroutine instead of
	// pinning an OS thread, and Close reliably wakes a blocked Read. See
	// DESIGN.md §3.5.
	file *os.File

	// name is the device path of the slave side.
	name string

	mu sync.RWMutex

	// slave is the parent's copy of the slave end. It exists for two reasons:
	// a PTY's window size cannot be set until a slave has been opened at all
	// (TIOCSWINSZ answers ENOTTY on a master whose slave has never existed),
	// and the caller may legitimately Resize or Size before spawning.
	//
	// Spawn gives this copy to the child and then closes it, because holding a
	// slave open in the parent would stop the master from ever observing the
	// session end. spawnSlave reopens one if a later Spawn needs it.
	slave *os.File

	writeErr error
}

var (
	_ Master     = (*unixMaster)(nil)
	_ UnixMaster = (*unixMaster)(nil)
)

func (m *unixMaster) Read(p []byte) (int, error) {
	n, err := m.file.Read(p)
	// Linux reports EIO once every slave handle has been closed, where Darwin
	// reports a clean EOF. Normalise so callers see one behaviour everywhere.
	// See DESIGN.md §3.6 and decision D7.
	if errors.Is(err, syscall.EIO) {
		return n, io.EOF
	}
	return n, err
}

func (m *unixMaster) Write(p []byte) (int, error) {
	m.mu.RLock()
	err := m.writeErr
	m.mu.RUnlock()
	if err != nil {
		return 0, err
	}
	return m.file.Write(p)
}

// CloseWrite implements the logical half-close described on Master.
func (m *unixMaster) CloseWrite() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeErr = os.ErrClosed
	return nil
}

// Close releases the master and, if it is still ours, the slave.
func (m *unixMaster) Close() error {
	m.mu.Lock()
	slave := m.slave
	m.slave = nil
	m.mu.Unlock()

	err := m.file.Close()
	if slave != nil {
		err = errors.Join(err, slave.Close())
	}
	return err
}

func (m *unixMaster) Name() string { return m.name }

func (m *unixMaster) Resize(size Size) error {
	ws := &unix.Winsize{
		Row:    size.Rows,
		Col:    size.Cols,
		Xpixel: size.PixelWidth,
		Ypixel: size.PixelHeight,
	}
	return m.control(func(fd uintptr) error {
		return unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, ws)
	})
}

func (m *unixMaster) Size() (Size, error) {
	var ws *unix.Winsize
	err := m.control(func(fd uintptr) error {
		var err error
		ws, err = unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ)
		return err
	})
	if err != nil {
		return Size{}, err
	}
	return Size{
		Rows:        ws.Row,
		Cols:        ws.Col,
		PixelWidth:  ws.Xpixel,
		PixelHeight: ws.Ypixel,
	}, nil
}

func (m *unixMaster) Termios() (*unix.Termios, error) {
	var termios *unix.Termios
	err := m.control(func(fd uintptr) error {
		var err error
		termios, err = unix.IoctlGetTermios(int(fd), ioctlGetTermios)
		return err
	})
	return termios, err
}

func (m *unixMaster) Pgrp() (int, error) {
	var pgrp int32
	err := m.control(func(fd uintptr) error {
		_, _, errno := unix.Syscall(
			unix.SYS_IOCTL, fd,
			uintptr(unix.TIOCGPGRP),
			uintptr(unsafe.Pointer(&pgrp)),
		)
		if errno != 0 {
			return errno
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int(pgrp), nil
}

func (m *unixMaster) SyscallConn() (syscall.RawConn, error) { return m.file.SyscallConn() }

func (m *unixMaster) control(fn func(fd uintptr) error) error {
	return controlFile(m.file, fn)
}

// spawnSlave hands the slave to a spawned child.
//
// The returned handle becomes the caller's to close; the master keeps no
// reference to it. If an earlier spawn already consumed the copy made by
// OpenPty, a fresh one is opened from the same device path, which remains
// valid for as long as the master lives.
func (m *unixMaster) spawnSlave() (*os.File, error) {
	m.mu.Lock()
	slave := m.slave
	m.slave = nil
	m.mu.Unlock()

	if slave != nil {
		return slave, nil
	}
	return m.openSlave()
}

// openSlave opens the slave device directly.
func (m *unixMaster) openSlave() (*os.File, error) {
	// O_NOCTTY: opening a terminal must not steal our own controlling terminal.
	return os.OpenFile(m.name, os.O_RDWR|syscall.O_NOCTTY, 0)
}

// controlFile runs fn with f's descriptor.
//
// It deliberately goes through SyscallConn rather than (*os.File).Fd(): Fd
// clears O_NONBLOCK, which is shared by every dup of this open file
// description, and would silently degrade this handle and all its siblings to
// blocking mode. See DESIGN.md §8 trap 1.
func controlFile(f *os.File, fn func(fd uintptr) error) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var inner error
	if err := rc.Control(func(fd uintptr) { inner = fn(fd) }); err != nil {
		return err
	}
	return inner
}

// ioctlPointer issues an ioctl whose argument is a pointer.
func ioctlPointer(f *os.File, req uint, arg unsafe.Pointer) error {
	return controlFile(f, func(fd uintptr) error {
		if _, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, uintptr(req), uintptr(arg)); errno != 0 {
			return errno
		}
		return nil
	})
}

// ioctlZero issues an ioctl whose argument is a pointer to a zero int32.
func ioctlZero(f *os.File, req uint) error {
	var zero int32
	return ioctlPointer(f, req, unsafe.Pointer(&zero))
}

// ioctlSetPointerInt issues an ioctl whose argument is a pointer to an int.
func ioctlSetPointerInt(f *os.File, req uint, value int) error {
	return controlFile(f, func(fd uintptr) error {
		return unix.IoctlSetPointerInt(int(fd), req, value)
	})
}

// ioctlNone issues an ioctl that takes no argument.
func ioctlNone(f *os.File, req uint) error {
	return controlFile(f, func(fd uintptr) error {
		if _, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, uintptr(req), 0); errno != 0 {
			return errno
		}
		return nil
	})
}

// cString returns the NUL-terminated string at the start of buf.
func cString(buf []byte) string {
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// openedPty is what opening a pseudo-terminal produces.
type openedPty struct {
	// master is the controlling end.
	master *os.File
	// slaveName is the device path of the other end.
	slaveName string
	// slave is set only where the kernel hands both ends over at once, which
	// OpenBSD's PTMGET does. Reopening the slave by name there would be
	// wasteful, and would briefly leave the terminal with no slave at all.
	slave *os.File
}

// nonblockingFile wraps a raw descriptor that the Go runtime does not know
// about yet.
//
// The descriptor must already be non-blocking: os.NewFile only registers a file
// with the netpoller when it finds O_NONBLOCK set, and a descriptor from
// posix_openpt or PTMGET arrives blocking. Without this, reads on such a master
// would pin an OS thread and Close would not wake them — the very problem
// DESIGN.md §3.5 exists to avoid.
func nonblockingFile(fd int, name string) (*os.File, error) {
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}
