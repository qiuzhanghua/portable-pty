package pty

import (
	"os"
	"os/exec"
	"strings"
)

// Environ returns the environment a PTY child should inherit.
//
// It is a snapshot taken when called, not a live view of the process
// environment. Compared with os.Environ it adds what portable-pty's
// CommandBuilder adds:
//
//   - Unix: SHELL is guaranteed to be present. When the variable is unset it is
//     filled in from the passwd database, falling back to /bin/sh.
//   - Windows: the machine and user environments are merged in from the
//     registry, REG_EXPAND_SZ values are expanded, and the user Path is
//     appended to the machine Path.
func Environ() []string { return baseEnviron() }

// HomeDir returns the current user's home directory: HOME on Unix, USERPROFILE
// on Windows.
func HomeDir() (string, error) { return os.UserHomeDir() }

// Command builds a command the way CommandBuilder::new does.
//
// It is exec.Command with two things portable-pty guarantees: Env is an
// explicit snapshot (see Environ) rather than nil, and Dir defaults to the
// user's home directory. Assign cmd.Dir to run elsewhere, including "" to
// inherit this process's working directory.
//
// Note that portable-pty silently substitutes the home directory when a
// configured cwd turns out not to exist. This package does not: hiding a bad
// path only defers the failure. A non-existent cmd.Dir makes Spawn fail with
// the chdir error.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = Environ()
	if dir, err := HomeDir(); err == nil {
		cmd.Dir = dir
	}
	return cmd
}

// LoginShell builds a command that runs the user's shell as a login shell.
//
// On Unix argv[0] gets a "-" prefix (for example "-bash"), which is what makes
// the shell read its login profiles. On Windows the shell is ComSpec and
// argv[0] is left alone. This is CommandBuilder::new_default_prog.
func LoginShell() *exec.Cmd {
	shell := Shell()
	cmd := exec.Command(shell)
	cmd.Args[0] = shellArgv0(shell)
	cmd.Env = Environ()
	if dir, err := HomeDir(); err == nil {
		cmd.Dir = dir
	}
	return cmd
}

// EnvGet reports the value of key in env, and whether it was present.
//
// Key matching is case-insensitive on Windows, which is how the operating
// system treats environment variable names there.
func EnvGet(env []string, key string) (string, bool) {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && envKeyEqual(k, key) {
			return v, true
		}
	}
	return "", false
}

// EnvSet returns env with key set to value, replacing any existing entry for
// key. The caller's spelling of the name is preserved, as
// CommandBuilder::env does.
func EnvSet(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok && envKeyEqual(k, key) {
			if !replaced {
				out = append(out, key+"="+value)
				replaced = true
			}
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, key+"="+value)
	}
	return out
}

// EnvUnset returns env with every entry for key removed. This is
// CommandBuilder::env_remove.
func EnvUnset(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok && envKeyEqual(k, key) {
			continue
		}
		out = append(out, kv)
	}
	return out
}
