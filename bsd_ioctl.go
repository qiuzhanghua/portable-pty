package pty

// This file holds the two BSD ioctl pieces that are pure data: the _IOC request
// arithmetic and the argument structs it sizes. Neither needs a platform type,
// so they live outside the build constraints and their layout can be checked by
// tests that run on every platform.
//
// That matters more than it looks. The BSD paths themselves cannot be exercised
// here — there is no FreeBSD, OpenBSD or NetBSD runner in CI — but the part
// most likely to be quietly wrong is the layout and the arithmetic, because a
// mistake there produces a request number the kernel does not recognise and a
// failure that only appears on the machine running it.

// The BSD _IOC macros from <sys/ioccom.h>.
//
// Request numbers encode the size of the argument struct, which differs between
// 32- and 64-bit builds. Hence computing them from unsafe.Sizeof rather than
// writing out a constant: the same source is then right on every architecture.
const (
	ioCOut       uintptr = 0x40000000
	ioCIn        uintptr = 0x80000000
	ioCParamMask uintptr = 0x1fff
)

func ioC(dir uintptr, group byte, num uintptr, paramLen uintptr) uintptr {
	return dir | (paramLen&ioCParamMask)<<16 | uintptr(group)<<8 | num
}

// ioR builds a request whose argument the kernel writes into.
func ioR(group byte, num uintptr, paramLen uintptr) uint {
	return uint(ioC(ioCOut, group, num, paramLen))
}

// ioW builds a request whose argument the kernel reads.
func ioW(group byte, num uintptr, paramLen uintptr) uint {
	return uint(ioC(ioCIn, group, num, paramLen))
}

// fiodgnameArg is struct fiodgname_arg from FreeBSD's <sys/filio.h>.
//
// Go inserts the same four bytes of padding before the pointer on 64-bit builds
// as C does, so no explicit pad field is needed and unsafe.Sizeof gives the
// right length for the request number.
type fiodgnameArg struct {
	Len int32
	Buf *byte
}

// ptmget is struct ptmget from OpenBSD's <sys/ttycom.h>: the clone device
// returns both ends of a new pseudo-terminal in one of these.
type ptmget struct {
	Cfd int32
	Sfd int32
	Cn  [16]byte
	Sn  [16]byte
}
