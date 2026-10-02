# Go 版 portable-pty 设计文档

| 项 | 值 |
|---|---|
| 状态 | 设计已收敛；实测结果见 §10 |
| 日期 | 2026-10-02 |
| 目标读者 | 实现者、reviewer |
| 上游参照 | Rust `portable-pty` 0.9.0（wezterm，MIT） |

本文件只描述**设计**。实现代码尚未开始；所有结论都标注了来源，凡未经一手核实的推断一律显式标记为 `UNVERIFIED`。

---

## 1. 背景与目标

### 1.1 背景

Rust 生态有 `portable-pty`（wezterm 子项目），提供跨平台 PTY 抽象：运行时可选的 `PtySystem` 工厂、`CommandBuilder`、`MasterPty`/`SlavePty`/`Child`/`ChildKiller` 一组 trait，以及 Unix termios 与 Windows ConPTY 两套实现。

Go 生态尚无与它**能力对等**的库。本项目的目标是把这份能力迁移到 Go，为后续 Go 语言终端类应用（终端复用器、远程 shell、IDE 终端面板等）提供统一底座。

### 1.2 目标

1. **能力对等**：`portable-pty` 的每一项能力在 Go 侧都有对应物，或明确记录为何不做（见 §6）。
2. **Go 惯用外形**：命名与形状服从 Go 惯例，不做 Rust trait 的逐字镜像。
3. **纯 Go**：无 cgo、无外部 DLL、无 C 依赖。
4. **跨平台一致**：同一段用户代码在 linux / darwin / Windows 上行为一致（差异需被归一化，见 §4 D7）。

### 1.3 非目标

| 不做 | 理由 |
|---|---|
| `winpty` 回退 | 需要第三方 DLL，与「纯 Go」目标冲突；ConPTY 已覆盖 Win10 1809+ |
| `Downcast` 等价物 | Go 有类型断言，无此需求 |
| `serde_support` 等价物 | Go 用户可用 `encoding/json` 自行处理 |
| Solaris / illumos / AIX | 走 STREAMS 或自有 API，属另一套实现（见 §7.2） |
| `os/exec` 级别的完整兼容 | Windows 上无法使用 `os/exec`，见 §3.2 |

---

## 2. 现状调查

### 2.1 环境（已核实）

| 项 | 事实 |
|---|---|
| 工具链 | `c`（即 `. ~/cot/bin/activate`）→ Go **1.27.1**，`GOROOT=/Users/q/cot/lib/go_1.27.1_darwin_arm64`，`GOPATH=/Users/q/cot/repo/go` |
| 陷阱 | `/usr/local/go` 是 **1.17.6** 且不在 PATH；不要用它 |
| 代理 | `GOPROXY=https://goproxy.cn,direct`，网络可用 |
| 沙箱 | `GOCACHE`（`~/Library/Caches/go-build`）与 `GOMODCACHE`（`/Users/q/cot/repo/go/pkg/mod`）**均不可写**；仅 workspace 与 `/tmp` 可写 |
| 应对 | 构建/测试时把 `GOCACHE`、`GOMODCACHE` 指向 `/tmp`（已确认决策） |
| **设备访问** | workspace-write 沙箱下打开 `/dev/ptmx` 返回 **`EPERM`**。PTY 的实测与测试必须放宽到 `danger-full-access`，或放到外部终端 / CI 运行。这是本项目的硬性开发前提。 |
| 容器 | 本机**没有** docker / podman / colima / lima / orbstack / qemu，因此 **Linux 行为无法本地验证**（见 §10 T2） |
| 远端仓库 | `github.com/qiuzhanghua/portable-pty` 尚未创建（404） |

### 2.2 Go 生态现状

**`github.com/creack/pty`** —— Unix PTY 事实标准。API：`Open` / `Start` / `StartWithSize` / `StartWithAttrs` / `InheritSize` / `Setsize` / `Getsize` / `GetsizeFull`，`Winsize` 结构体。仅 Unix。

**`github.com/aymanbagabas/go-pty` v0.2.3** —— MIT，仓库建于 2023-07，最近 push 2026-09，88★、4 个 open issue、未归档。**它已经是 portable-pty 的 Go 版**：Unix 侧基于 `creack/pty`，Windows 侧基于 `microsoft/hcsshim` 的 ConPTY。API 为 `New() (Pty, error)`，其中 `Pty` 是 `io.ReadWriteCloser` + `Name/Command/CommandContext/Resize/Fd`，命令类型 `Cmd` 照抄 `os/exec.Cmd` 形状；另有 `UnixPty{Master,Slave,Control,SetWinsize}`、`ConPty{InputPipe,OutputPipe}`、`ApplyTerminalModes`（因此依赖 `golang.org/x/crypto/ssh`）。

**结论：真正缺的不是 PTY 本体，而是 portable-pty 的抽象层。**

| portable-pty 能力 | creack/pty | go-pty |
|---|:---:|:---:|
| `PtySystem` 运行时可换工厂 | ✗ | ✗（`New()` 编译期定死） |
| `CommandBuilder` | ✗ | ✗ |
| `try_clone_reader` / `take_writer` 句柄模型 | ✗ | ✗ |
| `ExitStatus{code, signal}` | ✗ | ✗ |
| `ChildKiller` / `clone_killer` | ✗ | ✗ |
| `serial` | ✗ | ✗ |

### 2.3 与上游的关系

本项目是**独立实现**，不 fork、不包装 `go-pty`。`creack/pty`、`go-pty`、`microsoft/hcsshim` 仅作为**行为参照**（尤其是 Unix 打开流程与 ConPTY 调用序列），不进入依赖。

---

## 3. 已核实的技术约束

以下全部来自一手来源（Go 1.27.1 标准库源码、`x/sys@v0.42.0` 源码）。

### 3.1 Windows：ConPTY 原语齐备 ✅

`x/sys/windows@v0.42.0` 已导出实现 ConPTY 所需的**全部**原语，**不需要** `syscall.NewLazyDLL`：

```go
windows.CreatePseudoConsole(size Coord, in Handle, out Handle, flags uint32, pconsole *Handle) error
windows.ResizePseudoConsole(pconsole Handle, size Coord) error
windows.ClosePseudoConsole(console Handle)                    // 无 error 返回值
windows.NewProcThreadAttributeList(maxAttrCount uint32) (*ProcThreadAttributeListContainer, error)
        (*ProcThreadAttributeListContainer).Update(attr uintptr, value unsafe.Pointer, size uintptr) error
        (*ProcThreadAttributeListContainer).List() *ProcThreadAttributeList
        (*ProcThreadAttributeListContainer).Delete()

windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE = 0x00020016
windows.EXTENDED_STARTUPINFO_PRESENT / STARTF_USESTDHANDLES
windows.PSEUDOCONSOLE_INHERIT_CURSOR = 0x1
windows.Coord{X, Y int16}
windows.StartupInfoEx{StartupInfo; ProcThreadAttributeList *ProcThreadAttributeList}
windows.ProcessInformation
windows.CreateProcess(appName, commandLine *uint16, procSecurity, threadSecurity *SecurityAttributes,
                      inheritHandles bool, creationFlags uint32, env, currentDir *uint16,
                      startupInfo *StartupInfo, outProcInfo *ProcessInformation) error
windows.EscapeArg(string) string
windows.ComposeCommandLine([]string) string
```

