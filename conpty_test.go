package pty

import (
	"errors"
	"os"
	"slices"
	"testing"
)

// The ConPTY choreography is tested here with a fake host, on every platform,
// because the rules it encodes — which pipe end goes where, when the attribute
// list may be freed, which handles are the caller's — are exactly the ones that
// stay invisible until they reach a real Windows machine.

type fakeConPTY struct {
	events []string

	pipes   [][2]*os.File
	pipeFDs [][2]uintptr

	cols, rows             uint16
	consoleIn, consoleOut  uintptr
	attachedConsole        conpty
	spec                   *conptyProcessSpec
	attrsDeleted           bool
	attrsDeletedBeforeProc bool
	consoleClosed          bool
	threadReleased         bool

	failCreatePseudoConsole error
	failSetPseudoConsole    error
	failCreateProcess       error
	failResize              error
}

func (f *fakeConPTY) makePipe() (r, w *os.File, err error) {
	r, w, err = os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	f.events = append(f.events, "makePipe")
	f.pipes = append(f.pipes, [2]*os.File{r, w})
	// Capture the descriptors now: the choreography closes some of these.
	f.pipeFDs = append(f.pipeFDs, [2]uintptr{r.Fd(), w.Fd()})
	return r, w, nil
}

func (f *fakeConPTY) createPseudoConsole(cols, rows uint16, in, out uintptr) (conpty, error) {
	f.events = append(f.events, "createPseudoConsole")
	f.cols, f.rows, f.consoleIn, f.consoleOut = cols, rows, in, out
	if f.failCreatePseudoConsole != nil {
		return nil, f.failCreatePseudoConsole
	}
	return &fakeConsole{host: f}, nil
}

func (f *fakeConPTY) newAttributeList(n int) (conptyAttributes, error) {
	f.events = append(f.events, "newAttributeList")
	return &fakeAttrs{host: f, capacity: n}, nil
}

func (f *fakeConPTY) createProcess(spec *conptyProcessSpec, _ conptyAttributes) (conptyProcess, error) {
	if f.attrsDeleted {
		f.attrsDeletedBeforeProc = true
	}
	f.events = append(f.events, "createProcess")
	f.spec = spec
	if f.failCreateProcess != nil {
		return nil, f.failCreateProcess
	}
	return &fakeProcess{host: f, pidValue: 4242}, nil
}

func (f *fakeConPTY) newChild(proc conptyProcess) Child {
	f.events = append(f.events, "newChild")
	return fakeChild{proc: proc}
}

type fakeConsole struct{ host *fakeConPTY }

func (c *fakeConsole) resize(cols, rows uint16) error {
	c.host.events = append(c.host.events, "resize")
	if c.host.failResize != nil {
		return c.host.failResize
	}
	c.host.cols, c.host.rows = cols, rows
	return nil
}

func (c *fakeConsole) close() {
	c.host.events = append(c.host.events, "console.close")
	c.host.consoleClosed = true
}

type fakeAttrs struct {
	host     *fakeConPTY
	capacity int
}

func (a *fakeAttrs) setPseudoConsole(pc conpty) error {
	a.host.events = append(a.host.events, "setPseudoConsole")
	a.host.attachedConsole = pc
	return a.host.failSetPseudoConsole
}

func (a *fakeAttrs) delete() {
	a.host.events = append(a.host.events, "attrs.delete")
	a.host.attrsDeleted = true
}

type fakeProcess struct {
	host     *fakeConPTY
	pidValue int
}

func (p *fakeProcess) pid() int { return p.pidValue }

func (p *fakeProcess) releaseThreadHandle() {
	p.host.events = append(p.host.events, "releaseThreadHandle")
	p.host.threadReleased = true
}

type fakeChild struct{ proc conptyProcess }

func (fakeChild) Wait() (ExitStatus, error)          { return ExitStatus{}, nil }
func (fakeChild) TryWait() (ExitStatus, bool, error) { return ExitStatus{}, true, nil }
func (fakeChild) PID() int                           { return 0 }
func (fakeChild) Kill() error                        { return nil }
func (fakeChild) CloneKiller() Killer                { return fakeKiller{} }

type fakeKiller struct{}

