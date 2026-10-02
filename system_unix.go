//go:build linux || darwin

package pty

// Native returns a System backed by the host's POSIX PTY implementation.
func Native() System { return nativeSystem{} }

// nativeSystem implements System using /dev/ptmx and friends.
type nativeSystem struct{}

// OpenPty creates a new PTY with the requested window size.
//
// Pass DefaultSize if you have no better information: a zero Size really does
// request a 0x0 window, which upsets most programs that query it.
//
// The slave is opened here and held until the first Spawn. That is not an
// implementation detail: on Darwin the kernel answers TIOCSWINSZ with ENOTTY
// on a master whose slave has never been opened, so the window size could not
// be set at all otherwise. It also lets callers Resize or Size before spawning.
func (nativeSystem) OpenPty(size Size) (Master, error) {
	file, slaveName, err := openMaster()
	if err != nil {
		return nil, err
	}

	m := &unixMaster{file: file, name: slaveName}

	slave, err := m.openSlave()
	if err != nil {
		file.Close()
		return nil, err
	}
	m.slave = slave

	if err := m.Resize(size); err != nil {
		m.Close()
		return nil, err
	}
	return m, nil
}
