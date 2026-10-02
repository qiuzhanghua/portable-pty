//go:build windows

package pty

// Native returns a System backed by Windows pseudo consoles (ConPTY).
//
// ConPTY needs Windows 10 version 1809 (build 17763) or newer. On older
// systems CreatePseudoConsole does not exist and OpenPty reports whatever the
// loader returns for it.
func Native() System { return conPtySystem{} }

type conPtySystem struct{}

// OpenPty creates a pseudoconsole. The pipes and the console are created now;
// the process that uses them is created by Master.Spawn.
//
// As on Unix, a zero Size really does mean a 0x0 console; pass DefaultSize
// unless you have better information.
func (conPtySystem) OpenPty(size Size) (Master, error) {
	console, err := newConPTY(windowsConPTYHost{}, size)
	if err != nil {
		return nil, err
	}
	return &conptyMaster{console: console, size: size}, nil
}
