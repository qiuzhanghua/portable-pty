# portable-pty (Go)

A cross-platform pseudo-terminal (PTY) library for Go, ported from the Rust
[`portable-pty`](https://github.com/wezterm/wezterm/tree/main/pty) crate.

> **Status: pre-alpha.** Only the public interface and its types exist so far.
> There is **no PTY implementation yet** — `System`, `Master`, `Child` and
> `Killer` are interfaces without implementations. Do not depend on this
> module yet. See [DESIGN.md](DESIGN.md) for the plan.

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
| Unix PTY | ✅ | ✅ | planned |
| Windows ConPTY | ❌ | ✅ | planned |
| Runtime-selectable PTY system | ❌ | ❌ | planned |
| Command builder | ❌ | ❌ | planned |
| Exit status with signal name | ❌ | ❌ | ✅ |
| Killer decoupled from `Wait` | ❌ | ❌ | planned |
| Serial ports | ❌ | ❌ | planned |
| Dependencies | none | `x/sys`, `x/crypto/ssh` | `x/sys` only |

`go-pty` already covers the Unix + ConPTY core, and is a reasonable choice
today. This module exists to fill in the abstraction layer it omits, and to fix
a correctness problem in the `creack/pty` lineage (see below).

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

## Requirements

Go 1.24 or later. The dependency is pinned to `golang.org/x/sys v0.41.0`, which
is the newest release that does not require Go 1.25.

## Documentation

- [DESIGN.md](DESIGN.md) — full design, verified platform facts, decisions,
  milestones, and open questions (written in Chinese).
- [Package documentation](https://pkg.go.dev/github.com/qiuzhanghua/portable-pty)

## License

MIT. This is a port of `portable-pty`, which is part of
[wezterm](https://github.com/wezterm/wezterm) and is copyright (c) 2018-Present
Wez Furlong. See [LICENSE](LICENSE).
