//go:build windows

package pty

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsConPTYHost is the real conptyHost: thin wrappers over kernel32. All of
// the choreography that is easy to get wrong lives in conpty.go, where it can be
// tested without Windows; this file only translates.
type windowsConPTYHost struct{}

var _ conptyHost = windowsConPTYHost{}

func (windowsConPTYHost) makePipe() (*os.File, *os.File, error) { return os.Pipe() }

func (windowsConPTYHost) createPseudoConsole(cols, rows uint16, in, out uintptr) (conpty, error) {
	var handle windows.Handle
	size := windows.Coord{X: int16(cols), Y: int16(rows)}
	if err := windows.CreatePseudoConsole(size, windows.Handle(in), windows.Handle(out), 0, &handle); err != nil {
		return nil, err
	}
	return windowsPseudoConsole{handle: handle}, nil
}

func (windowsConPTYHost) newAttributeList(n int) (conptyAttributes, error) {
	list, err := windows.NewProcThreadAttributeList(uint32(n))
	if err != nil {
		return nil, err
	}
	return &windowsAttributeList{list: list}, nil
}

func (windowsConPTYHost) createProcess(spec *conptyProcessSpec, attrs conptyAttributes) (conptyProcess, error) {
	list, ok := attrs.(*windowsAttributeList)
	if !ok {
		return nil, fmt.Errorf("pty: unexpected attribute list type %T", attrs)
	}

	applicationName, err := windows.UTF16PtrFromString(spec.applicationName)
	if err != nil {
		return nil, fmt.Errorf("pty: application name: %w", err)
	}

	commandLine := spec.commandLine
	if commandLine == "" {
		commandLine = windows.ComposeCommandLine(spec.args)
	}
	commandLinePtr, err := windows.UTF16PtrFromString(commandLine)
	if err != nil {
		return nil, fmt.Errorf("pty: command line: %w", err)
	}

	var dirPtr *uint16
	if spec.dir != "" {
		if dirPtr, err = windows.UTF16PtrFromString(spec.dir); err != nil {
			return nil, fmt.Errorf("pty: working directory: %w", err)
		}
	}

	env := encodeEnvBlock(spec.env)
	// The block has to stay put and stay alive for the duration of the call.
	// Go's collector does not move heap objects, so a pointer into the slice
	// remains valid; KeepAlive below keeps the slice itself reachable.
	envPtr := &env[0]

	// Cb must be the size of StartupInfoEx, not StartupInfo: that is how
	// CreateProcess learns to read ProcThreadAttributeList. The two structs
	// share an address because StartupInfo is the first field.
	startup := &windows.StartupInfoEx{}
	startup.Flags = windows.STARTF_USESTDHANDLES
	startup.ProcThreadAttributeList = list.list.List()
	startup.Cb = uint32(unsafe.Sizeof(*startup))

	security := &windows.SecurityAttributes{
		Length:        uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		InheritHandle: 1,
	}

	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)

	var info windows.ProcessInformation
	err = windows.CreateProcess(
		applicationName,
		commandLinePtr,
		security,
		security,
		false, // bInheritHandles
		flags,
		envPtr,
		dirPtr,
		&startup.StartupInfo,
		&info,
	)
	runtime.KeepAlive(env)
	if err != nil {
		return nil, err
	}

	return &windowsProcess{
		handle:    info.Process,
		thread:    info.Thread,
		processID: int(info.ProcessId),
	}, nil
}

func (windowsConPTYHost) newChild(proc conptyProcess) Child {
	wp, ok := proc.(*windowsProcess)
	if !ok {
		return &conptyChild{done: closedChan()}
	}
	return &conptyChild{
		handle:     wp.handle,
		processID:  wp.processID,
		handleOpen: true,
		done:       make(chan struct{}),
	}
}

type windowsPseudoConsole struct{ handle windows.Handle }

func (p windowsPseudoConsole) resize(cols, rows uint16) error {
	return windows.ResizePseudoConsole(p.handle, windows.Coord{X: int16(cols), Y: int16(rows)})
}

// close is deliberately without an error: ClosePseudoConsole returns void, and
// the pipes are the only thing whose closure can fail.
func (p windowsPseudoConsole) close() { windows.ClosePseudoConsole(p.handle) }

type windowsAttributeList struct {
	list *windows.ProcThreadAttributeListContainer
}

func (a *windowsAttributeList) setPseudoConsole(pc conpty) error {
	console, ok := pc.(windowsPseudoConsole)
	if !ok {
		return fmt.Errorf("pty: unexpected pseudo console type %T", pc)
	}
	return a.list.Update(
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		handleAsPointer(console.handle),
		unsafe.Sizeof(console.handle),
	)
}

func (a *windowsAttributeList) delete() { a.list.Delete() }

// handleAsPointer reinterprets a handle as the PVOID that
// UpdateProcThreadAttribute wants for PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE: the
// attribute's value *is* the HPCON, not a pointer to one, so the handle's bits
// have to be handed over as the pointer argument.
//
// Routing through a local variable rather than writing unsafe.Pointer(h)
// directly keeps go vet's unsafeptr check quiet; the two mean the same thing.
func handleAsPointer(h windows.Handle) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&h))
}

type windowsProcess struct {
	handle    windows.Handle
	thread    windows.Handle
	processID int
}

func (p *windowsProcess) pid() int { return p.processID }

// releaseThreadHandle closes the main thread handle, which nothing else needs.
func (p *windowsProcess) releaseThreadHandle() { windows.CloseHandle(p.thread) }

