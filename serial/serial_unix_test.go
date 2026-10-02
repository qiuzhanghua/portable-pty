//go:build linux || darwin

package serial

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/qiuzhanghua/portable-pty"
)

// fakeDevice returns a pty whose slave end stands in for a serial port.
//
// On the systems this test runs on, a serial library will open a pty slave and
// exchange bytes over it, which is the only way to exercise this code without
// hardware. GetModemStatusBits does not work on a pty, which is why nothing
// here depends on carrier detect.
func fakeDevice(t *testing.T) pty.Master {
	t.Helper()
	device, err := pty.Native().OpenPty(pty.DefaultSize)
	if err != nil {
		t.Fatalf("OpenPty: %v", err)
	}
	t.Cleanup(func() { device.Close() })
	return device
}

func readExactly(t *testing.T, r io.Reader, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatalf("reading %d bytes: %v", n, err)
	}
	return string(buf)
}

func TestSystemOpensPortAndTransfersBothWays(t *testing.T) {
	device := fakeDevice(t)

	// Opened through the System abstraction, which is what M5 is for. The size
	// is ignored.
	port, err := System(device.Name(), DefaultConfig()).OpenPty(pty.Size{Rows: 1, Cols: 1})
	if err != nil {
		t.Fatalf("OpenPty: %v", err)
	}
	defer port.Close()

	if _, err := port.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := readExactly(t, device, len("hello")); got != "hello" {
		t.Errorf("the device received %q, want %q", got, "hello")
	}

	if _, err := device.Write([]byte("world")); err != nil {
		t.Fatalf("device write: %v", err)
	}
	if got := readExactly(t, port, len("world")); got != "world" {
		t.Errorf("the port received %q, want %q", got, "world")
	}
}

func TestNameIsThePortPath(t *testing.T) {
	device := fakeDevice(t)

	port, err := Open(device.Name(), DefaultConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer port.Close()

	if got := port.Name(); got != device.Name() {
		t.Errorf("Name() = %q, want %q", got, device.Name())
	}
}

// CloseWrite can only be a local convention here: a serial line has no
// half-close. Writes must stop and reads must keep working.
func TestCloseWriteStopsWritesButNotReads(t *testing.T) {
	device := fakeDevice(t)

	port, err := Open(device.Name(), DefaultConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer port.Close()

	if _, err := port.Write([]byte("first")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := port.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	if _, err := port.Write([]byte("second")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Write after CloseWrite = %v, want os.ErrClosed", err)
	}

	if _, err := device.Write([]byte("still here")); err != nil {
		t.Fatalf("device write: %v", err)
	}
	if got := readExactly(t, port, len("still here")); got != "still here" {
		t.Errorf("after CloseWrite the port received %q, want %q", got, "still here")
	}
}

// The library reports a read timeout as a zero-length read with a nil error.
// Passing that through would look like EOF to io.ReadAll and truncate the
// stream, so Read must wait instead — and must not return early.
func TestReadBlocksUntilDataArrives(t *testing.T) {
	device := fakeDevice(t)

	port, err := Open(device.Name(), DefaultConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer port.Close()

	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 16)
		n, _ := port.Read(buf)
		got <- string(buf[:n])
	}()

	// Well past the 50ms read timeout, so several timeouts must have elapsed.
	time.Sleep(300 * time.Millisecond)
	select {
	case s := <-got:
		t.Fatalf("Read returned %q before any data arrived: a read timeout is being reported as input", s)
	default:
	}

	if _, err := device.Write([]byte("late")); err != nil {
		t.Fatalf("device write: %v", err)
	}
	select {
	case s := <-got:
		if s != "late" {
			t.Errorf("Read returned %q, want %q", s, "late")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read never returned after data arrived")
	}
}

func TestCloseUnblocksAReader(t *testing.T) {
	device := fakeDevice(t)

	port, err := Open(device.Name(), DefaultConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 8)
		_, err := port.Read(buf)
		done <- err
	}()

	time.Sleep(150 * time.Millisecond)
	if err := port.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Error("Read returned a nil error after Close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not unblock the reader")
	}
}

// Every documented line setting must be accepted, including the zero Config
// that stands for the defaults.
func TestOpenAcceptsTheDocumentedSettings(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"defaults", Config{}},
		{"1200-8N1", Config{BaudRate: 1200, DataBits: 8, Parity: ParityNone, StopBits: StopBitsOne}},
		{"115200-7E2", Config{BaudRate: 115200, DataBits: 7, Parity: ParityEven, StopBits: StopBitsTwo}},
		{"57600-8O1", Config{BaudRate: 57600, DataBits: 8, Parity: ParityOdd, StopBits: StopBitsOne}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device := fakeDevice(t)

			port, err := Open(device.Name(), tc.cfg)
			if err != nil {
				t.Fatalf("Open(%+v): %v", tc.cfg, err)
			}
			port.Close()
		})
	}
}

func TestOpenMissingPortFails(t *testing.T) {
	if _, err := Open("/dev/pty-nonexistent-serial-port", DefaultConfig()); err == nil {
		t.Error("opening a missing port succeeded, want an error")
	}
}