func (fakeKiller) Kill() error         { return nil }
func (fakeKiller) CloneKiller() Killer { return fakeKiller{} }

func closed(f *os.File) bool {
	_, err := f.Write(nil)
	return errors.Is(err, os.ErrClosed)
}

// The console must be given its own ends of both pipes, and our ends must stay
// open; mixing these up produces a console that never sees input.
func TestNewConPTYWiresTheConsoleEnds(t *testing.T) {
	host := &fakeConPTY{}
	console, err := newConPTY(host, Size{Cols: 120, Rows: 40})
	if err != nil {
		t.Fatalf("newConPTY: %v", err)
	}
	defer console.close()

	if want := []string{"makePipe", "makePipe", "createPseudoConsole"}; !slices.Equal(host.events, want) {
		t.Errorf("events = %v, want %v", host.events, want)
	}
	if host.cols != 120 || host.rows != 40 {
		t.Errorf("console size = %dx%d, want 120x40", host.cols, host.rows)
	}

	// Pipe 0 is input (console reads end 0), pipe 1 is output (console writes
	// end 1). Anything else is a bug.
	if host.consoleIn != host.pipeFDs[0][0] {
		t.Errorf("console input = %d, want the read end of pipe 0 (%d)", host.consoleIn, host.pipeFDs[0][0])
	}
	if host.consoleOut != host.pipeFDs[1][1] {
		t.Errorf("console output = %d, want the write end of pipe 1 (%d)", host.consoleOut, host.pipeFDs[1][1])
	}

	// The console duplicated its ends into conhost; ours must be gone.
	if !closed(host.pipes[0][0]) {
		t.Error("the console's input end is still open on our side")
	}
	if !closed(host.pipes[1][1]) {
		t.Error("the console's output end is still open on our side")
	}

	// Our ends must survive, or the session is unusable.
	if closed(host.pipes[0][1]) {
		t.Error("our write end was closed")
	}
	if closed(host.pipes[1][0]) {
		t.Error("our read end was closed")
	}
}

func TestNewConPTYCleansUpWhenConsoleFails(t *testing.T) {
	host := &fakeConPTY{failCreatePseudoConsole: errors.New("no conpty here")}

	if _, err := newConPTY(host, DefaultSize); err == nil {
		t.Fatal("newConPTY succeeded, want an error")
	}
	for i, pair := range host.pipes {
		for j, f := range pair {
			if !closed(f) {
				t.Errorf("pipe %d end %d leaked after a failed createPseudoConsole", i, j)
			}
		}
	}
}

// The attribute list must be populated before CreateProcess and freed only
// afterwards: CreateProcess reads it.
func TestStartProcessOrdering(t *testing.T) {
	host := &fakeConPTY{}
	console, err := newConPTY(host, DefaultSize)
	if err != nil {
		t.Fatalf("newConPTY: %v", err)
	}
	defer console.close()

	host.events = nil
	if _, err := console.startProcess(&conptyProcessSpec{applicationName: "cmd.exe"}); err != nil {
		t.Fatalf("startProcess: %v", err)
	}

	want := []string{"newAttributeList", "setPseudoConsole", "createProcess", "attrs.delete", "releaseThreadHandle", "newChild"}
	if !slices.Equal(host.events, want) {
		t.Errorf("events = %v, want %v", host.events, want)
	}
	if host.attachedConsole != console.console {
		t.Error("the attribute list was given a different console")
	}
	if !host.threadReleased {
		t.Error("the thread handle was never released")
	}
}

func TestStartProcessFreesAttributesWhenCreateProcessFails(t *testing.T) {
	host := &fakeConPTY{failCreateProcess: errors.New("boom")}
	console, err := newConPTY(host, DefaultSize)
	if err != nil {
		t.Fatalf("newConPTY: %v", err)
	}
	defer console.close()

	if _, err := console.startProcess(&conptyProcessSpec{applicationName: "cmd.exe"}); err == nil {
		t.Fatal("startProcess succeeded, want an error")
	}
	if !host.attrsDeleted {
		t.Error("the attribute list leaked when CreateProcess failed")
	}
	if host.threadReleased {
		t.Error("the thread handle was released for a process that never started")
	}
}

