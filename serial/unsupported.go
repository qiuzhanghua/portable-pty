//go:build !(linux || darwin || freebsd || openbsd || windows || js)

package serial

import (
	"fmt"

	"github.com/qiuzhanghua/portable-pty"
)

// Open always fails with pty.ErrUnsupported.
//
// go.bug.st/serial implements linux, darwin, freebsd, openbsd and windows only.
// Excluding this package elsewhere would leave an importer with "build
// constraints exclude all Go files", so instead it exists everywhere and
// reports the limitation the way the core package does for platforms it does
// not implement.
//
// netbsd and dragonfly are in that group only because of an inconsistency
// between two build tags upstream, which looks like an oversight rather than a
// limitation; DESIGN.md §10.9 has the evidence, a ready-to-file report, and
// what to change here once it is fixed.
func Open(port string, _ Config) (pty.Master, error) {
	return nil, fmt.Errorf("serial: %s: %w", port, pty.ErrUnsupported)
}

// System returns a pty.System whose OpenPty always fails with
// pty.ErrUnsupported.
func System(port string, cfg Config) pty.System {
	return unsupportedSystem{port: port, cfg: cfg}
}

type unsupportedSystem struct {
	port string
	cfg  Config
}

func (s unsupportedSystem) OpenPty(pty.Size) (pty.Master, error) { return Open(s.port, s.cfg) }