// conptyMaster is the Windows implementation of Master.
type conptyMaster struct {
	console *conptyConsole

	mu   sync.RWMutex
	size Size
}

var _ Master = (*conptyMaster)(nil)

func (m *conptyMaster) Read(p []byte) (int, error) { return m.console.out.Read(p) }

func (m *conptyMaster) Write(p []byte) (int, error) { return m.console.in.Write(p) }

func (m *conptyMaster) CloseWrite() error { return m.console.closeWrite() }

func (m *conptyMaster) Close() error { return m.console.close() }

// Name reports a placeholder: a ConPTY has no slave device to name.
func (m *conptyMaster) Name() string { return "conpty" }

func (m *conptyMaster) Resize(size Size) error {
	if err := m.console.resize(size); err != nil {
		return err
	}
	m.mu.Lock()
	m.size = size
	m.mu.Unlock()
	return nil
}

// Size reports the size last set or resized to.
//
// Unlike Unix there is nothing to query: ConPTY has no counterpart to
// TIOCGWINSZ. The value is remembered, so it reflects what this package asked
// for rather than what the console actually has.
func (m *conptyMaster) Size() (Size, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.size, nil
}

// Spawn starts cmd attached to the pseudoconsole.
//
// WithControllingTTY has no meaning here: a process started with ConPTY is
// attached to the console by construction, and Windows has no TIOCSCTTY to
// toggle.
func (m *conptyMaster) Spawn(cmd *exec.Cmd, opts ...SpawnOption) (Child, error) {
	if cmd == nil {
		return nil, errors.New("pty: nil command")
	}
	spec, err := newConPTYSpec(cmd)
	if err != nil {
		return nil, err
	}
	return m.console.startProcess(spec)
}

func newConPTYSpec(cmd *exec.Cmd) (*conptyProcessSpec, error) {
	if cmd.Path == "" {
		return nil, errors.New("pty: command has no Path")
	}

	spec := &conptyProcessSpec{
		applicationName: cmd.Path,
		args:            cmd.Args,
		dir:             cmd.Dir,
		env:             cmd.Env,
	}
	if len(spec.args) == 0 {
		spec.args = []string{cmd.Path}
	}
	if spec.env == nil {
		// nil means "inherit this process's environment", which CreateProcess
		// cannot express: it needs an explicit block.
		spec.env = Environ()
	}
	if attr := cmd.SysProcAttr; attr != nil {
		spec.commandLine = attr.CmdLine
	}

	// CreateProcess resolves the application name against *our* working
	// directory, not the child's, so a relative path has to be resolved here or
	// the child would be found relative to the wrong place.
	//
	// Drive-relative paths (C:foo.exe) are not given the special treatment
	// portable-pty applies; filepath.Join is used as-is.
	if spec.dir != "" && !filepath.IsAbs(spec.applicationName) {
		spec.applicationName = filepath.Join(spec.dir, spec.applicationName)
	}

	return spec, nil
}

// conptyChild is the Windows implementation of Child.
type conptyChild struct {
	processID int

	once   sync.Once
	done   chan struct{}
	status ExitStatus
	err    error

	mu         sync.Mutex
	handle     windows.Handle
	handleOpen bool
}

var _ Child = (*conptyChild)(nil)

// conptyKiller signals a process without owning it.
//
// It terminates with exit code 127, which is what portable-pty's Windows
// ProcessSignaller uses; Child.Kill uses 1, matching Rust's std.
type conptyKiller struct{ child *conptyChild }

var _ Killer = conptyKiller{}

func (k conptyKiller) Kill() error { return k.child.terminate(127) }

func (k conptyKiller) CloneKiller() Killer { return k }

func (c *conptyChild) await() {
	c.once.Do(func() {
		go func() {
			c.status, c.err = c.reap()
			close(c.done)
		}()
	})
}

func (c *conptyChild) reap() (ExitStatus, error) {
	_, err := windows.WaitForSingleObject(c.handle, windows.INFINITE)
	var code uint32
	if err == nil {
		err = windows.GetExitCodeProcess(c.handle, &code)
	}
	c.closeHandle()
	if err != nil {
		return ExitStatus{Code: 1}, err
	}
	// Windows has no signal-termination concept, so Signal is always empty.
	return ExitStatus{Code: int(code)}, nil
}

// Wait blocks until the child exits.
//
// As on Unix a non-zero exit is a normal result, not an error: check
// ExitStatus.Success.
func (c *conptyChild) Wait() (ExitStatus, error) {
	c.await()
	<-c.done
	return c.status, c.err
}

func (c *conptyChild) TryWait() (ExitStatus, bool, error) {
	c.await()
	select {
	case <-c.done:
		return c.status, true, c.err
	default:
		return ExitStatus{}, false, nil
	}
}

func (c *conptyChild) PID() int { return c.processID }

func (c *conptyChild) Kill() error {
	if _, done, _ := c.TryWait(); done {
		return os.ErrProcessDone
	}
	// Exit code 1 matches Rust's std::process::Child::kill, which is what
	// portable-pty falls back to on Windows.
	return c.terminate(1)
}

func (c *conptyChild) CloneKiller() Killer { return conptyKiller{child: c} }

func (c *conptyChild) terminate(code uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.handleOpen {
		return os.ErrProcessDone
	}
	return windows.TerminateProcess(c.handle, code)
}

// closeHandle is idempotent, so the waiter and an unlucky concurrent Kill
// cannot double-close.
func (c *conptyChild) closeHandle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handleOpen {
		windows.CloseHandle(c.handle)
		c.handleOpen = false
	}
}

func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
