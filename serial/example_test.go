package serial_test

import (
	"io"
	"os"

	"github.com/qiuzhanghua/portable-pty/serial"
)

// Compiled but not run: CI has no serial device. Note that a serial port is a
// pty.Master, so anything written against that interface works with one.
func ExampleOpen() {
	port, err := serial.Open("/dev/ttyUSB0", serial.DefaultConfig()) // 9600 8N1
	if err != nil {
		panic(err)
	}
	defer port.Close()

	// Read blocks until bytes arrive, and fails if the adapter is removed.
	io.Copy(os.Stdout, port)
}
