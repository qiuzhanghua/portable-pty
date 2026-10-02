# Changelog

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
