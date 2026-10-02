//go:build openbsd

package pty

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ioctlGetTermios is the request that reads terminal attributes. Linux spells
// this TCGETS; the BSDs spell it TIOCGETA.
const ioctlGetTermios = unix.TIOCGETA

// ptmget and the _IOC arithmetic live in bsd_ioctl.go, outside the build
// constraints.

// ioctlPTMGET is _IOR('t', 1, struct ptmget). Evaluated, that is 0x40287401,
// which is the value the reference implementation hard-codes.
var ioctlPTMGET = ioR('t', 1, unsafe.Sizeof(ptmget{}))

// openMaster opens the master side of a fresh PTY.
//
// UNVERIFIED: this path has no CI coverage. See DESIGN.md §7.1.
//
// /dev/ptm is only a handle on the clone device: the descriptors that matter
// arrive inside the ioctl's argument, and the file opened here is closed again
// before returning.
func openMaster() (openedPty, error) {
	clone, err := os.OpenFile("/dev/ptm", os.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return openedPty{}, err
	}
	defer clone.Close()

	var arg ptmget
	if err := ioctlPointer(clone, ioctlPTMGET, unsafe.Pointer(&arg)); err != nil {
		return openedPty{}, err
	}

	name := cString(arg.Sn[:])
	if name == "" {
		unix.Close(int(arg.Cfd))
		unix.Close(int(arg.Sfd))
		return openedPty{}, errors.New("pty: PTMGET returned an empty slave name")
	}

	master, err := nonblockingFile(int(arg.Cfd), "/dev/ptm")
	if err != nil {
		unix.Close(int(arg.Sfd))
		return openedPty{}, err
	}

	// The kernel has already opened the slave, so it is used as it stands
	// rather than reopened by name. It has to be made non-blocking for the same
	// reason the master does; os/exec clears the flag again for the child
	// itself, as described in DESIGN.md §3.9.
	slave, err := nonblockingFile(int(arg.Sfd), name)
	if err != nil {
		master.Close()
		return openedPty{}, err
	}

	return openedPty{master: master, slaveName: name, slave: slave}, nil
}
