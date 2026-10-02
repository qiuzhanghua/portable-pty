# portable-pty (Go)

A cross-platform pseudo-terminal (PTY) library for Go, ported from the Rust
[`portable-pty`](https://github.com/wezterm/wezterm/tree/main/pty) crate.

> **Status: early.** Linux, macOS and Windows are implemented: open/close,
> window size, spawning with a controlling terminal, exit status, and a child
> that can be killed independently of `Wait`. The command helpers
> (`Command`, `LoginShell`, `Environ`) work on every platform, and the `serial`
> subpackage exposes a serial port as a `pty.Master`. FreeBSD, OpenBSD and
> NetBSD are implemented but **unverified**, because no CI runner exists for
> them; other platforms return `ErrUnsupported`. The API may still change.
> See [DESIGN.md](DESIGN.md) for what is and is not verified.

## Usage

```bash
go get github.com/qiuzhanghua/portable-pty@latest
```

If `proxy.golang.org` is unreachable from where you are, point Go at a mirror
first: `go env -w GOPROXY=https://goproxy.cn,direct`.

### Run a command on a PTY

```go
package main

import (
	"fmt"
	"io"
	"os"

	pty "github.com/qiuzhanghua/portable-pty"
)

func main() {
	// Allocate a PTY.
	m, err := pty.Native().OpenPty(pty.DefaultSize)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open pty:", err)
		os.Exit(1)
	}
	defer m.Close()

	// Spawn a command on it; the PTY becomes its controlling terminal.
	child, err := m.Spawn(pty.Command("sh", "-c", "printf 'the child sees '; tty"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "spawn:", err)
		os.Exit(1)
	}

	// Drain the PTY while the child runs, the way a terminal would.
	done := make(chan struct{})
	go func() {
		io.Copy(os.Stdout, m)
		close(done)
	}()

	status, err := child.Wait()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wait:", err)
		os.Exit(1)
	}
	<-done

	fmt.Printf("\n[%s]\n", status) // e.g. [Success]
}
```

which prints something like

```
the child sees /dev/pts/3

[Success]
```

### Just a PTY, no child

```go
m, err := pty.Native().OpenPty(pty.Size{Cols: 80, Rows: 24})
if err != nil {
	return err
}
defer m.Close()

fmt.Println(m.Name()) // /dev/pts/3, /dev/ttys003, or "conpty" on Windows

if err := m.Resize(pty.Size{Cols: 120, Rows: 40}); err != nil {
	return err
}
```

Pass `pty.DefaultSize` when you have nothing better: a zero `Size` really does
ask for a 0x0 window, which upsets programs that query it.

### A serial port

A serial line is exposed as the same `pty.Master` interface, so code written
against it works with either.

```go
import "github.com/qiuzhanghua/portable-pty/serial"

port, err := serial.Open("/dev/ttyUSB0", serial.DefaultConfig()) // 9600 8N1
if err != nil {
	return err
}
defer port.Close()

io.Copy(os.Stdout, port) // Read blocks until bytes arrive
```

`Spawn` reports `pty.ErrNoProcess` on a serial line, because there is no process
behind it.

The snippets above are compiled as examples in
[`example_test.go`](example_test.go), and `go doc` shows the same API offline.

## Design goals

- **Capability parity** with the Rust crate: runtime-selectable PTY system,
  a command builder, independent reader/writer handles, exit status that
  distinguishes signals, and a killable child that can be signalled
  independently of `Wait`.
- **Idiomatic Go surface** rather than a literal trait translation.
- **Pure Go**: no cgo, no DLLs, no C dependencies.
- **Consistent behaviour across platforms**: platform differences are
  normalised rather than leaked to callers.

Linux, macOS, the BSDs, and Windows (ConPTY) are targeted. Other Unix variants
report `ErrUnsupported`.

## Comparison with other Go PTY libraries

| | [creack/pty](https://github.com/creack/pty) | [aymanbagabas/go-pty](https://github.com/aymanbagabas/go-pty) | this module |
|---|---|---|---|
| Unix PTY | ✅ | ✅ | ✅ linux, darwin; ⚠️ BSDs implemented, unverified |
| Windows ConPTY | ❌ | ✅ | ✅ |
| Runtime-selectable PTY system | ❌ | ❌ | ✅ |
| Command helpers (`Command`, `LoginShell`, `Environ`) | ❌ | ❌ | ✅ |
| Exit status with signal name | ❌ | ❌ | ✅ |
| Killer decoupled from `Wait` | ❌ | ❌ | ✅ |
| Child stdio guaranteed blocking | — | — | ✅ asserted by test |
| Serial ports | ❌ | ❌ | ✅ `serial` subpackage |
| Dependencies | none | `x/sys`, `x/crypto/ssh` | `x/sys`; `go.bug.st/serial` only in `serial` |

Relative to portable-pty there is one known gap: the Rust crate's
`CommandBuilder::umask` has no Go equivalent, because `os/exec` offers no
`pre_exec` hook and `syscall.SysProcAttr` has no `Umask` field on Linux or
Darwin. See DESIGN.md D3 for why the available workarounds were rejected.

`go-pty` already covers the Unix + ConPTY core, and is a reasonable choice if
you need Windows today. This module exists to fill in the abstraction layer it
omits, and to fix a correctness problem in the `creack/pty` lineage (see
below).

## A note on handle handling

On Unix the master is opened with `os.OpenFile` so the Go runtime registers it
with the netpoller. Reads then park the goroutine instead of pinning an OS
thread, and `Close` reliably wakes a blocked `Read`.

The older approach used by `creack/pty` — `syscall.Open` followed by
`os.NewFile` — leaves the descriptor blocking and outside the netpoller. This
was measured directly: a blocked `Read` on such a handle was **still blocked
three seconds after `Close`**, with the OS thread pinned.

For the same reason, do **not** call `(*os.File).Fd()` on a PTY master. `Fd`
clears `O_NONBLOCK`, which is shared by every `dup` of the same open file
description, so one call silently degrades all handles of that PTY. Use
`SyscallConn()` instead.

## Platform support

| | Verified by CI |
|---|---|
| Linux, macOS, Windows | ✅ tests run on every push |
| FreeBSD, OpenBSD, NetBSD | ⚠️ compiled only — implemented, never executed |
| Anything else | `Native()` returns `ErrUnsupported` |

The BSD implementations are honest about this: each says `UNVERIFIED` in its
documentation, and the parts that *can* be checked without the hardware — the
argument struct layouts and the `_IOC` request numbers derived from them — are
checked on every platform. See DESIGN.md §10.8.

## Requirements

Go 1.20 or later. Dependencies are pinned to the newest releases that keep that
floor: `golang.org/x/sys v0.30.0` (v0.31.0 and later require Go 1.23), and
`go.bug.st/serial v1.6.4` for the `serial` subpackage (v1.7.0 and later require
Go 1.25). Neither dependency sets the floor any more — both work well below it —
so 1.20 is where this module's own syntax happens to land.

CI tests both that floor and the current release, since Go 1.20 is itself long
past its upstream support window.

The `serial` subpackage builds only where `go.bug.st/serial` is implemented —
Linux, macOS, FreeBSD, OpenBSD and Windows. Elsewhere its `Open` returns
`ErrUnsupported`, so the package still compiles and says so plainly. This
includes NetBSD: v1.8.0 does not support it either, despite being tagged for it
upstream. DESIGN.md §10.9 has the evidence, a ready-to-file upstream report, and
the checklist for re-enabling it.

## Documentation

- [DESIGN.md](DESIGN.md) — full design, verified platform facts, decisions,
  milestones, and open questions (written in Chinese).
- [Package documentation](https://pkg.go.dev/github.com/qiuzhanghua/portable-pty)

## License

MIT. This is a port of `portable-pty`, which is part of
[wezterm](https://github.com/wezterm/wezterm) and is copyright (c) 2018-Present
Wez Furlong. See [LICENSE](LICENSE).
