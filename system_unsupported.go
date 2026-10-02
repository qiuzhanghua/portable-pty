//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !windows

package pty

// Native returns a System whose OpenPty always fails with ErrUnsupported.
//
// solaris, aix, illumos and every other GOOS land here. See DESIGN.md §7.1:
// the three BSDs are implemented but unverified, and this package reports them
// as unsupported nowhere — they are simply not this file's business any more.
func Native() System { return unsupportedSystem{} }

type unsupportedSystem struct{}

func (unsupportedSystem) OpenPty(Size) (Master, error) { return nil, ErrUnsupported }
