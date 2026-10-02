//go:build unix

package pty

import (
	"bufio"
	"os"
	"path"
	"strconv"
	"strings"
)

// Shell returns the shell this package would run for the user.
//
// It is $SHELL when that names an executable file, otherwise the login shell
// recorded in the passwd database, otherwise /bin/sh. This is
// CommandBuilder::get_shell.
func Shell() string {
	if sh := os.Getenv("SHELL"); sh != "" && isExecutable(sh) {
		return sh
	}
	if sh := passwdShell(); sh != "" {
		return sh
	}
	return "/bin/sh"
}

func baseEnviron() []string {
	env := os.Environ()
	if _, ok := EnvGet(env, "SHELL"); ok {
		return env
	}
	// Deliberately not Shell(): that consults $SHELL, which is precisely the
	// variable we have just found to be missing.
	shell := passwdShell()
	if shell == "" {
		shell = "/bin/sh"
	}
	return append(env, "SHELL="+shell)
}

// shellArgv0 is what turns a shell into a login shell: a "-" prefix on argv[0].
func shellArgv0(shell string) string { return "-" + path.Base(shell) }

// envKeyEqual compares environment variable names. Unix names are
// case-sensitive.
func envKeyEqual(a, b string) bool { return a == b }

// isExecutable reports whether name refers to something that can be run.
//
// This tests the mode bits instead of calling access(2) as portable-pty does.
// For choosing a shell the two agree; the mode check keeps this file free of
// platform-specific syscalls, so it builds on every Unix Go supports.
func isExecutable(name string) bool {
	info, err := os.Stat(name)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
}

// passwdShell returns the login shell recorded for this uid in /etc/passwd, or
// "" if it cannot be determined.
//
// Go's os/user deliberately does not expose pw_shell, so the file is read
// directly. That is reliable on Linux. On macOS accounts usually live in
// OpenDirectory rather than /etc/passwd, so this returns "" there and callers
// fall back to /bin/sh; portable-pty reaches the real value through getpwuid,
// so that is a known, narrow divergence.
func passwdShell() string {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()

	uid := strconv.Itoa(os.Getuid())
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) < 7 || fields[2] != uid {
			continue
		}
		if sh := fields[6]; sh != "" && isExecutable(sh) {
			return sh
		}
		return ""
	}
	return ""
}
