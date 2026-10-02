//go:build netbsd

package pty

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// ioctlGetTermios is the request that reads terminal attributes. Linux spells
// this TCGETS; the BSDs spell it TIOCGETA.
const ioctlGetTermios = unix.TIOCGETA

// openMaster opens the master side of a fresh PTY.
//
// UNVERIFIED: this path has no CI coverage. See DESIGN.md §7.1.
func openMaster() (openedPty, error) {
	file, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return openedPty{}, err
	}

	// NetBSD has no TIOCSPTLCK. The slave is released by granting it, and
	// unlockpt() there is a no-op, so neither has a counterpart here.
	if err := ioctlSetPointerInt(file, unix.TIOCGRANTPT, 0); err != nil {
		file.Close()
		return openedPty{}, err
	}

	// TIOCPTSNAME fills in a ptmget whose Sn field holds the slave's path.
	var name string
	err = controlFile(file, func(fd uintptr) error {
		ptm, err := unix.IoctlGetPtmget(int(fd), unix.TIOCPTSNAME)
		if err != nil {
			return err
		}
		name = cString(ptm.Sn[:])
		return nil
	})
	if err != nil {
		file.Close()
		return openedPty{}, err
	}
	if name == "" {
		file.Close()
		return openedPty{}, errors.New("pty: TIOCPTSNAME returned an empty name")
	}

	return openedPty{master: file, slaveName: name}, nil
}
