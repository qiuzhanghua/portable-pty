package serial

import "testing"

// These run everywhere, including the platforms where the serial library does
// not exist, which is why the types and their defaults live outside the
// build-tagged file.

func TestDefaultConfigMatchesUpstream(t *testing.T) {
	cfg := DefaultConfig()
	want := Config{BaudRate: 9600, DataBits: 8, Parity: ParityNone, StopBits: StopBitsOne}
	if cfg != want {
		t.Errorf("DefaultConfig() = %+v, want %+v", cfg, want)
	}
}

func TestParityAndStopBitsValues(t *testing.T) {
	// The zero values are the defaults, so a zero Config means "8N1" rather
	// than something invalid.
	if ParityNone != 0 {
		t.Errorf("ParityNone = %d, want 0", ParityNone)
	}
	if StopBitsOne != 0 {
		t.Errorf("StopBitsOne = %d, want 0", StopBitsOne)
	}
}
