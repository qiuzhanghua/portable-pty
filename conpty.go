package pty

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// This file holds the platform-neutral half of Windows ConPTY: the handle
// choreography. Everything here is written against small interfaces so that the
// ordering and ownership rules — the parts of ConPTY that are easy to get wrong
// and invisible until they reach a real Windows box — can be tested with a fake
// on any platform. What remains Windows-only is the set of kernel32 calls.

// conpty is a pseudoconsole handle.
type conpty interface {
	// resize changes the size of the console's buffers.
	resize(cols, rows uint16) error
	// close releases the pseudoconsole.
	close()
}

// conptyAttributes is a PROC_THREAD_ATTRIBUTE_LIST prepared for CreateProcess.
type conptyAttributes interface {
	// setPseudoConsole records the pseudoconsole the new process attaches to.
	setPseudoConsole(pc conpty) error
	// delete releases the list.
	//
	// It must not be called before CreateProcess returns: CreateProcess reads
	// the list, and freeing it early is a use-after-free.
	delete()
}

// conptyProcess is a process that has been started.
type conptyProcess interface {
	// pid is the new process's identifier.
	pid() int
	// releaseThreadHandle closes the thread handle, which nothing else needs.
	releaseThreadHandle()
}

// conptyProcessSpec describes the process to start. It deliberately carries
// plain values rather than an *exec.Cmd, because the fields that matter
// (SysProcAttr.CmdLine) only exist on Windows and this type does not.
type conptyProcessSpec struct {
	applicationName string
	args            []string // ignored when commandLine is set
	commandLine     string   // verbatim override, from SysProcAttr.CmdLine
	dir             string
	env             []string
}

// conptyHost is the Windows-specific half of ConPTY. The real implementation is
// a thin wrapper over golang.org/x/sys/windows; the fake used by the tests
// records what was called and in what order.
type conptyHost interface {
	// makePipe returns a connected pair: index 0 reads, index 1 writes.
	makePipe() (r, w *os.File, err error)
	// createPseudoConsole creates a console that reads its input from in and
	// writes its output to out. It takes ownership of neither.
	createPseudoConsole(cols, rows uint16, in, out uintptr) (conpty, error)
	// newAttributeList allocates a list with room for n attributes.
	newAttributeList(n int) (conptyAttributes, error)
	// createProcess starts spec, attaching it to the console recorded in attrs.
	createProcess(spec *conptyProcessSpec, attrs conptyAttributes) (conptyProcess, error)
	// newChild wraps a started process as a Child.
	newChild(proc conptyProcess) Child
}

// conptyConsole is a pseudoconsole together with the two pipe ends this side
// keeps: writes go to the child's input, reads come from its output.
type conptyConsole struct {
	host    conptyHost
	console conpty
	in      *os.File // write end: our input to the console
	out     *os.File // read end: the console's output

	// closeOnce makes Close idempotent, which matters more than it looks.
	// ClosePseudoConsole on a handle that has already been closed is undefined
	// behaviour and in practice takes the process down, and double-closing is
	// easy to reach: a caller that closes explicitly and also defers a Close
	// will do it every time.
	closeOnce sync.Once
	closeErr  error
}

// newConPTY creates a pseudoconsole of the given size plus its two pipes.
//
// ConPTY cannot report its size back, so the size is remembered from here on.
func newConPTY(host conptyHost, size Size) (*conptyConsole, error) {
	// The console reads its input from consoleIn, so that is the read end of the
	// first pipe; we keep the write end.
	consoleIn, ourIn, err := host.makePipe()
	if err != nil {
		return nil, fmt.Errorf("pty: create input pipe: %w", err)
	}
	// The console writes its output to consoleOut, so that is the *write* end of
	// the second pipe; we keep the read end. Getting this pair the wrong way
	// round yields a console that never produces output, so the order of these
	// two variables matters.
	ourOut, consoleOut, err := host.makePipe()
	if err != nil {
		consoleIn.Close()
		ourIn.Close()
		return nil, fmt.Errorf("pty: create output pipe: %w", err)
	}

	console, err := host.createPseudoConsole(size.Cols, size.Rows, consoleIn.Fd(), consoleOut.Fd())

	// Either way the console ends are spent: on success the console holds its
	// own references after duplicating them into conhost, and on failure
	// nothing will ever read them. Keeping them open would leak a handle and
	// stop EOF from propagating back to us.
	consoleIn.Close()
	consoleOut.Close()

	if err != nil {
		ourIn.Close()
		ourOut.Close()
		return nil, fmt.Errorf("pty: create pseudo console: %w", err)
	}

	return &conptyConsole{host: host, console: console, in: ourIn, out: ourOut}, nil
}

// startProcess attaches a new process to the console.
//
// The ordering is load-bearing, and this is the reason the type is testable
// without Windows:
//
//  1. the attribute list is populated with the pseudoconsole;
//  2. CreateProcess runs, and only then is the list deleted — it must outlive
//     the call;
//  3. the thread handle is closed, because nothing else wants it.
func (c *conptyConsole) startProcess(spec *conptyProcessSpec) (Child, error) {
	attrs, err := c.host.newAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("pty: allocate attribute list: %w", err)
	}
	if err := attrs.setPseudoConsole(c.console); err != nil {
		attrs.delete()
		return nil, fmt.Errorf("pty: attach pseudo console: %w", err)
	}

	proc, err := c.host.createProcess(spec, attrs)

	// CreateProcess has read the list by now. This is the earliest point at
	// which it is safe to free.
	attrs.delete()

	if err != nil {
		return nil, fmt.Errorf("pty: create process: %w", err)
	}
	proc.releaseThreadHandle()

	return c.host.newChild(proc), nil
}

func (c *conptyConsole) resize(size Size) error {
	if err := c.console.resize(size.Cols, size.Rows); err != nil {
		return fmt.Errorf("pty: resize pseudo console: %w", err)
	}
	return nil
}

// closeWrite shuts the write direction only.
//
// Unlike Unix, this is not merely logical: the console reads the child's input
// from the other end of this pipe, so closing our write end gives the child a
// real end-of-file while reads keep working.
func (c *conptyConsole) closeWrite() error {
	return c.in.Close()
}

func (c *conptyConsole) close() error {
	c.closeOnce.Do(func() {
		c.console.close()
		c.closeErr = errors.Join(closeIfOpen(c.in), closeIfOpen(c.out))
	})
	return c.closeErr
}

// closeIfOpen tolerates a file that an earlier CloseWrite already closed, which
// is not an error at this level.
func closeIfOpen(f *os.File) error {
	err := f.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}
