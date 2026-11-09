//go:build unix

package pty

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Shell returns the shell this package would run for the user.
//
// On Unix it is, in order: the interactive shell that encloses this process
// (walking the parent chain), the login shell from the user database, $SHELL,
// then /bin/sh.
//
// The order matters. $SHELL is the shell the user was *given* at login, not the
// one they are typing in, and a GUI process inherits it from launchd or a
// service manager rather than from a shell. On macOS a GUI app is started with
// /bin/bash even when the account has used zsh for years, and most accounts are
// not in /etc/passwd at all, so the old $SHELL -> passwd -> /bin/sh ladder fell
// through to bash or sh exactly where the answer was most visible. This is the
// ladder qcode's `shell.rs` uses.
func Shell() string {
	return selectShell(enclosingShell(), loginShell(), os.Getenv("SHELL"))
}

// selectShell picks the first candidate that can actually be executed, in the
// order given.
func selectShell(enclosing, login, env string) string {
	for _, shell := range [...]string{enclosing, login, env} {
		if shell != "" && isExecutable(shell) {
			return shell
		}
	}
	return "/bin/sh"
}

func baseEnviron() []string {
	env := os.Environ()
	if _, ok := EnvGet(env, "SHELL"); ok {
		return env
	}
	// Shell is what the child is going to run, so it is also the value SHELL
	// must have. $SHELL is missing here, so Shell cannot recurse into it.
	return append(env, "SHELL="+Shell())
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

// ---------------------------------------------------------------- parent chain

// maxChainDepth bounds the walk up the parent chain, and stops a corrupted or
// recycled pid from making it loop.
const maxChainDepth = 12

// interactiveShells are the shells a person types in. sh and dash are absent on
// purpose: scripts and `sh -c` wrappers put one in every parent chain, so
// "nearest shell" would resolve to dash for almost any program started from a
// script. A user whose login shell really is dash is still caught by the
// login-shell layer.
var interactiveShells = map[string]bool{
	"bash":   true,
	"zsh":    true,
	"fish":   true,
	"nu":     true,
	"elvish": true,
	"xonsh":  true,
	"tcsh":   true,
	"csh":    true,
	"ksh":    true,
	"mksh":   true,
	"oksh":   true,
	"yash":   true,
	"pwsh":   true,
}

// processRow is the part of the process table the search needs.
type processRow struct {
	ppid int
	comm string
}

// enclosingShell returns the interactive shell that started this process, or ""
// when the chain contains none.
func enclosingShell() string {
	return enclosingShellFrom(os.Getpid(), readProcessTable())
}

// enclosingShellFrom walks up from start, returning the first interactive shell
// it can execute. Missing rows, unreadable paths and cycles all end the walk
// quietly: none of them is an error worth failing a terminal over.
func enclosingShellFrom(start int, rows map[int]processRow) string {
	pid := start
	seen := make(map[int]bool, maxChainDepth)
	for i := 0; i < maxChainDepth; i++ {
		if pid <= 0 || seen[pid] {
			return ""
		}
		seen[pid] = true

		row, ok := rows[pid]
		if !ok {
			return ""
		}
		if shell := executableShell(row.comm); shell != "" {
			return shell
		}
		pid = row.ppid
	}
	return ""
}

// executableShell turns one chain entry into an executable path, or "" when the
// entry is not an interactive shell.
//
// `comm` is a bare name on Linux and usually a full path on macOS. A bare name
// is resolved through PATH only after its basename matched, so an unrelated
// executable cannot be picked by accident.
func executableShell(comm string) string {
	name := path.Base(comm)
	if !interactiveShells[name] {
		return ""
	}
	exe := comm
	if !path.IsAbs(exe) {
		resolved, err := exec.LookPath(name)
		if err != nil {
			return ""
		}
		exe = resolved
	}
	if !isExecutable(exe) {
		return ""
	}
	return exe
}

// readProcessTable reads the whole process table once. A nil result means it
// could not be read (`ps` missing, sandboxed, ...); the caller then falls
// through to the next layer.
func readProcessTable() map[int]processRow {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,comm=").Output()
	if err != nil {
		return nil
	}
	rows := make(map[int]processRow)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		// `comm` is the last column and may contain spaces on macOS.
		rows[pid] = processRow{ppid: ppid, comm: strings.Join(fields[2:], " ")}
	}
	return rows
}

// ---------------------------------------------------------------- login shell

// loginShell returns pw_shell: the shell `chsh` sets for the account.
func loginShell() string {
	if runtime.GOOS == "darwin" {
		if shell := darwinLoginShell(); shell != "" {
			return shell
		}
	}
	return passwdShell()
}

var (
	darwinShellOnce sync.Once
	darwinShellPath string
)

// darwinLoginShell asks OpenDirectory for the account's shell. The answer is
// cached: it cannot change while this process is running.
func darwinLoginShell() string {
	darwinShellOnce.Do(func() {
		darwinShellPath = readDarwinLoginShell()
	})
	return darwinShellPath
}

// readDarwinLoginShell reads pw_shell from the directory service.
//
// macOS keeps accounts in OpenDirectory rather than /etc/passwd, and Go's
// os/user does not expose pw_shell, so the only cgo-free way to ask is dscl.
// It runs at most once per process, and only when the parent chain found no
// shell.
func readDarwinLoginShell() string {
	current, err := user.Current()
	if err != nil || current.Username == "" {
		return ""
	}
	out, err := exec.Command(
		"/usr/bin/dscl", ".", "-read", "/Users/"+current.Username, "UserShell",
	).Output()
	if err != nil {
		return ""
	}
	// Output looks like: "UserShell: /bin/zsh".
	for _, line := range strings.Split(string(out), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "UserShell:"); ok {
			if shell := strings.TrimSpace(value); shell != "" && isExecutable(shell) {
				return shell
			}
		}
	}
	return ""
}

// passwdShell returns the login shell recorded for this uid in /etc/passwd, or
// "" if it cannot be determined.
//
// Go's os/user deliberately does not expose pw_shell, and without cgo there is
// no getpwuid_r, so the file is parsed. That is authoritative on Linux and the
// BSDs; macOS accounts are served by dscl above.
func passwdShell() string {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	return passwdShellFrom(os.Getuid(), f)
}

// passwdShellFrom is passwdShell with the uid and passwd data injected, so the
// parser can be tested without touching the real account database.
func passwdShellFrom(uid int, passwd io.Reader) string {
	want := strconv.Itoa(uid)
	sc := bufio.NewScanner(passwd)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) < 7 || fields[2] != want {
			continue
		}
		if sh := fields[6]; sh != "" && isExecutable(sh) {
			return sh
		}
		return ""
	}
	return ""
}
