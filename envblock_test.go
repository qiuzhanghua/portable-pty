package pty

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

// decodeEnvBlock reverses encodeEnvBlock, so tests can assert on strings.
func decodeEnvBlock(block []uint16) []string {
	parts := strings.Split(string(utf16.Decode(block)), "\x00")
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// withoutSystemRoot clears SYSTEMROOT, so tests that assert an exact block are
// not perturbed by the injection this package deliberately performs.
//
// This is not tidiness: SYSTEMROOT is normally set on Windows and normally
// absent elsewhere, so without it these assertions pass on Linux and macOS and
// fail on Windows — which is exactly what happened.
func withoutSystemRoot(t *testing.T) {
	t.Helper()
	t.Setenv("SYSTEMROOT", "")
}

func TestEncodeEnvBlockEntriesInOrder(t *testing.T) {
	withoutSystemRoot(t)
	got := decodeEnvBlock(encodeEnvBlock([]string{"A=1", "B=2"}))
	if want := []string{"A=1", "B=2"}; !slices.Equal(got, want) {
		t.Errorf("encodeEnvBlock = %q, want %q", got, want)
	}
}

// The block is entries separated by NULs and terminated by an extra NUL, which
// is the length CreateProcess requires.
func TestEncodeEnvBlockTerminator(t *testing.T) {
	block := encodeEnvBlock([]string{"A=1"})
	if len(block) < 2 {
		t.Fatalf("block = %v, want at least two units", block)
	}
	if last := block[len(block)-2:]; last[0] != 0 || last[1] != 0 {
		t.Errorf("block ends with %v, want two NUL units", last)
	}
}

func TestEncodeEnvBlockEmpty(t *testing.T) {
	withoutSystemRoot(t)
	if got := encodeEnvBlock(nil); len(got) != 1 || got[0] != 0 {
		t.Errorf("encodeEnvBlock(nil) = %v, want a single NUL", got)
	}
}

// Windows does not distinguish environment names by case, so a repeat must
// collapse; the first position is kept and the last value wins.
func TestEncodeEnvBlockDedupsCaseInsensitively(t *testing.T) {
	withoutSystemRoot(t)
	got := decodeEnvBlock(encodeEnvBlock([]string{"Path=first", "OTHER=x", "PATH=second"}))
	if want := []string{"PATH=second", "OTHER=x"}; !slices.Equal(got, want) {
		t.Errorf("encodeEnvBlock = %q, want %q", got, want)
	}
}

func TestEncodeEnvBlockDropsEntriesWithoutSeparator(t *testing.T) {
	withoutSystemRoot(t)
	got := decodeEnvBlock(encodeEnvBlock([]string{"GOOD=1", "MALFORMED"}))
	if want := []string{"GOOD=1"}; !slices.Equal(got, want) {
		t.Errorf("encodeEnvBlock = %q, want %q", got, want)
	}
}

// CreateProcess and a great many programs misbehave without SYSTEMROOT.
func TestEncodeEnvBlockAddsSystemRoot(t *testing.T) {
	t.Setenv("SYSTEMROOT", `C:\Windows`)

	got := decodeEnvBlock(encodeEnvBlock([]string{"A=1"}))
	if !slices.Contains(got, `SYSTEMROOT=C:\Windows`) {
		t.Errorf("encodeEnvBlock = %q, want it to contain SYSTEMROOT", got)
	}
}

func TestEncodeEnvBlockKeepsExistingSystemRoot(t *testing.T) {
	t.Setenv("SYSTEMROOT", `C:\Windows`)

	got := decodeEnvBlock(encodeEnvBlock([]string{`SystemRoot=D:\Elsewhere`}))
	if len(got) != 1 || got[0] != `SystemRoot=D:\Elsewhere` {
		t.Errorf("encodeEnvBlock = %q, want the existing SystemRoot untouched", got)
	}
}

func TestEncodeEnvBlockNoSystemRootAvailable(t *testing.T) {
	t.Setenv("SYSTEMROOT", "")

	got := decodeEnvBlock(encodeEnvBlock([]string{"A=1"}))
	if len(got) != 1 {
		t.Errorf("encodeEnvBlock = %q, want nothing added when SYSTEMROOT is unknown", got)
	}
}
