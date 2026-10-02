# Changelog

## v0.2.1

Documentation only. No API changes and no behaviour changes: the only new Go
files are test files, which are not part of the built package.

### Added

- **Usage examples** at the front of the README — a command on a PTY, a bare
  PTY, and a serial port. The same snippets live in [`example_test.go`](example_test.go)
  and [`serial/example_test.go`](serial/example_test.go), where the compiler
  checks them. They carry no `// Output:` comment, so the testing package
  compiles them without running them: spawning a shell and printing a tty path
  cannot produce stable output across platforms.
- **DESIGN.md §10.9**, recording the `go.bug.st/serial` NetBSD gap: the exact
  error, the two build tags behind it, a report ready to file upstream, and the
  checklist for re-enabling NetBSD here once upstream fixes it.

### Changed

- The README is now bilingual, English first then Chinese, in a single file
  rather than two that would drift apart.
- Fixed a stale row in the comparison table: the command helpers have shipped
  since M4, but the table still described them as planned.

## v0.2.0

No API changes. The module now builds with older toolchains.

### Changed

- **The Go floor is 1.20 instead of 1.24**, and `golang.org/x/sys` is pinned to
  v0.30.0 instead of v0.41.0. Neither dependency ever set that floor — x/sys
  v0.30.0 declares Go 1.18 and `go.bug.st/serial` v1.6.4 declares 1.17 — so the
  old floor came only from syntax this module had no need of: ranging over an
  integer, and `slices.Equal`/`slices.Contains` in tests. Those are now two
  small helpers and two ordinary loops.
- CI runs its tests against the floor **and** the current release on each
  platform. A job pinned to `go.mod` alone would from here on test only a
  toolchain that is years past its upstream support window.

### Notes

- The ConPTY wrappers in x/sys are byte-identical between v0.30.0 and v0.41.0,
  so the older dependency does not change Windows behaviour.
- 1.20 is deliberately where this stops, rather than 1.18: Go 1.19 introduced
  the `unix` build tag, and without it `env_other.go` — the js/plan9 stub — is
  selected on Unix in place of `env_unix.go`. The package compiles and silently
  loses `Shell`'s passwd fallback. See DESIGN.md §10.3 for the full ladder.

## v0.1.0

First release. Everything below is new; the version is 0.x because the API may
still change.

### Added

- **Pseudo-terminals** behind a `System`/`Master`/`Child` abstraction, so the
  implementation is chosen at runtime. `Native()` returns the platform's.
- **Linux and macOS**: `os.OpenFile` on `/dev/ptmx`, so the master joins the Go
  netpoller — reads park the goroutine instead of pinning an OS thread, and
  `Close` reliably wakes a blocked read.
- **Windows ConPTY**: processes are created with `CreateProcess` and a
  `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE` attribute list, because `os/exec` cannot
  attach a process to a console ([golang/go#62708]).
- **FreeBSD, OpenBSD and NetBSD**: implemented but *unverified*, as no CI runner
  exists for them. See DESIGN.md §10.8 for exactly what was and was not checked.
- **`Command`, `LoginShell`, `Environ`, `Shell`, `HomeDir`** and `EnvGet`/
  `EnvSet`/`EnvUnset`, replacing the Rust crate's `CommandBuilder` without
  inventing a new command type.
- **`Master.Resize`/`Size`**, `Termios`, `Pgrp`, exit status carrying a signal
  name, and a `Killer` that can be used from a goroutine other than the one
  blocked in `Wait`.
- **`serial` subpackage**: a serial port as a `pty.Master`, in its own package so
  that the core keeps `golang.org/x/sys` as its only dependency.

### Deliberate divergences from portable-pty

- `CommandBuilder::umask` has no equivalent: `os/exec` offers no `pre_exec`, and
  `syscall.SysProcAttr` has no `Umask` field on Linux or macOS.
- `SerialTty`'s dummy child is not reproduced; `serial`'s `Spawn` reports
  `ErrNoProcess`. Its XON/XOFF flow-control default is also absent, because
  `go.bug.st/serial` exposes no flow-control setting.
- Signal names are the deterministic `SIG*` form rather than `strsignal`'s
  locale-dependent text.
- `Command` and `LoginShell` default `Dir` to the home directory, as upstream
  does, but do not silently substitute it when a configured directory is
  missing.

### Verified

CI runs the full suite on Linux, macOS and Windows, against both Go 1.20 (the
declared floor) and the current release, and cross-compiles eleven targets
with the floor. `go test -race` runs where the race detector does.

[golang/go#62708]: https://github.com/golang/go/issues/62708
