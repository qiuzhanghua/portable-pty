//go:build freebsd

package pty

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ioctlGetTermios is the request that reads terminal attributes. Linux spells
// this TCGETS; the BSDs spell it TIOCGETA.
const ioctlGetTermios = unix.TIOCGETA

// fiodgnameArg and the _IOC arithmetic live in bsd_ioctl.go, outside the
// build constraints, so their layout is checked by tests that run everywhere.

// ioctlFIODGNAME is _IOW('f', 120, struct fiodgname_arg), the request that
// reports the device name behind a descriptor.
var ioctlFIODGNAME = ioW('f', 120, unsafe.Sizeof(fiodgnameArg{}))

// freebsdNameBuffer is deliberately larger than any device name FreeBSD
// produces. SPECNAMELEN differs between architectures (0x3f on most, 0xff on
// arm64) and the kernel writes only as many bytes as the name needs, so sizing
// generously avoids having to care.
const freebsdNameBuffer = 256

// openMaster opens the master side of a fresh PTY.
//
// UNVERIFIED: this path has no CI coverage. See DESIGN.md §7.1.
func openMaster() (openedPty, error) {
	// FreeBSD has posix_openpt(2) rather than a /dev/ptmx.
	fd, _, errno := unix.Syscall(unix.SYS_POSIX_OPENPT, uintptr(unix.O_RDWR|unix.O_CLOEXEC), 0, 0)
	if errno != 0 {
		return openedPty{}, errno
	}

	master, err := nonblockingFile(int(fd), "/dev/ptmx")
	if err != nil {
		return openedPty{}, err
	}

	// ptsname(3) refuses to name anything that is not a pty master, and this is
	// the same check.
	if err := ioctlNone(master, unix.TIOCPTMASTER); err != nil {
		master.Close()
		return openedPty{}, err
	}

	// The name comes back relative to /dev, for example "pts/0".
	buf := make([]byte, freebsdNameBuffer)
	arg := fiodgnameArg{Len: int32(len(buf)), Buf: &buf[0]}
	if err := ioctlPointer(master, ioctlFIODGNAME, unsafe.Pointer(&arg)); err != nil {
		master.Close()
		return openedPty{}, err
	}

	name := cString(buf)
	if name == "" {
		master.Close()
		return openedPty{}, errors.New("pty: FIODGNAME returned an empty name")
	}

	return openedPty{master: master, slaveName: "/dev/" + name}, nil
}