注意两处易错点（已核实）：
- `ClosePseudoConsole` **不返回 error**。
- `StartupInfoEx` 的字段名是 `ProcThreadAttributeList`（不是 `AttributeList`）。

### 3.2 Windows：`os/exec` **不可用** ❌

Go 1.27.1 的 `src/syscall/exec_windows.go` 中 `SysProcAttr` 字段为：

`HideWindow`、`CmdLine`、`CreationFlags`、`Token`、`ProcessAttributes`、`ThreadAttributes`、`NoInheritHandles`、`AdditionalInheritedHandles`、`ParentProcess`

**没有 `PseudoConsole`**。这正是 [golang/go#62708](https://github.com/golang/go/issues/62708) 未解决的原因：用 `os/exec` 启动的进程无法被附加到 pseudoconsole。

→ **这是本设计最硬的约束**：Windows 必须自己调 `CreateProcess` + `StartupInfoEx`。

### 3.3 Unix：`os/exec` 够用 ✅

`syscall.SysProcAttr`（Darwin/BSD 见 `exec_bsd.go`）具备 `Setsid`、`Setctty`、`Ctty`、`Pgid`、`Foreground`、`Noctty`；`forkAndExecInChild` 中确实会执行 `setsid` 与 `TIOCSCTTY`。因此 Unix 侧可以复用 `os/exec` 成熟的 `LookPath` 与启动流程。

### 3.4 `x/sys/unix` 提供 PTY 所需 ioctl ✅

`IoctlGetWinsize` / `IoctlSetWinsize` / `IoctlGetTermios` / `IoctlSetTermios`，常量 `TIOCGWINSZ` / `TIOCSWINSZ` / `TIOCSCTTY` / `TIOCGPGRP` / `TIOCSPGRP`。

> 注：`Grantpt` / `Unlockpt` / `Ptsname` 在 `x/sys/unix` 中**只对 zos 导出**，linux/darwin/BSD 需自行封装 ioctl（darwin 用 `TIOCPTYGRANT` / `TIOCPTYUNLK` / `TIOCPTYGNAME`，linux 用 `TIOCGPTN` / `TIOCSPTLCK`）。

### 3.5 Go `os.File` 的 poller 语义（本设计的关键依据）✅

**已由实测确认**（darwin/arm64，探针与结果见 §10）。原始依据来自 `src/os/file_unix.go`：

1. **`os.OpenFile` 打开 `/dev/ptmx` → 自动进入 netpoller。** `kindOpenFile` 使 `pollable` 初值为 true；darwin 上仅 `S_IFREG`/`S_IFDIR`/`S_IFIFO` 会被置为不可 poll（字符设备不受影响）。随后因打开时未带 `O_NONBLOCK`，Go 会调用 `syscall.SetNonblock(fd, true)` 并设 `f.nonblock = true`，再 `pfd.Init("file", true)`。

   → **读 master 不会占用 OS 线程；`Close()` 能唤醒阻塞中的 `Read`。**

2. **`os.NewFile` 在 1.27.1 会检查 `O_NONBLOCK`。** `newFileFromNewFile` 先 `unix.Fcntl(fd, F_GETFL, 0)`，再把 `unix.HasNonblockFlag(flags)` 作为 `nonBlocking` 传入 `newFile`；而 `pollable := kind == kindOpenFile || kind == kindPipe || kind == kindSock || nonBlocking`。

   → 由于 `dup` 共享同一个 open file description，`O_NONBLOCK` 会被继承，**所以 dup 出来的 fd 也能进入 netpoller**，且各 dup 是不同 fd，注册互不干扰。这使 Rust 式「dup 出独立 reader」在 Go 上可复刻。

   ✅ **不是版本限制**：实测 go1.18.10 / go1.22.0 / go1.27.1 **均**满足该行为（更早版本走等价的 `kindNonBlock` 判定）。因此「dup 出独立 reader」的设计**不受 Go 版本约束**；版本下限改由 `//go:build unix`（≥ 1.19）与所需 `x/sys` 版本决定（见 §10 T1）。

3. **`(*os.File).Fd()` 会破坏 poller。** `Fd()` 中 `if f.nonblock { f.pfd.SetBlocking() }` —— 清除 `O_NONBLOCK`。而该标志被同一 open file description 的所有 dup **共享**，因此**一次 `Fd()` 会让全部句柄退化为阻塞并破坏 poller 语义**。

   **`SyscallConn()` 无此副作用**（`os.file.SyscallConn` → `newRawConn` → `rawConn.Control` → `poll.FD.RawControl`，其实现只有 `incref` / `f(uintptr(fd.Sysfd))` / `decref`，**没有** `SetBlocking`）。→ 需要裸 fd 时一律用 `SyscallConn()`。

4. **`creack/pty` 走的是更差的一条路。** 它用 `syscall.Open` + `os.NewFile` 打开 master（见其 `pty_darwin.go`）：`kindNewFile` 且非阻塞位未置 → `pollable = false`，**阻塞且不在 poller 中**。实测结果是**阻塞中的 `Read` 在 `Close()` 之后 3 秒仍未唤醒，OS 线程被永久钉住**（§10.1 T3）。本项目要做得更好，必须用 `os.OpenFile`。

### 3.6 `EIO` vs `EOF` 的平台差异 ⚠️

slave 全部关闭后：**Linux 读 master 返回 `EIO`**，**darwin 返回 `0`（即 EOF）**。

- **darwin：已实测确认** —— `n=0, err=EOF`（`EIO` 未出现）。详见 §10 T2。
- **Linux：仍为 `UNVERIFIED`** —— 本机没有容器运行时（§2.1），**无法本地验证**，只能靠 CI。这是 D7 归一化唯一尚未闭合的环节。

→ 结论不变：必须归一化，否则两平台行为不一致；归一化的代码路径在 Linux 上待 CI 验证。

### 3.7 Windows ConPTY 调用序列（已由两个独立实现交叉确认）✅

`go-pty`（`pty_windows.go` / `cmd_windows.go`）与 `microsoft/hcsshim`（`internal/conpty/conpty.go`）的实现**逐项一致**，可直接作为 M3 的施工图。关键点（含易错项）：

**1. 两条管道，方向极易写反**

```go
ptyIn, inPipeOurs := os.Pipe()    // console 从 ptyIn 读输入；我们写 inPipeOurs
outPipeOurs, ptyOut := os.Pipe()  // console 向 ptyOut 写输出；我们读 outPipeOurs

windows.CreatePseudoConsole(windows.Coord{X: cols, Y: rows},
    windows.Handle(ptyIn.Fd()), windows.Handle(ptyOut.Fd()), 0, &hpc)
ptyIn.Close(); ptyOut.Close()     // console 已 dup 过，自己这侧要关
```

**2. attribute list：句柄按值作为指针传入**

```go
attrs, _ := windows.NewProcThreadAttributeList(1)
attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
             unsafe.Pointer(hpc),        // 注意是 hpc 的值，不是 &hpc
             unsafe.Sizeof(hpc))
```

> 我们比参照实现占优的一点：`x/sys@v0.42.0` **已导出** `windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE`，无需像 `go-pty` / `hcsshim` 那样自带常量或走 `internal/winapi`。

**3. `StartupInfoEx`：`Cb` 必须是 `sizeof(StartupInfoEx)`**

```go
siEx := new(windows.StartupInfoEx)
siEx.Flags = windows.STARTF_USESTDHANDLES
siEx.ProcThreadAttributeList = attrs.List()
siEx.Cb = uint32(unsafe.Sizeof(*siEx))   // ← 不是 sizeof(StartupInfo)
```

⚠️ 两个实现**都只设 `STARTF_USESTDHANDLES`，不填 `StdInput`/`StdOutput`/`StdErr`** —— 标准句柄由 pseudoconsole 提供。

**4. `CreateProcess`**

```go
flags := windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT | extra
windows.CreateProcess(exeW, cmdLineW, pSec, tSec,
                      false,                  // bInheritHandles
                      flags, envBlockW, dirW, &siEx.StartupInfo, &pi)
```

- `pSec` / `tSec` 需 `InheritHandle: 1`。
- `envBlockW` 为 UTF-16、双 NUL 结尾的环境块。
- ⚠️ **必须含 `SYSTEMROOT`**，否则进程可能起不来（两个实现都为此写了 `addCriticalEnv`）。
- 环境变量需按**大小写不敏感**去重。
- 命令行用 `windows.ComposeCommandLine`（已有正确实现，不必自己写 MSVCRT 引号）。
- `argv0` 若相对 `Dir` 需要先绝对化（`CreateProcess` 的 cwd 语义与 `exec.Cmd.Dir` 不同）。

**5. 收尾**

- 立即 `CloseHandle(pi.Thread)`。
- 用 `os.FindProcess(int(pi.ProcessId))` 换一个 `*os.Process`，**复用标准库的等待 / 退出码逻辑**，省掉手写 `WaitForSingleObject`。
- attribute list 必须**活过 `CreateProcess`**，并在不再需要时 `attrs.Delete()`。
- 最后 `ClosePseudoConsole(hpc)`，再关自己这侧的两条管道。

**6. 与 §5 API 的衔接**

Windows 无信号概念 → `ExitStatus.Signal` 恒为空；`Resize` 走 `ResizePseudoConsole`；`Name()` 返回固定串。

> 仍待 CI 验证（§10 T5）：以上为**参照实现交叉确认**，尚未在真实 Windows 上跑通本项目的代码。

### 3.8 Darwin：slave 未打开前，master 上的 winsize ioctl 会失败 ✅（实测发现）

在 darwin 上对**刚打开、且从未有过 slave** 的 pty master 调用 `TIOCSWINSZ` / `TIOCGWINSZ`，内核返回 **`ENOTTY`**；一旦打开过 slave，两者立刻成功。

实测输出（`/dev/ptmx`，darwin/arm64）：

```
TIOCPTYGNAME -> "/dev/ttys001"   err=<nil>
TIOCPTYGRANT -> <nil>
TIOCPTYUNLK  -> <nil>
--- 尚未打开 slave ---
IoctlSetWinsize -> inappropriate ioctl for device
IoctlGetWinsize -> inappropriate ioctl for device
--- 打开 slave 之后 ---
IoctlSetWinsize -> <nil>
IoctlGetWinsize -> <nil>  {Row:24 Col:80}
```

**这条直接改写了 D2 的形态**：`OpenPty` 必须**先打开 slave 并持有它**，否则连窗口尺寸都设不了，`Resize()` / `Size()` 在 spawn 之前也会失效。这也解释了上游为什么用 `PtyPair` 一次返回两端 —— 不是设计品味，是系统约束。Linux 上不存在该限制，但实现走同一条路。

> 附带确认：`unix.Syscall`（x/sys）与 `syscall.Syscall`（stdlib）在 darwin 上**都能**完成 `TIOCPTYGNAME` / `TIOCPTYGRANT` / `TIOCPTYUNLK`。最初怀疑 x/sys 有问题是一次误判，已回退。

### 3.9 子进程的 stdio 之所以是阻塞的

`os.OpenFile` 会把 slave 也放进 netpoller 并置 `O_NONBLOCK`。子进程会用 `dup2` 继承**同一个 open file description**，于是会连带继承 `O_NONBLOCK` —— 那会让 shell 之类程序在写 stdout 时拿到 `EAGAIN`。

之所以没出问题：`os/exec` 在启动前会对每个 child file 调用 `(*os.File).Fd()`（`os/exec_posix.go`：`sysattr.Files = append(sysattr.Files, f.Fd())`），而 §3.5 第 3 条的副作用会**清掉 `O_NONBLOCK`**。

也就是说，这里的正确性**依赖于那个「陷阱」**。它已被 `TestChildStdioIsBlocking` 显式钉住（子进程自我检查 fd 0/1/2 的 `F_GETFL`），不靠假设。

---

## 4. 核心设计决策

### D1 —— `Spawn(*exec.Cmd)` 作为统一入口

> **Unix 交给 `os/exec`；Windows 从 `*exec.Cmd` 取出字段，自己调 `CreateProcess`。**

这是全局最关键的一条，同时解决三个问题：

1. Windows 被迫放弃 `os/exec`（§3.2），但输入类型仍是 `*exec.Cmd`，**用户代码在两个平台完全一致**。
2. 不必照搬 `CommandBuilder` 类型：Go 已有的能力（`LookPath`、`Dir`、`Env`）直接复用。
3. 不需要 `PtyPair` 的「幽灵 slave」（见 D2）。

备选方案（已否决）：
- 自建 `portablepty.Command` 类型 → 与 `os/exec` 重复，且用户无法复用已有的 `*exec.Cmd` 构造与测试代码。
- Windows 走 `cmd.exe` 包装以绕过 `CreateProcess` → 改动语义（多一层 shell、环境/引号双重处理），不可接受。

**实现要点**：Windows 上自行 `CreateProcess` 会绕过 `exec.Cmd.Start`，因此 `cmd.Process` / `cmd.ProcessState` / `cmd.Cancel` / `cmd.WaitDelay` **不会**由 Go 填充，须由我们的 `Child` 接管。此限制必须写进 Godoc。

### D2 —— 不做 `PtyPair`，`System.OpenPty` 直接返回 `Master`

Rust 用 `PtyPair{slave, master}` 且依赖 Drop 顺序（`lib.rs` 注释明写 `slave is listed first so that it is dropped first`）。Go **没有确定性析构**，只有不可靠的 `runtime.SetFinalizer`，因此必须显式 `Close()`。

更根本的问题：**ConPTY 没有 slave 概念**（只有 pseudoconsole + 两条管道），保留 `Slave` 会在 Windows 上产生幽灵对象。

→ 采用 `System.OpenPty(size) (Master, error)`，spawn 挂在 `Master` 上。

**但 slave 仍然要开，只是不对外暴露。** §3.8 的实测表明：darwin 上 master 在 slave 从未打开过时连 winsize 都设不了。因此 `OpenPty` 会**自己打开 slave 并持有它**，用它完成尺寸设置，并在首次 `Spawn` 时把它交给子进程，随即**关闭父进程这侧的副本**（否则 master 永远看不到会话结束，见 §8 陷阱 4）。

所以最终形态是「对外没有 Slave 对象，对内 slave 由 Master 代管」—— 既避开了 Windows 上的幽灵对象，也满足了 darwin 的系统约束。若后续再 `Spawn`，`spawnSlave` 会按同一路径重新打开一个。

### D3 —— `CommandBuilder` 拆成辅助函数 + `SpawnOption`

`CommandBuilder` 的能力按来源拆分：

| 能力 | Go 侧处置 |
|---|---|
| `arg`/`args`/`cwd`/`env`/`env_clear`/`env_remove` | 直接复用 `*exec.Cmd` 的 `Args`/`Dir`/`Env` |
| PATH 解析 | 复用 `exec.LookPath`（Unix） |
| base env 快照 | `pty.Environ()` |
| login shell（`argv0 = -bash`） | `pty.LoginShell()` |
| `controlling_tty` 开关 | `pty.WithControllingTTY(bool)` |
| `umask` | ❌ **Go 做不到**，已从 API 移除 |
| **Windows 注册表环境合并**（HKLM+HKCU，`REG_EXPAND_SZ` 展开、PATH 拼接） | `pty.Environ()` 内实现 |
| **`PATHEXT` 搜索** | Windows 侧 `CreateProcess` 前自行解析 |
| **MSVCRT 引号规则** | 改用 `windows.ComposeCommandLine`（Go 已有正确实现） |
| Windows UTF-16 环境块 | 自行构造（`CreateEnvironmentBlock` 需要 token，不适用） |
| 大小写不敏感环境键 | Windows 侧规范化 |

→ **不引入新的命令类型**，用 `pty.Command(name, args...)` / `pty.LoginShell()` 构造 `*exec.Cmd`，用 `SpawnOption` 承载 spawn 期参数。

**为什么 `umask` 被砍掉（设计变更）**：Rust 用 `pre_exec` 在 fork 之后、exec 之前于**子进程**里调 `umask()`。Go **没有等价的钩子**：`syscall.SysProcAttr` 在 linux 与 darwin 上都没有 `Umask` 字段（已逐个核对完整结构体），而 `os/exec` 也不提供 fork/exec 之间的回调。

剩下的两条路都不该走：
- **父进程里 `syscall.Umask` 前后包夹 `cmd.Start()`** —— umask 是**进程全局**的，会与其它 goroutine 并发创建文件产生竞态，静默写出错误权限的文件。库不该这么干。
- **用 `/bin/sh -c 'umask NNN; exec ...'` 包一层** —— 改变语义（多一层 shell、命令行要二次转义），和 D1 否决 `cmd.exe` 包装是同一个理由。

因此这是相对上游的一处**能力缺口**，已在 §6 如实标为 ❌。若将来 Go 增加 `SysProcAttr.Umask`，应第一时间补回。

### D4 —— reader/writer 所有权：`io.ReadWriteCloser` + `CloseWrite()`

Rust 的 `try_clone_reader()` / `take_writer()` 在 Go 里不必逐字复刻：`*os.File` 本身支持并发 `Read`/`Write`/`Close`，所以「多句柄」不是必需能力。

但 `take_writer` 表达了一个真实需求：**只关闭写方向以向 slave 送 EOF，同时保持读**。Go 惯例对应物是 `net.TCPConn.CloseWrite()`。→ 提供 `Master.CloseWrite() error`。

**⚠️ 语义诚实性（必须写进 Godoc）**：pty 上「真正的半关」并不成立 —— master 只有**一个** fd，只有**最后一个** master 句柄关闭时 slave 才会收到 `SIGHUP`/`EIO`。Rust 文档中 "Dropping the writer will send EOF to the slave end" 对 pty 而言并不准确。

**实现取舍（已落地）**：原先设想的「dup 出独立写 fd」这条其实**无效** —— 关掉其中一个 dup 不会让 slave 收到 EOF，因为同一 open file description 的另一个 dup 仍然存活，而且 pty 设备本身没有「写方向」可关。所以 `CloseWrite()` 实现为**逻辑半关**：

- 之后的 `Write` 一律返回 `os.ErrClosed`（与 `Close()` 之后的错误一致，方便 `errors.Is`）；
- `Read` 完全不受影响；
- **不关任何 fd**，因此**不会**挂断 slave。

Godoc 必须写明：
- `CloseWrite()` 只是逻辑半关，**不会**让 slave 看到 EOF；
- 需要真正终止会话时应使用 `Close()`；
- 需要「发送 EOF 字符」语义时应向 master **写 VEOF（通常是 `^D`）**。

保留 `CloseWrite()` 是为了能力对等，但主推 `Close()` 与「写 VEOF」两种正经做法。

### D5 —— `ExitStatus` 与 `Killer` 语义对齐上游

`Killer.Kill()` 在 Unix 上必须复刻 `portable-pty` 的**原始语义**（见其 `lib.rs`）：

1. 先发 `SIGHUP`；
2. 若失败（`kill` 返回非 0）→ 返回 `os.LastError`；
3. 成功后进入**宽限期**：最多 5 次探测，每次间隔 50ms，用 `TryWait` 检查是否已退出；
4. 若宽限期结束仍存活 → 再执行真正的强杀。

`ExitStatus.Signal` 为信号名字符串（上游用 `strsignal`，无法解析时退回 `"Signal N"`）。`String()` 对齐上游 `Display`：`"Success"` / `"Terminated by X"` / `"Exited with code N"`。

`CloneKiller()` 的存在理由：让调用方能把 killer 从可能阻塞在 `Wait()` 的 goroutine 中**分离**出来。

### D6 —— 暴露 `SyscallConn()` 而非 `Fd()`

理由见 §3.5 第 3 条：`Fd()` 调用 `SetBlocking()`，会通过共享的 open file description 破坏**所有** dup 句柄的 poller 语义。

上游的 `unix::as_raw_fd()` 在 Go 里用 `SyscallConn() (syscall.RawConn, error)` 表达，既能做 ioctl，又无副作用。

### D7 —— `EIO → io.EOF` 归一化

slave 全关后 Linux 返回 `EIO`、darwin 返回 EOF（§3.6）。若不归一化，同一段用户代码在两个平台表现不同，违背 §1.2 目标 4。

→ 在读路径上把 `syscall.EIO` 映射为 `io.EOF`。（是否额外提供「可关闭归一化」的开关：**否**，保持 API 小；确需区分者可用 `errors.Is` 之前的底层接口。若后续出现真实需求再议。）

### D8 —— 依赖与命名

- **依赖只有 `golang.org/x/sys`**（`unix` + `windows`）。**不引** `golang.org/x/crypto/ssh`（`go-pty` 为 `TerminalModes` 引了，我们不提供该能力）。
- **包名 `pty`**，避免 `portablepty.PtySize` 这类 stutter。
- 控制端类型名 **`Master`**（而非 `Pty`），避免 `pty.Pty` 这种重复。
- 模块路径保留 `github.com/qiuzhanghua/portable-pty`。注意：与 Rust crate 同名，检索时可能混淆，需在 README 首段说明关系与差异。

---

## 5. API 契约（签名级）

> 本节是设计稿，不含实现。

```go
package pty

// ---------- 值类型 ----------

// Size 描述可见显示区域。PixelWidth/PixelHeight 是单元格的像素尺寸，
// 部分系统忽略该值。
type Size struct {
    Rows, Cols              uint16
    PixelWidth, PixelHeight uint16
}

// ExitStatus 描述子进程退出状态。
type ExitStatus struct {
    Code   int
    Signal string // 为空表示非信号终止
}

func (s ExitStatus) Success() bool
func (s ExitStatus) String() string

// ---------- 工厂（替代 PtySystem + native_pty_system） ----------

type System interface {
    OpenPty(size Size) (Master, error)
}

// Native 返回当前平台的 PtySystem。
func Native() System

// ---------- 控制端（替代 MasterPty + SlavePty） ----------

type Master interface {
    io.ReadWriteCloser

    // CloseWrite 逻辑上关闭写方向：之后 Write 返回 os.ErrClosed，Read 不受影响。
    // 它不关 fd，因此不会让 slave 看到 EOF；真正的挂断请用 Close。
    // 详见 D4 的语义说明。
    CloseWrite() error

    Resize(size Size) error        // 上游 MasterPty::resize
    Size() (Size, error)           // 上游 MasterPty::get_size
    Name() string                  // 上游 unix::tty_name

    Spawn(cmd *exec.Cmd, opts ...SpawnOption) (Child, error)
}

// ---------- 进程（替代 Child + ChildKiller） ----------

type Child interface {
    Wait() (ExitStatus, error)
    TryWait() (ExitStatus, bool, error) // bool 表示是否已结束
    PID() int
    Kill() error
    CloneKiller() Killer
}

// Killer 可与阻塞在 Wait 的 goroutine 分离使用。
type Killer interface {
    Kill() error
    CloneKiller() Killer
}

// ---------- spawn 期选项（替代 CommandBuilder 的 umask / controlling_tty） ----------

type SpawnOption func(*spawnConfig)

func WithControllingTTY(bool) SpawnOption

// 上游 CommandBuilder 的 umask 无法在 Go 中实现，故不提供（见 D3）。

// ---------- 命令辅助（替代 CommandBuilder） ----------

// Command 构造 *exec.Cmd，并预置 base env 快照。
func Command(name string, args ...string) *exec.Cmd

// LoginShell 构造默认登录 shell，argv0 形如 "-bash"。
func LoginShell() *exec.Cmd

// Environ 返回 base env 快照；Windows 上包含注册表环境合并。
func Environ() []string
```

### Unix 专属能力（build tag 隔离）

```go
//go:build unix

type UnixMaster interface {
    Master
    Termios() (*unix.Termios, error)          // 上游 unix::get_termios
    Pgrp() (int, error)                       // 上游 unix::process_group_leader
    SyscallConn() (syscall.RawConn, error)    // 替代 unix::as_raw_fd，见 D6
}
```

### Windows 专属能力

```go
//go:build windows

type WindowsMaster interface {
    Master
    // 待定：ConPTY 句柄/管道是否需要暴露给调用方。
    // 参考 go-pty 的 ConPty{InputPipe, OutputPipe}。
}
```

---

## 6. 能力对等清单

| `portable-pty` 能力 | 本设计 | 状态 |
|---|---|:---:|
| `PtySize` | `Size` | ✅ |
| `PtySystem` / `native_pty_system()` | `System` / `Native()` | ✅ |
| `MasterPty::resize` | `Master.Resize` | ✅ |
| `MasterPty::get_size` | `Master.Size` | ✅ |
| `MasterPty::try_clone_reader` | 内建 `io.Reader`（Unix 可 dup 独立句柄） | ✅ 语义等价，形状不同 |
| `MasterPty::take_writer` | `io.Writer` + `CloseWrite()`（逻辑半关） | ⚠️ 见 D4 |
| `unix::tty_name` | `Master.Name()` | ✅ |
| `unix::get_termios` | `UnixMaster.Termios()` | ✅ |
| `unix::process_group_leader` | `UnixMaster.Pgrp()` | ✅ |
| `unix::as_raw_fd` | `UnixMaster.SyscallConn()` | ✅ 更安全 |
| `Child::try_wait` | `Child.TryWait` | ✅ |
| `Child::wait` | `Child.Wait` | ✅ |
| `Child::process_id` | `Child.PID` | ✅ |
| `ChildKiller::kill` | `Killer.Kill`（含 SIGHUP→宽限→强杀） | ✅ |
| `ChildKiller::clone_killer` | `Killer.CloneKiller` | ✅ |
| `ExitStatus` | `ExitStatus` | ✅ |
| `CommandBuilder` | `Command`/`LoginShell`/`Environ` + `SpawnOption` | ✅ 能力覆盖 |
| `serial` | M5 实现 | ✅ 计划内 |
| `winpty` 回退 | 不做 | ❌ 非目标 |
| `Downcast` | 不做 | ❌ 非目标 |
| `serde_support` | 不做 | ❌ 非目标 |

---

## 7. 平台与构建策略

### 7.1 目标平台

| 平台 | 状态（M1 之后） |
|---|---|
| linux | ✅ 已实现 |
| darwin | ✅ 已实现（主要开发与验证平台） |
| windows | ⏳ M3 计划（ConPTY，Win10 1809 / build 17763+） |
| freebsd / openbsd / netbsd | ⏸ **暂缓**，返回 `ErrUnsupported` |
| 其他 Unix（solaris、illumos、aix、zos…） | `ErrUnsupported` |

**为什么 BSD 被暂缓（对用户原决定的修正）**：三个 BSD 各有一套互不相同的 open/grant/unlock 序列 —— freebsd 走 `posix_openpt` + `FIODGNAME`；openbsd 只有一个 `/dev/ptm` + `PTMGET`，一次 ioctl 同时返回两端 fd；netbsd 才是 `/dev/ptmx` + `TIOCPTSNAME` + `TIOCGRANTPT`。而且它们取到的都是**裸 fd**，要进 netpoller 必须先自己 `SetNonblock` 再 `os.NewFile`（§3.5 第 2 条）。

本项目**没有任何 BSD CI**（cross-compile 只能证明能编译，不能证明能跑）。把无法验证的代码标成「支持」是负债，不是能力。因此 M1 只交付能真实验证的平台，BSD 留待有验证手段时再补；`cross-compile` 矩阵仍保留它们，确保至少编译不被破坏。

构建约束相应收紧为 `//go:build linux || darwin`，其余平台一律 `ErrUnsupported`。

### 7.2 为什么不做 Solaris/illumos

它们走 STREAMS（`grantpt`/`unlockpt`/`ptsname` 语义与 `TIOCPTMGET` 均不同，`creack/pty` 为此有独立文件）。首版用 `ErrUnsupported` 明确拒绝，好过静默出错。

### 7.3 文件布局（草案）

```
pty.go                // Size / ExitStatus / System / Master / Child / Killer / Native
cmd.go                // Command / LoginShell / Environ / SpawnOption
system_unix.go        // //go:build unix —— UnixSystem.OpenPty
system_windows.go     // //go:build windows —— ConPtySystem.OpenPty + 可用性探测
master_unix.go        // openpt/grantpt/unlockpt/ptsname、Resize/Size/Termios/Pgrp
master_windows.go     // CreatePseudoConsole、管道、Resize、ClosePseudoConsole
spawn_unix.go         // os/exec 路径 + Setsid/Setctty + 把 slave 交给子进程
spawn_windows.go      // CreateProcess + StartupInfoEx + env block + ComposeCommandLine
child_unix.go         // Child/Killer（SIGHUP→宽限→强杀）
child_windows.go      // Child/Killer（TerminateProcess / 句柄）
serial.go             // M5
```

### 7.4 CI（已建立：`.github/workflows/ci.yml`）

两个 job：

**1. `test` —— 三平台原生矩阵**（`ubuntu-latest` / `macos-latest` / `windows-latest`）
依次执行：`go mod tidy` 差分检查（仅 Linux）、`gofmt -l`、`go build`、`go vet`、`go test`、`go test -race`（非 Windows）。
**这是唯一能验证真实 PTY 行为的 job**，也是闭合 T2b（Linux `EIO`）与 T5（Windows ConPTY）的地方。

**2. `cross-compile` —— 11 个目标平台**

| 类别 | 目标 |
|---|---|
| 支持的 Unix | linux/amd64, linux/arm64, darwin/arm64, darwin/amd64, freebsd/amd64, openbsd/amd64, netbsd/amd64 |
| ConPTY | windows/amd64, windows/arm64 |
| 明确不支持（须仍能编译） | solaris/amd64, aix/ppc64 |

每个目标跑 `go build` + `go vet`（`CGO_ENABLED=0`）。**这是唯一能抓出 build tag 写错的手段** —— 单平台构建永远发现不了 `master_unix.go` / `master_windows.go` / unsupported stub 的分派错误。

其他约定：
- `GOTOOLCHAIN: local` —— `go.mod` 是 Go 版本的唯一真相，禁止 CI 静默升级工具链；
- `go-version-file: go.mod` —— CI 与 `go.mod` 自动同步；
- 并发组 `cancel-in-progress` —— 同一 ref 的新推送取消旧运行；
- **`.gitattributes` 强制 `eol=lf`** —— GitHub 的 Windows runner 上 `core.autocrlf` 默认为 `true`，没有这个文件时检出会把 LF 全转成 CRLF，`gofmt -l` 就会把**每一个** `.go` 文件都报成未格式化。**这不是理论风险：CI 首次运行就是这样挂在 `windows-latest` 的 `gofmt` 上（Linux/macOS 通过），并连带跳过了 Build/Vet/Test。**

### 7.5 CI 首跑记录（2026-10-02）

| 运行 | commit | 结果 |
|---|---|---|
| [#1](https://github.com/qiuzhanghua/portable-pty/actions/runs/36949000233) | `ecbf66c` | ❌ 13/14 —— `test (windows-latest)` 在 `gofmt` 步失败（CRLF，见 §7.4） |
| [#2](https://github.com/qiuzhanghua/portable-pty/actions/runs/36949206815) | `6072454` | ✅ **14/14**（3 平台 test + 11 目标 cross-compile） |

> 注意：首跑验证的是**管道本身**。`test` job 目前只跑 `ExitStatus` 等纯逻辑用例，**尚未验证任何 PTY 行为**。T2b（Linux `EIO`）与 T5（Windows ConPTY）要等 M1/M2 有真实实现后才会产生信号。

---

## 8. 必须写进 Godoc 的陷阱

| # | 陷阱 | 说明 |
|---|---|---|
| 1 | **不要调用 `(*os.File).Fd()`** | 会 `SetBlocking()` 清除 `O_NONBLOCK`，而该标志被所有 dup 共享 → 破坏全部句柄的 poller 语义。用 `SyscallConn()`。（§3.5.3，**已实测确认**） |
| 2 | **`CloseWrite()` 不会让 slave 收到 EOF** | 它只让后续 Write 返回 `os.ErrClosed`；pty 只有单个 master fd，真正的半关不成立。（D4） |
| 3 | **Windows 上 `cmd.Process`/`ProcessState`/`Cancel`/`WaitDelay` 不生效** | 我们自行 `CreateProcess`，绕过了 `exec.Cmd.Start`。（D1） |
| 4 | **父进程必须释放 slave** | Unix 上父进程若持有 slave fd，master 永远看不到 EOF/HUP（`creack/pty` 的做法是 spawn 后立即关闭）。`Spawn` 默认应关掉父进程侧的 slave 并文档化。 |
| 5 | **`Close()` 之后 `Read` 返回 `os.ErrClosed`，而不是 `io.EOF`** | 需在文档与测试中明确区分「正常 EOF」与「本地关闭」。**已实测确认**：darwin 上返回 `read /dev/ptmx: file already closed`。 |
| 6 | **`Resize` 只影响 winsize，不重绘** | 与上游一致；重绘由子进程自行处理（收到 `SIGWINCH`）。 |
| 7 | **读 master 返回 `io.EOF` 表示 slave 侧全部关闭** | darwin 已确认；Linux 待 CI（§10.2 T2b）。这是判断会话结束的唯一可靠信号。 |

---

## 9. 里程碑

| 阶段 | 内容 | 完成判据 |
|---|---|---|
| **M0** | 骨架、接口定稿、CI（三平台）、`go.mod` 定 `go 1.24.0` + `x/sys@v0.41.0` | ✅ **已完成**：接口/类型落地，`gofmt`/`build`/`vet`/`test` 在 go1.24.0 下通过，11 平台交叉 `build`+`vet` 通过；三平台 CI 待仓库建立后首跑 |
| **M1** | Unix：`OpenPty`/`Close`、`Size`/`Resize`、`Spawn`、`ExitStatus`、`Child`/`Killer` | ✅ **darwin 全部通过**（含真实 shell 往返与退出码）；⏳ Linux 由 CI 验证 |
| **M2** | writer 所有权收尾、`EIO→EOF` 归一化回归、阻塞/唤醒用例 | **Linux CI 上闭合 T2b**；`EIO→EOF` 在 linux/darwin 行为一致；`Fd()` 禁用规则有 lint 兜底 |
| **M3** | Windows ConPTY：`CreateProcess` + attribute list + 双管道 + `Resize` | `windows-latest` 上能跑通 `cmd.exe`/`powershell` |
| **M4** | `Command`/`LoginShell`/`Environ`（含注册表环境合并、`PATHEXT`）、`SpawnOption` | 与上游 `CommandBuilder` 行为逐项对照测试 |
| **M5** | `serial`（串口），与 `System`/`Master` 抽象合流 | 能用 `System` 抽象打开串口 |
| **M6** | 文档、README（含与 Rust crate / go-pty 的关系说明）、API 对照表 | 可发布 |

---

## 10. 实测结果

探针为一次性脚本，位于 `/tmp/ptyprobe`（PTY 行为）与 `/tmp/goprobe`（Go 版本 bisect），**均不进入仓库**。环境：darwin/arm64。

### 10.1 已闭合

| # | 问题 | 结论 | 证据 |
|---|---|---|---|
| **T4** | `os.OpenFile` vs `syscall.Open`+`os.NewFile`；dup 能否进 poller | ✅ 与 §3.5 完全一致 | `os.OpenFile` → `O_NONBLOCK=true, POLLABLE`；`syscall.Open`+`NewFile` → `O_NONBLOCK=false, not-pollable`；`dup(os.OpenFile)` → `O_NONBLOCK=true, POLLABLE` |
| **T4b** | `Fd()` 是否真会破坏 poller，是否会波及 dup | ✅ 陷阱属实，且波及 dup | `Fd()` 前 `O_NONBLOCK=true`，之后 `=false`（changed=true）；同一 open file description 的 dup 也变为 `false` |
| **T3** | `Close()` 能否唤醒阻塞中的 `Read` | ✅ pollable 立即唤醒；非 pollable **永久卡死** | `os.OpenFile` → `Read woke 0s after Close`，err=`file already closed`(ErrClosed=true)；`syscall.Open`+`NewFile` → **`Read STILL BLOCKED 3s after Close`（线程被钉住）** |
| **T2a** | darwin：slave 全关后读 master | ✅ 返回 EOF（非 EIO） | `master.Read -> n=0 err=EOF (EOF=true, EIO=false)` |
| **T1** | `os.NewFile` 的 `O_NONBLOCK` 行为从哪个 Go 版本开始 | ✅ **不构成版本限制** | go1.18.10 / go1.22.0 / go1.27.1 **均** `pollable=true` |

**由实测得出的两个额外结论：**

1. **D4 可以按 Rust 式句柄模型落地** —— dup 出的 reader 确实进入 netpoller，且 `Close()` 能可靠唤醒 `Read`。
2. **§3.5 第 4 条被直接证实** —— `creack/pty` 的 `syscall.Open`+`os.NewFile` 路线会让阻塞中的 `Read` 在 `Close()` 之后**永远醒不过来**（实测 3 秒未醒、线程被钉住）。本项目采用 `os.OpenFile` 是**实质性的正确性改进**，不只是风格偏好。

### 10.2 仍未闭合

| # | 问题 | 状态 | 计划 |
|---|---|---|---|
| **T2b** | **Linux** 上 slave 全关后读 master 返回 `EIO` 还是 EOF？是否被 poller 包装？ | ❌ 本机无容器运行时，**无法验证** | Linux CI 真实用例（M2 完成判据） |
| **T5** | Windows ConPTY 序列在本项目代码上是否跑通 | ⚠️ 已由 `go-pty` 与 `hcsshim` **交叉确认**（§3.7），但未在真实 Windows 上验证 | `windows-latest` CI（M3 完成判据） |

### 10.3 Go 版本下限（已定：**`go 1.24`**）

T1 表明 poller 行为不构成约束；下限完全由依赖决定。实测各版本 `golang.org/x/sys` 的 `go` 指令：

| x/sys | `go` 指令 |
|---|---|
| v0.33.0 – v0.35.0 | 1.23.0 |
| **v0.36.0 – v0.41.0** | **1.24.0** |
| v0.42.0 – v0.47.0 | 1.25.0 |
| v0.48.0 | 1.26.0 |

→ 选 **`x/sys@v0.41.0`**，即 `go 1.24.0` 分支里最新的一个（v0.42.0 起需要 1.25）。

**已验证**（不是推断）：

1. v0.41.0 的 ConPTY 与 Unix 符号**全部齐备** —— `CreatePseudoConsole` / `ResizePseudoConsole` / `ClosePseudoConsole` / `NewProcThreadAttributeList` / `Coord` / `StartupInfoEx` / `ProcessInformation` / `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE` / `ComposeCommandLine` / `EscapeArg` / `PSEUDOCONSOLE_INHERIT_CURSOR` / `EXTENDED_STARTUPINFO_PRESENT` / `STARTF_USESTDHANDLES` / `CREATE_UNICODE_ENVIRONMENT` / `CreateProcess`，以及 `IoctlGet/SetWinsize`、`IoctlGet/SetTermios`、`TIOCGWINSZ` / `TIOCSWINSZ` / `TIOCSCTTY`。
2. 在 **go1.24.0** 下对 11 个目标平台（含 `windows/amd64`、`windows/arm64`、`solaris/amd64`、`aix/ppc64`）跑 `go build` + `go vet`，**全部通过**。

最终 `go.mod`：

```
go 1.24.0
require golang.org/x/sys v0.41.0
```

> 代价：比最新 x/sys 落后若干版本。日后若要跟进，必须重新核对 `go` 指令是否抬高了下限。

### 10.4 M1 实现期的新发现

| # | 发现 | 影响 |
|---|---|---|
| **T6** | **darwin 上，slave 从未打开过的 master，`TIOCSWINSZ`/`TIOCGWINSZ` 返回 `ENOTTY`** | 改写 D2：`OpenPty` 必须先打开并持有 slave（§3.8） |
| **T7** | 子进程 stdio 之所以是阻塞的，依赖 `os/exec` 调用 `(*os.File).Fd()` 清掉 `O_NONBLOCK` | 已用 `TestChildStdioIsBlocking` 钉住（§3.9） |
| **T8** | `syscall.SysProcAttr` 在 linux 与 darwin 上**都没有** `Umask` 字段；Go 无 `pre_exec` 等价物 | `WithUmask` 从 API 移除，成为对上游的能力缺口（D3、§6） |
| **T9** | `unix.Syscall` 与 `syscall.Syscall` 在 darwin 上都能完成 PTY 的 ioctl | 一次性误判被实测否证，已回退到 `unix.Syscall`（§3.8 尾注） |

**M1 实测结果（darwin/arm64，go1.24.0）**：13 个用例全部通过，含 `TestInteractiveShell`（真实 `/bin/sh` 往返：写入命令 → 读回 shell 计算出的 `marker-42` → 拿到退出码 3）、`TestKillReportsSignal`（SIGHUP）、`TestCloseWriteStopsWritesButKeepsReads`、`TestTermiosAndPgrp`、`TestChildStdioIsBlocking`。`go test -race` 通过；11 个目标平台 `build` + `vet` + `test -c` 全部通过。

---

## 11. 风险

| 风险 | 影响 | 缓解 |
|---|---|---|
| **Windows 无 CI 则不可测** | M3 沦为盲写，回归无法发现 | M0 就把 `windows-latest` 接进 CI |
| **依赖抬高 Go 门槛** | 已缓解：锁定 `x/sys@v0.41.0` 后门限降到 `go 1.24`（vs 若不锁则需 1.25） | 已用 11 平台 `build`+`vet` 验证；日后升级 `x/sys` 必须重新核对 `go` 指令 |
| **依赖落后于最新 x/sys** | 可能错过上游 bugfix | `x/sys` 相对稳定；升级时重跑 §10.3 的符号与平台核对 |
| **`EIO`/EOF 平台差异（T2b）** | 跨平台行为不一致，违背 §1.2 | D7 归一化 + Linux CI 用例 |
| **Linux 路径完全未经本地验证** | 本机无容器运行时 | M1/M2 必须跑 Linux CI，不能只信 darwin |
| **`Fd()` 陷阱（已实测确认）** | 一次调用即破坏所有 dup 句柄的 poller，并让阻塞的 `Read` 永久卡死 | D6：只暴露 `SyscallConn()`；文档 + lint 双重防护 |
| **ConPTY 版本门槛（1809）** | 老系统上失败 | 运行时探测 `CreatePseudoConsole` 是否可用，返回清晰错误 |
| **Windows 管道方向写反** | 表现为无输出或立即 EOF，调试成本高 | T5 |
| **与 `go-pty` 重复造轮子** | 生态碎片化 | README 明确说明差异与适用场景；若发现 `go-pty` 更合适应主动说明 |
| **逐行翻译的许可问题** | 法律风险 | 见 §12 |

---

## 12. 许可与署名

上游 `portable-pty` 采用 **MIT**（作者 Wez Furlong，wezterm 项目）。

本项目是「迁移」，若实现中包含对上游源码的**逐行改写**，则构成**衍生作品**，必须：

1. 保留 MIT 许可证全文；
2. 保留原始版权声明（`Copyright (c) ... Wez Furlong`）；
3. 在 README / LICENSE 中明确标注来源与许可。

→ 建议仓库内放 `LICENSE`（MIT，含上游版权行）+ `LICENSE-MIT-portable-pty`（上游原文），并在 README 说明。

---

## 13. 决策记录

| # | 决策 | 结论 | 来源 |
|---|---|---|---|
| 1 | 项目定位 | 独立忠实移植 | 用户决定 |
| 2 | API 风格 | Go 惯用优先（能力对等 + 惯用外形） | 用户决定 |
| 3 | Windows 范围 | 只做 ConPTY（Win10 1809+），无 cgo / 无 DLL | 用户决定 |
| 4 | `serial` | 纳入，排到 M5 | 用户决定 |
| 5 | Unix 平台广度 | linux + darwin + freebsd/openbsd/netbsd | 用户决定 |
| 6 | Go 版本下限 | 先实测再定（见 T1）—— **已由第 13 行给出最终结论** | 用户决定 |
| 7 | 构建缓存 | 沙箱内把 `GOCACHE`/`GOMODCACHE` 指向 `/tmp` | 用户决定 |
| 8 | `CloseWrite()` | 保留，Godoc 写清真实语义 | 用户决定 |
| 9 | `EIO` 处理 | 归一化为 `io.EOF` | 用户决定 |
| 10 | 包名 | `pty`；控制端类型名 `Master` | 本文建议 |
| 11 | 依赖 | 仅 `golang.org/x/sys` | 本文建议 |
| 12 | 上游关系 | 独立实现，不包装 `go-pty`；后者仅作行为参照 | 本文建议 |
| 13 | Go 版本下限 | **`go 1.24`** —— 锁定 `x/sys@v0.41.0`（v0.36–v0.41 均为 `go 1.24.0`；v0.42 起要求 1.25） | 实测（§10.3） |
| 14 | reader 实现路线 | `os.OpenFile` + dup 独立句柄；**不采用** `creack/pty` 的 `syscall.Open`+`os.NewFile` 路线 | 实测（T3/T4/T4b） |
| 15 | Windows ConPTY 序列 | 采用 `go-pty` / `hcsshim` 交叉确认的序列（§3.7） | 参照实现 |
| 16 | CI | 两 job：三平台原生 `test` + 11 目标 `cross-compile`；`GOTOOLCHAIN: local` | 用户决定（§7.4） |
| 17 | 仓库布局 | 采用 §7.3 草案；M0 先落接口与类型，实现留待 M1+ | 本文建议 |
| 18 | `OpenPty` 是否打开 slave | **要打开并持有**，首次 `Spawn` 时交给子进程并关掉父进程副本 | 实测 T6（§3.8） |
| 19 | BSD 平台 | **暂缓**，返回 `ErrUnsupported`；无 BSD CI 前不宣称支持 | 对用户原决定的修正（§7.1） |
| 20 | `WithUmask` | **移除**：Go 无 `pre_exec`，`SysProcAttr` 亦无 `Umask` 字段 | 实测 T8（D3） |
| 21 | `TryWait` 实现 | 后台 goroutine 调一次 `cmd.Wait()`，`TryWait` 非阻塞读取结果 | M1 实现 |
| 22 | 克隆 killer 的语义 | 只发 SIGHUP，**不带**宽限期；与上游 `ProcessSignaller` 一致 | M1 实现 |
| 23 | 信号名格式 | 用 `unix.SignalName`（`SIGHUP`），**刻意偏离**上游的 `strsignal`（受 locale 影响） | M1 实现 |

---

## 附录 A：上游 `portable-pty` 0.9.0 关键 API

供实现时对照。

**`lib.rs`**

```rust
pub struct PtySize { pub rows: u16, pub cols: u16, pub pixel_width: u16, pub pixel_height: u16 }

pub trait MasterPty: Downcast + Send {
    fn resize(&self, size: PtySize) -> Result<(), Error>;
    fn get_size(&self) -> Result<PtySize, Error>;
    fn try_clone_reader(&self) -> Result<Box<dyn std::io::Read + Send>, Error>;
    fn take_writer(&self) -> Result<Box<dyn std::io::Write + Send>, Error>;
    #[cfg(unix)] fn process_group_leader(&self) -> Option<libc::pid_t>;
    #[cfg(unix)] fn as_raw_fd(&self) -> Option<unix::RawFd>;
    #[cfg(unix)] fn tty_name(&self) -> Option<std::path::PathBuf>;
    #[cfg(unix)] fn get_termios(&self) -> Option<nix::sys::termios::Termios> { None }
}

pub trait Child: std::fmt::Debug + ChildKiller + Downcast + Send {
    fn try_wait(&mut self) -> IoResult<Option<ExitStatus>>;
    fn wait(&mut self) -> IoResult<ExitStatus>;
    fn process_id(&self) -> Option<u32>;
}

pub trait ChildKiller: std::fmt::Debug + Downcast + Send {
    fn kill(&mut self) -> IoResult<()>;
    fn clone_killer(&self) -> Box<dyn ChildKiller + Send + Sync>;
}

pub trait SlavePty {
    fn spawn_command(&self, cmd: CommandBuilder) -> Result<Box<dyn Child + Send + Sync>, Error>;
}

pub struct ExitStatus { code: u32, signal: Option<String> }

pub struct PtyPair { pub slave: Box<dyn SlavePty + Send>, pub master: Box<dyn MasterPty + Send> }

pub trait PtySystem: Downcast {
    fn openpty(&self, size: PtySize) -> anyhow::Result<PtyPair>;
}

pub fn native_pty_system() -> Box<dyn PtySystem + Send>;
```

**`cmdbuilder.rs` 能力清单**：`new`、`from_argv`、`new_default_prog`、`is_default_prog`、`arg`、`args`、`get_argv(_mut)`、`env`、`env_remove`、`env_clear`、`get_env`、`cwd`、`clear_cwd`、`get_cwd`、`iter_extra_env_as_str`、`iter_full_env_as_str`、`as_unix_command_line`、`set_controlling_tty`/`get_controlling_tty`、`umask`（unix）、`get_shell`。

行为要点：
- base env 在 `new()` 时**快照**；Unix 上补 `SHELL`（`$SHELL` 不可执行则回退 `getpwuid`，再回退 `/bin/sh`）。
- Windows 上合并 HKLM + HKCU 注册表环境，展开 `REG_EXPAND_SZ`，并**把用户 PATH 追加到系统 PATH 之后**；环境键大小写不敏感（内部 lower-case 规范化，保留 `preferred_key` 原始大小写）。
- Unix `as_command()`：手动按 `PATH` 用 `access(X_OK)` 解析可执行文件，设 `arg0`，`current_dir` 在 `cwd` 非目录时回退 `HOME`，`env_clear()` 后设 `SHELL` 再逐项设置。
- Windows `cmdline()`：用 MSVCRT 引号规则构造宽字符命令行（`append_quoted`），环境块为双 NUL 结尾的 UTF-16。
- `arg()` 在 `new_default_prog()` 构造的 builder 上会 **panic**。
