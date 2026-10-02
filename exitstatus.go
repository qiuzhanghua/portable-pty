package pty

import "fmt"

// ExitStatus describes how a child process terminated.
type ExitStatus struct {
	// Code is the process exit code.
	Code int

	// Signal is the name of the signal that terminated the process, or the
	// empty string if the process exited normally. It is always empty on
	// Windows.
	Signal string
}

// Success reports whether the process completed successfully.
func (s ExitStatus) Success() bool {
	return s.Signal == "" && s.Code == 0
}

// String mirrors the Display implementation of portable-pty's ExitStatus,
// producing "Success", "Terminated by SIGKILL", or "Exited with code 2".
func (s ExitStatus) String() string {
	switch {
	case s.Success():
		return "Success"
	case s.Signal != "":
		return "Terminated by " + s.Signal
	default:
		return fmt.Sprintf("Exited with code %d", s.Code)
	}
}