func TestStartProcessFreesAttributesWhenAttachFails(t *testing.T) {
	host := &fakeConPTY{failSetPseudoConsole: errors.New("cannot attach")}
	console, err := newConPTY(host, DefaultSize)
	if err != nil {
		t.Fatalf("newConPTY: %v", err)
	}
	defer console.close()

	if _, err := console.startProcess(&conptyProcessSpec{applicationName: "cmd.exe"}); err == nil {
		t.Fatal("startProcess succeeded, want an error")
	}
	if !host.attrsDeleted {
		t.Error("the attribute list leaked when attaching the console failed")
	}
	if slices.Contains(host.events, "createProcess") {
		t.Error("CreateProcess ran despite the console never being attached")
	}
}

func TestStartProcessPassesTheSpecThrough(t *testing.T) {
	host := &fakeConPTY{}
	console, err := newConPTY(host, DefaultSize)
	if err != nil {
		t.Fatalf("newConPTY: %v", err)
	}
	defer console.close()

	spec := &conptyProcessSpec{
		applicationName: `C:\Windows\system32\cmd.exe`,
		args:            []string{"cmd.exe", "/c", "echo hi"},
		dir:             `C:\Users`,
		env:             []string{"A=1"},
	}
	if _, err := console.startProcess(spec); err != nil {
		t.Fatalf("startProcess: %v", err)
	}
	if host.spec != spec {
		t.Error("startProcess did not pass the spec through unchanged")
	}
}

func TestConPTYResizeAndClose(t *testing.T) {
	host := &fakeConPTY{}
	console, err := newConPTY(host, Size{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("newConPTY: %v", err)
	}

	if err := console.resize(Size{Cols: 100, Rows: 50}); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if host.cols != 100 || host.rows != 50 {
		t.Errorf("after resize the console is %dx%d, want 100x50", host.cols, host.rows)
	}

	if err := console.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !host.consoleClosed {
		t.Error("close did not release the pseudoconsole")
	}
}

// On Windows CloseWrite is not merely logical: it closes our end of the
// console's input pipe, so the child really does see end-of-file, while reads
// keep working.
func TestConPTYCloseWriteSendsRealEOF(t *testing.T) {
	host := &fakeConPTY{}
	console, err := newConPTY(host, DefaultSize)
	if err != nil {
		t.Fatalf("newConPTY: %v", err)
	}
	defer console.close()

	if err := console.closeWrite(); err != nil {
		t.Fatalf("closeWrite: %v", err)
	}
	if !closed(console.in) {
		t.Error("CloseWrite left the console's input pipe open, so no EOF was sent")
	}
	if closed(console.out) {
		t.Error("CloseWrite closed the read direction as well")
	}
	if host.consoleClosed {
		t.Error("CloseWrite released the pseudoconsole; only Close should")
	}

	// Close afterwards must not report the already-closed pipe as an error.
	if err := console.close(); err != nil {
		t.Errorf("close after closeWrite = %v, want nil", err)
	}
}

func TestConPTYResizeErrorIsWrapped(t *testing.T) {
	host := &fakeConPTY{failResize: errors.New("no")}
	console, err := newConPTY(host, DefaultSize)
	if err != nil {
		t.Fatalf("newConPTY: %v", err)
	}
	defer console.close()

	if err := console.resize(Size{Cols: 1, Rows: 1}); err == nil {
		t.Error("resize succeeded, want the failure reported")
	}
}

// Closing twice must be harmless and must release the pseudoconsole exactly
// once. ClosePseudoConsole on an already-closed handle is undefined behaviour,
// and double-closing is easy to reach: a caller that closes explicitly and also
// defers a Close does it every time.
func TestConPTYCloseIsIdempotent(t *testing.T) {
	host := &fakeConPTY{}
	console, err := newConPTY(host, DefaultSize)
	if err != nil {
		t.Fatalf("newConPTY: %v", err)
	}

	if err := console.close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := console.close(); err != nil {
		t.Errorf("second close = %v, want nil", err)
	}

	released := 0
	for _, e := range host.events {
		if e == "console.close" {
			released++
		}
	}
	if released != 1 {
		t.Errorf("the pseudoconsole was released %d times, want exactly 1", released)
	}
}
