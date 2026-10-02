package pty

import (
	"strconv"
	"testing"
	"unsafe"
)

// These run on every platform, which is the point: the BSD implementations
// cannot be exercised here, but their riskiest parts — the argument layouts and
// the request arithmetic derived from them — can be.

// The reference implementation hard-codes 0x40287401 for OpenBSD's PTMGET,
// which is _IOR('t', 1, struct ptmget). Reproducing that number from the macro
// and the struct size is a real check on both.
func TestIOCRMatchesTheReferencePTMGET(t *testing.T) {
	if got := ioR('t', 1, unsafe.Sizeof(ptmget{})); got != 0x40287401 {
		t.Errorf("ioR('t', 1, sizeof(ptmget)) = %#010x, want 0x40287401", got)
	}
}

// _IOW('f', 120, struct fiodgname_arg) on a 64-bit build. The size in the
// request is the struct's, so this also pins the padding Go inserts before the
// pointer.
func TestIOWMatchesTheReferenceFIODGNAME(t *testing.T) {
	if strconv.IntSize == 64 {
		if got := ioW('f', 120, unsafe.Sizeof(fiodgnameArg{})); got != 0x80106678 {
			t.Errorf("ioW('f', 120, sizeof(fiodgnameArg)) = %#010x, want 0x80106678", got)
		}
	}
}

// struct ptmget is two ints and two 16-byte name buffers. The reference request
// number only works out if that is 40 bytes, so the size is what says the
// declaration is right.
func TestPtmgetLayout(t *testing.T) {
	if got := unsafe.Sizeof(ptmget{}); got != 40 {
		t.Fatalf("sizeof(ptmget) = %d, want 40", got)
	}
	if got := unsafe.Offsetof(ptmget{}.Cn); got != 8 {
		t.Errorf("offsetof(ptmget.Cn) = %d, want 8", got)
	}
	if got := unsafe.Offsetof(ptmget{}.Sn); got != 24 {
		t.Errorf("offsetof(ptmget.Sn) = %d, want 24", got)
	}
}

// struct fiodgname_arg is an int followed by a pointer, so 16 bytes on a 64-bit
// build and 12 on a 32-bit one. Either way the request number changes with it,
// which is exactly why it is computed rather than written out.
func TestFiodgnameArgLayout(t *testing.T) {
	want := uintptr(16)
	if strconv.IntSize == 32 {
		want = 12
	}
	if got := unsafe.Sizeof(fiodgnameArg{}); got != want {
		t.Errorf("sizeof(fiodgnameArg) = %d, want %d on a %d-bit build", got, want, strconv.IntSize)
	}
}

// The mask is 13 bits wide, so a struct larger than that would silently produce
// a different request. Neither of ours is anywhere near it.
func TestIOCParamMaskCoversTheStructs(t *testing.T) {
	for _, size := range []uintptr{unsafe.Sizeof(ptmget{}), unsafe.Sizeof(fiodgnameArg{})} {
		if size > ioCParamMask {
			t.Errorf("a %d-byte argument does not fit the %#x mask", size, ioCParamMask)
		}
	}
}
