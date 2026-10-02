package serial

import (
	"errors"
	"os/exec"
	"testing"

	bugst "go.bug.st/serial"

	"github.com/qiuzhanghua/portable-pty"
)

// A serial system is a pty.System, which is the whole point of the abstraction.
var _ pty.System = System("/dev/null", DefaultConfig())

func TestDefaultConfigMatchesUpstream(t *testing.T) {
	cfg := DefaultConfig()
	want := Config{BaudRate: 9600, DataBits: 8, Parity: ParityNone, StopBits: StopBitsOne}
	if cfg != want {
		t.Errorf("DefaultConfig() = %+v, want %+v", cfg, want)
	}
}

func TestOpenRejectsEmptyPort(t *testing.T) {
	if _, err := Open("", DefaultConfig()); err == nil {
		t.Error("Open with an empty name succeeded, want an error")
	}
}

// A serial line has no process, so Spawn must say so rather than invent one.
func TestSpawnReportsNoProcess(t *testing.T) {
	m := &master{name: "fake"}
	if _, err := m.Spawn(exec.Command("true")); !errors.Is(err, pty.ErrNoProcess) {
		t.Errorf("Spawn error = %v, want it to wrap pty.ErrNoProcess", err)
	}
}

// Neither of these touches the port, so a bare master is enough.
func TestResizeAndSizeAreInert(t *testing.T) {
	m := &master{name: "fake"}

	if err := m.Resize(pty.Size{Rows: 1, Cols: 1}); err != nil {
		t.Errorf("Resize = %v, want nil: a serial line has no window size", err)
	}
	got, err := m.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if got != pty.DefaultSize {
		t.Errorf("Size = %+v, want %+v", got, pty.DefaultSize)
	}
}

func TestParityMapping(t *testing.T) {
	for _, tc := range []struct {
		in   Parity
		want bugst.Parity
	}{
		{ParityNone, bugst.NoParity},
		{ParityOdd, bugst.OddParity},
		{ParityEven, bugst.EvenParity},
		{Parity(99), bugst.NoParity}, // unknown values fall back rather than panic
	} {
		if got := toParity(tc.in); got != tc.want {
			t.Errorf("toParity(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestStopBitsMapping(t *testing.T) {
	if got := toStopBits(StopBitsOne); got != bugst.OneStopBit {
		t.Errorf("toStopBits(StopBitsOne) = %v, want %v", got, bugst.OneStopBit)
	}
	if got := toStopBits(StopBitsTwo); got != bugst.TwoStopBits {
		t.Errorf("toStopBits(StopBitsTwo) = %v, want %v", got, bugst.TwoStopBits)
	}
}
