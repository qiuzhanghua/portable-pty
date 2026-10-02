//go:build linux

package pty

import (
	"os"
	"strconv"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ioctlGetTermios is the request that reads terminal attributes. Linux and the
// BSDs spell this differently (TCGETS vs TIOCGETA).
const ioctlGetTermios = unix.TCGETS

// openMaster opens the master side of a fresh PTY.
func openMaster() (openedPty, error) {
	// os.OpenFile, and not syscall.Open followed by os.NewFile. This is what
	// gets the descriptor registered with the netpoller; see DESIGN.md §3.5.
	file, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return openedPty{}, err
	}

	// TIOCGPTN reports the device number rather than a name.
	var n uint32
	if err := ioctlPointer(file, unix.TIOCGPTN, unsafe.Pointer(&n)); err != nil {
		file.Close()
		return openedPty{}, err
	}

	// Clear the PTY lock so that the slave can be opened.
	if err := ioctlZero(file, unix.TIOCSPTLCK); err != nil {
		file.Close()
		return openedPty{}, err
	}

	return openedPty{
		master:    file,
		slaveName: "/dev/pts/" + strconv.FormatUint(uint64(n), 10),
	}, nil
}
