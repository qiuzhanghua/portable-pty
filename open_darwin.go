//go:build darwin

package pty

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ioctlGetTermios is the request that reads terminal attributes. Linux and
// Darwin spell this differently (TCGETS vs TIOCGETA).
const ioctlGetTermios = unix.TIOCGETA

// slaveNameBuffer is the size of the buffer TIOCPTYGNAME fills in. Darwin's
// device names are short, but the ioctl will happily write up to MAXPATHLEN.
const slaveNameBuffer = 128

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

	// Darwin needs both: grantpt() changes the ownership of the slave, and
	// unlockpt() clears the lock that stops it being opened.
	if err := ioctlZero(file, unix.TIOCPTYGRANT); err != nil {
		file.Close()
		return nil, "", err
	}
	if err := ioctlZero(file, unix.TIOCPTYUNLK); err != nil {
		file.Close()
		return nil, "", err
	}
	return file, name, nil
}

// slaveName reports the slave device path via TIOCPTYGNAME.
func slaveName(file *os.File) (string, error) {
	buf := make([]byte, slaveNameBuffer)
	if err := ioctlPointer(file, unix.TIOCPTYGNAME, unsafe.Pointer(&buf[0])); err != nil {
		return "", err
	}
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i]), nil
		}
	}
	return "", errors.New("pty: TIOCPTYGNAME did not return a NUL-terminated name")
}
