//go:build linux

package pty

import (
	"os"
	"strconv"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ioctlGetTermios is the request that reads terminal attributes. Linux and
// Darwin spell this differently (TCGETS vs TIOCGETA).
const ioctlGetTermios = unix.TCGETS

// openMaster opens the master side of a fresh PTY and returns it together with
// the path of the matching slave.
func openMaster() (*os.File, string, error) {
	// os.OpenFile, and not syscall.Open followed by os.NewFile. This is what
	// gets the descriptor registered with the netpoller; see DESIGN.md §3.5.
	file, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, "", err
	}

	name, err := slaveName(file)
	if err != nil {
		file.Close()
		return nil, "", err
	}

	// Clear the PTY lock so that the slave can be opened.
	if err := ioctlZero(file, unix.TIOCSPTLCK); err != nil {
		file.Close()
		return nil, "", err
	}
	return file, name, nil
}

// slaveName reports the slave device path via TIOCGPTN, which returns the
// device minor number rather than a string.
func slaveName(file *os.File) (string, error) {
	var n uint32
	if err := ioctlPointer(file, unix.TIOCGPTN, unsafe.Pointer(&n)); err != nil {
		return "", err
	}
	return "/dev/pts/" + strconv.FormatUint(uint64(n), 10), nil
}
