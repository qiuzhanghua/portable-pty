// Package serial exposes a serial port as a pty.Master, so that a serial line
// and a pseudo-terminal can be driven by the same code.
//
// It is a port of portable-pty's serial module. It lives in its own package
// rather than in pty so that importing pty does not pull in a serial-port
// dependency that most callers never need; Rust's crate layout cannot express
// that, but Go's package layout can.
//
// A serial line has no process behind it, so Master.Spawn reports
// pty.ErrNoProcess rather than inventing one. go.bug.st/serial implements
// linux, darwin, freebsd, openbsd and windows; on any other platform Open
// reports pty.ErrUnsupported. See DESIGN.md §10.7 for the rest, and §10.9 for
// the upstream gap that keeps netbsd in that group.
package serial

// Parity is a serial port's parity-checking mode.
type Parity int

const (
	// ParityNone disables parity checking. This is portable-pty's default.
	ParityNone Parity = iota
	// ParityOdd enables odd parity.
	ParityOdd
	// ParityEven enables even parity.
	ParityEven
)

// StopBits is the number of stop bits a serial port sends.
type StopBits int

const (
	// StopBitsOne sends one stop bit. This is portable-pty's default.
	StopBitsOne StopBits = iota
	// StopBitsTwo sends two stop bits.
	StopBitsTwo
)

// Config describes a serial port's line settings.
type Config struct {
	// BaudRate is the bit rate. Required.
	BaudRate int
	// DataBits is the character size: 5, 6, 7 or 8.
	DataBits int
	// Parity selects parity checking.
	Parity Parity
	// StopBits selects the number of stop bits.
	StopBits StopBits
}

// DefaultConfig returns portable-pty's defaults: 9600 baud, 8 data bits, no
// parity, one stop bit.
//
// portable-pty additionally defaults to XON/XOFF flow control.
// go.bug.st/serial exposes no flow-control setting, so that cannot be
// reproduced; there is deliberately no field for it rather than one that
// silently does nothing.
func DefaultConfig() Config {
	return Config{
		BaudRate: 9600,
		DataBits: 8,
		Parity:   ParityNone,
		StopBits: StopBitsOne,
	}
}
