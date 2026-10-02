//go:build linux || darwin || freebsd || openbsd || netbsd

package pty

// Native returns a System backed by the host's POSIX PTY implementation.
func Native() System { return nativeSystem{} }

// nativeSystem implements System using the platform's pseudo-terminal devices.
type nativeSystem struct{}

// OpenPty creates a new PTY with the requested window size.
//
// Pass DefaultSize if you have no better information: a zero Size really does
// request a 0x0 window, which upsets most programs that query it.
//
// The slave is opened here and held until the first Spawn. That is not an
// implementation detail: on Darwin the kernel answers TIOCSWINSZ with ENOTTY on
// a master whose slave has never been opened, so the window size could not be
// set at all otherwise. It also lets callers Resize or Size before spawning.
// Where the kernel hands both ends over at once, as OpenBSD does, that slave is
// used rather than reopening it by name.
func (nativeSystem) OpenPty(size Size) (Master, error) {
	opened, err := openMaster()
	if err != nil {
		return nil, err
	}

	m := &unixMaster{file: opened.master, name: opened.slaveName}

	slave := opened.slave
	if slave == nil {
		if slave, err = m.openSlave(); err != nil {
			opened.master.Close()
			return nil, err
		}
	}
	m.slave = slave

	if err := m.Resize(size); err != nil {
		m.Close()
		return nil, err
	}
	return m, nil
}
