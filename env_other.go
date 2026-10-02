//go:build !unix && !windows

package pty

import "os"

// Shell returns $SHELL. The remaining platforms this package does not support
// have no notion of a login shell to offer.
func Shell() string { return os.Getenv("SHELL") }

func shellArgv0(shell string) string { return shell }

func envKeyEqual(a, b string) bool { return a == b }

func baseEnviron() []string { return os.Environ() }
