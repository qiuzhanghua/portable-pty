//go:build darwin

package pty

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ioctlGetTermios is the request that reads terminal attributes. Linux and the
// BSDs spell this differently (TCGETS vs TIOCGETA).
const ioctlGetTermios = unix.TIOCGETA

// slaveNameBuffer is the size of the buffer TIOCPTYGNAME fills in. Darwin's
// device names are short, but the ioctl will happily write up to MAXPATHLEN.
const slaveNameBuffer = 128

// openMaster opens the master side of a fresh PTY.
func openMaster() (openedPty, error) {
	// os.OpenFile, and not syscall.Open followed by os.NewFile. This is what
	// gets the descriptor registered with the netpoller; see DESIGN.md §3.5.
	file, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return openedPty{}, err
	}

	buf := make([]byte, slaveNameBuffer)
	if err := ioctlPointer(file, unix.TIOCPTYGNAME, unsafe.Pointer(&buf[0])); err != nil {
		file.Close()
		return openedPty{}, err
	}
	name := cString(buf)
	if name == "" {
		file.Close()
		return openedPty{}, errors.New("pty: TIOCPTYGNAME returned an empty name")
	}

	// Darwin needs both: grantpt() changes the ownership of the slave, and
	// unlockpt() clears the lock that stops it being opened.
	if err := ioctlZero(file, unix.TIOCPTYGRANT); err != nil {
		file.Close()
		return openedPty{}, err
	}
	if err := ioctlZero(file, unix.TIOCPTYUNLK); err != nil {
		file.Close()
		return openedPty{}, err
	}

	return openedPty{master: file, slaveName: name}, nil
}
