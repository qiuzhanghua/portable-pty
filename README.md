# portable-pty (Go)

A cross-platform pseudo-terminal (PTY) library for Go, ported from the Rust
[`portable-pty`](https://github.com/wezterm/wezterm/tree/main/pty) crate.

一个 Go 的跨平台伪终端（PTY）库，移植自 Rust 的
[`portable-pty`](https://github.com/wezterm/wezterm/tree/main/pty) crate。

> **Status: early.** Linux, macOS and Windows are implemented: open/close,
> window size, spawning with a controlling terminal, exit status, and a child
> that can be killed independently of `Wait`. The command helpers
> (`Command`, `LoginShell`, `Environ`) work on every platform, and the `serial`
> subpackage exposes a serial port as a `pty.Master`. FreeBSD, OpenBSD and
> NetBSD are implemented but **unverified**, because no CI runner exists for
> them; other platforms return `ErrUnsupported`. The API may still change.
> See [DESIGN.md](DESIGN.md) for what is and is not verified.
>
> **状态：早期。** Linux、macOS 与 Windows 已实现：打开/关闭、窗口大小、带控制终端
> 地启动进程、退出状态，以及一个可以脱离 `Wait` 独立终止的子进程。命令辅助函数
> （`Command`、`LoginShell`、`Environ`）在所有平台可用，`serial` 子包把串口暴露成
> `pty.Master`。FreeBSD、OpenBSD、NetBSD 已实现但**未经验证**，因为 CI 没有对应
> runner；其余平台返回 `ErrUnsupported`。API 仍可能变动。哪些验证过、哪些没有，
> 见 [DESIGN.md](DESIGN.md)。

## Usage / 用法

Install it with `go get`:

用 `go get` 安装：

```bash
go get github.com/qiuzhanghua/portable-pty@latest
```

If `proxy.golang.org` is unreachable from where you are, point Go at a mirror
first: `go env -w GOPROXY=https://goproxy.cn,direct`.

如果你所在网络访问不到 `proxy.golang.org`，先让 Go 走镜像：
`go env -w GOPROXY=https://goproxy.cn,direct`。

### Run a command on a PTY / 在 PTY 上运行命令

Allocate a PTY, spawn a command on it, drain it while the child runs, then wait
for it.

分配一个 PTY，在它上面启动命令，一边排空输出一边等子进程结束。

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

which prints something like:

输出大致如下：

```
the child sees /dev/pts/3

[Success]
```

### Just a PTY, no child / 只要一个 PTY，不启动子进程

Pass `pty.DefaultSize` when you have nothing better: a zero `Size` really does
ask for a 0x0 window, which upsets programs that query it.

没有更好的选择时请传 `pty.DefaultSize`：零值 `Size` 是真的在请求一个 0x0 的窗口，
会干扰那些会去查询窗口大小的程序。

```go
m, err := pty.Native().OpenPty(pty.Size{Cols: 80, Rows: 24})
if err != nil {
	return err
}
defer m.Close()

fmt.Println(m.Name()) // /dev/pts/3, /dev/ttys003, or "conpty" on Windows
                      // Linux 上是 /dev/pts/3，macOS 上是 /dev/ttys003，Windows 上是 "conpty"

if err := m.Resize(pty.Size{Cols: 120, Rows: 40}); err != nil {
	return err
}
```

### A serial port / 串口

A serial line is exposed as the same `pty.Master` interface, so code written
against it works with either. `Spawn` reports `pty.ErrNoProcess` on a serial
line, because there is no process behind it.

串口被暴露成同一个 `pty.Master` 接口，所以针对该接口写的代码两边通用。在串口上
`Spawn` 会返回 `pty.ErrNoProcess`，因为串口背后没有进程。

```go
import "github.com/qiuzhanghua/portable-pty/serial"

port, err := serial.Open("/dev/ttyUSB0", serial.DefaultConfig()) // 9600 8N1
if err != nil {
	return err
}
defer port.Close()

io.Copy(os.Stdout, port) // Read blocks until bytes arrive / Read 会阻塞到有数据为止
```

The snippets above are compiled as examples in
[`example_test.go`](example_test.go), and `go doc` shows the same API offline.

上面这些代码片段在 [`example_test.go`](example_test.go) 中被当作示例编译，因此不会
写错；`go doc` 也能离线看到同一套 API。

## Design goals / 设计目标

- **Capability parity** with the Rust crate: runtime-selectable PTY system,
  a command builder, independent reader/writer handles, exit status that
  distinguishes signals, and a killable child that can be signalled
  independently of `Wait`.
- **Idiomatic Go surface** rather than a literal trait translation.
- **Pure Go**: no cgo, no DLLs, no C dependencies.
- **Consistent behaviour across platforms**: platform differences are
  normalised rather than leaked to callers.

- 与 Rust crate **能力对齐**：运行时可选的 PTY 实现、命令构建器、可独立使用的读写
  句柄、能区分信号的退出状态，以及一个可以脱离 `Wait` 被独立发信号的子进程。
- **Go 惯用的接口**，而不是把 trait 直译过来。
- **纯 Go**：不用 cgo、不加载 DLL、无 C 依赖。
- **跨平台行为一致**：平台差异被归一化，而不是泄漏给调用方。

Linux, macOS, the BSDs, and Windows (ConPTY) are targeted. Other Unix variants
report `ErrUnsupported`.

目标平台是 Linux、macOS、各 BSD，以及 Windows（ConPTY）。其他 Unix 变体返回
`ErrUnsupported`。

## Comparison with other Go PTY libraries / 与其他 Go PTY 库的对比

| | [creack/pty](https://github.com/creack/pty) | [aymanbagabas/go-pty](https://github.com/aymanbagabas/go-pty) | this module / 本模块 |
|---|---|---|---|
| Unix PTY | ✅ | ✅ | ✅ linux, darwin; ⚠️ BSDs implemented, unverified / BSD 已实现但未验证 |
| Windows ConPTY | ❌ | ✅ | ✅ |
| Runtime-selectable PTY system / 运行时可选的 PTY 实现 | ❌ | ❌ | ✅ |
| Command helpers (`Command`, `LoginShell`, `Environ`) / 命令辅助函数 | ❌ | ❌ | ✅ |
| Exit status with signal name / 带信号名的退出状态 | ❌ | ❌ | ✅ |
| Killer decoupled from `Wait` / Killer 与 `Wait` 解耦 | ❌ | ❌ | ✅ |
| Child stdio guaranteed blocking / 子进程 stdio 保证阻塞 | — | — | ✅ asserted by test / 有测试断言 |
| Serial ports / 串口 | ❌ | ❌ | ✅ `serial` subpackage / `serial` 子包 |
| Dependencies / 依赖 | none / 无 | `x/sys`, `x/crypto/ssh` | `x/sys`; `go.bug.st/serial` only in `serial` / 仅 `serial` 子包 |

Relative to portable-pty there is one known gap: the Rust crate's
`CommandBuilder::umask` has no Go equivalent, because `os/exec` offers no
`pre_exec` hook and `syscall.SysProcAttr` has no `Umask` field on Linux or
Darwin. See DESIGN.md D3 for why the available workarounds were rejected.

与 portable-pty 相比有一处已知缺口：Rust crate 的 `CommandBuilder::umask` 在 Go 里
没有对应物，因为 `os/exec` 不提供 `pre_exec` 钩子，而 `syscall.SysProcAttr` 在
Linux 与 Darwin 上都没有 `Umask` 字段。为什么可用的变通方案都被否决，见 DESIGN.md
的 D3。

`go-pty` already covers the Unix + ConPTY core, and is a reasonable choice if
you need Windows today. This module exists to fill in the abstraction layer it
omits, and to fix a correctness problem in the `creack/pty` lineage (see
below).

`go-pty` 已经覆盖了 Unix + ConPTY 的核心部分，如果你今天就需要 Windows，它是一个
合理选择。本模块的存在是为了补上它省略掉的那层抽象，并修掉 `creack/pty` 这一脉的
一个正确性问题（见下）。

## A note on handle handling / 关于句柄处理的一则说明

On Unix the master is opened with `os.OpenFile` so the Go runtime registers it
with the netpoller. Reads then park the goroutine instead of pinning an OS
thread, and `Close` reliably wakes a blocked `Read`.

在 Unix 上 master 是用 `os.OpenFile` 打开的，这样 Go 运行时会把它注册进 netpoller。
于是读操作只会挂起 goroutine，而不会占住一个 OS 线程，并且 `Close` 能可靠地唤醒被
阻塞的 `Read`。

The older approach used by `creack/pty` — `syscall.Open` followed by
`os.NewFile` — leaves the descriptor blocking and outside the netpoller. This
was measured directly: a blocked `Read` on such a handle was **still blocked
three seconds after `Close`**, with the OS thread pinned.

`creack/pty` 使用的旧做法 —— `syscall.Open` 之后再 `os.NewFile` —— 会让描述符保持
阻塞状态且不在 netpoller 中。这一点是直接实测的：这样一个句柄上被阻塞的 `Read`，
在 `Close` 之后**三秒仍然阻塞**，并且 OS 线程被占住。

For the same reason, do **not** call `(*os.File).Fd()` on a PTY master. `Fd`
clears `O_NONBLOCK`, which is shared by every `dup` of the same open file
description, so one call silently degrades all handles of that PTY. Use
`SyscallConn()` instead.

出于同样的原因，**不要**对 PTY master 调用 `(*os.File).Fd()`。`Fd` 会清掉
`O_NONBLOCK`，而该标志被同一份 open file description 的每个 `dup` 共享，因此单单
一次调用就会静默地劣化这个 PTY 的所有句柄。请改用 `SyscallConn()`。

## Platform support / 平台支持

| | Verified by CI / CI 验证程度 |
|---|---|
| Linux, macOS, Windows | ✅ tests run on every push / 每次推送都跑测试 |
| FreeBSD, OpenBSD, NetBSD | ⚠️ compiled only — implemented, never executed / 只验证能编译，从未真正运行 |
| Anything else / 其他平台 | `Native()` returns `ErrUnsupported` |

The BSD implementations are honest about this: each says `UNVERIFIED` in its
documentation, and the parts that *can* be checked without the hardware — the
argument struct layouts and the `_IOC` request numbers derived from them — are
checked on every platform. See DESIGN.md §10.8.

三个 BSD 的实现对此是诚实的：每一处 Godoc 都写着 `UNVERIFIED`；而那些**不需要真机
就能检验**的部分 —— 参数结构体的内存布局，以及由布局大小推出的 `_IOC` 请求号 ——
在每个平台上都被测试覆盖。见 DESIGN.md §10.8。

## Requirements / 环境要求

Go 1.20 or later. Dependencies are pinned to the newest releases that keep that
floor: `golang.org/x/sys v0.30.0` (v0.31.0 and later require Go 1.23), and
`go.bug.st/serial v1.6.4` for the `serial` subpackage (v1.7.0 and later require
Go 1.25). Neither dependency sets the floor any more — both work well below it —
so 1.20 is where this module's own syntax happens to land.

Go 1.20 或更高版本。依赖被锁定在「能维持该下限的最新版本」：
`golang.org/x/sys v0.30.0`（v0.31.0 起需要 Go 1.23），以及 `serial` 子包用的
`go.bug.st/serial v1.6.4`（v1.7.0 起需要 Go 1.25）。这两个依赖现在都不再决定下限
了 —— 它们都能在远低于 1.20 的版本上工作 —— 所以 1.20 只是本模块自身语法恰好落在
的位置。

CI tests both that floor and the current release, since Go 1.20 is itself long
past its upstream support window.

CI 同时测试该下限与当前最新版，因为 Go 1.20 本身早已超出上游支持窗口。

The `serial` subpackage builds only where `go.bug.st/serial` is implemented —
Linux, macOS, FreeBSD, OpenBSD and Windows. Elsewhere its `Open` returns
`ErrUnsupported`, so the package still compiles and says so plainly. This
includes NetBSD: v1.8.0 does not support it either, despite being tagged for it
upstream. DESIGN.md §10.9 has the evidence, a ready-to-file upstream report, and
the checklist for re-enabling it.

`serial` 子包只在 `go.bug.st/serial` 有实现的平台上构建 —— Linux、macOS、FreeBSD、
OpenBSD 与 Windows。其余平台上它的 `Open` 返回 `ErrUnsupported`，因此包始终能编译
并如实说明。这其中包括 NetBSD：上游虽然为它打了 tag，但 v1.8.0 同样不支持。
DESIGN.md §10.9 里有钱的证据、一份可直接提交的上游报告，以及重新启用它的收尾清单。

## Documentation / 文档

- [DESIGN.md](DESIGN.md) — full design, verified platform facts, decisions,
  milestones, and open questions (written in Chinese).
- [Package documentation](https://pkg.go.dev/github.com/qiuzhanghua/portable-pty)

- [DESIGN.md](DESIGN.md) —— 完整设计、实测的平台事实、各项决策、里程碑与未决问题
  （中文撰写）。
- [包文档](https://pkg.go.dev/github.com/qiuzhanghua/portable-pty)

## License / 许可证

MIT. This is a port of `portable-pty`, which is part of
[wezterm](https://github.com/wezterm/wezterm) and is copyright (c) 2018-Present
Wez Furlong. See [LICENSE](LICENSE).

MIT。本项目是 `portable-pty` 的移植，后者是
[wezterm](https://github.com/wezterm/wezterm) 的一部分，版权归 (c) 2018-Present
Wez Furlong 所有。见 [LICENSE](LICENSE)。
