//go:build !linux && !darwin && !windows

package pty

// Native returns a System whose OpenPty always fails with ErrUnsupported.
//
// freebsd, openbsd, netbsd, solaris, aix and every other GOOS land here for
// now. See DESIGN.md §7 for what is planned and why the BSDs are not claimed
// yet: they need per-OS open/grant/unlock sequences, and this project has no
// way to test them.
func Native() System { return unsupportedSystem{} }

type unsupportedSystem struct{}

func (unsupportedSystem) OpenPty(Size) (Master, error) { return nil, ErrUnsupported }
