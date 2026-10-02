package pty

import "testing"

func TestExitStatusSuccess(t *testing.T) {
	tests := []struct {
		name   string
		status ExitStatus
		want   bool
	}{
		{"zero value", ExitStatus{}, true},
		{"clean exit", ExitStatus{Code: 0}, true},
		{"non-zero exit", ExitStatus{Code: 2}, false},
		{"killed by signal", ExitStatus{Code: 0, Signal: "SIGKILL"}, false},
		{"signal with code", ExitStatus{Code: 1, Signal: "SIGHUP"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.Success(); got != tt.want {
				t.Errorf("Success() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExitStatusString(t *testing.T) {
	tests := []struct {
		name   string
		status ExitStatus
		want   string
	}{
		{"success", ExitStatus{}, "Success"},
		{"exit code", ExitStatus{Code: 2}, "Exited with code 2"},
		{"signal", ExitStatus{Code: 1, Signal: "SIGKILL"}, "Terminated by SIGKILL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultSize(t *testing.T) {
	if DefaultSize.Rows != 24 || DefaultSize.Cols != 80 {
		t.Errorf("DefaultSize = %+v, want 24x80", DefaultSize)
	}
}

func TestSpawnOptionsDefault(t *testing.T) {
	cfg := applySpawnOptions(nil)
	if !cfg.controllingTTY {
		t.Error("controllingTTY should default to true")
	}
	if cfg.umask != nil {
		t.Error("umask should default to nil")
	}
}

func TestSpawnOptionsApplied(t *testing.T) {
	cfg := applySpawnOptions([]SpawnOption{
		WithControllingTTY(false),
		nil, // must be tolerated
	})
	if cfg.controllingTTY {
		t.Error("WithControllingTTY(false) was not applied")
	}
}
