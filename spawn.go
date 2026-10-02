package pty

// SpawnOption configures a single call to Master.Spawn.
type SpawnOption func(*spawnConfig)

// spawnConfig holds the resolved options for one spawn attempt.
type spawnConfig struct {
	controllingTTY bool
}

// defaultSpawnConfig returns the options applied when no SpawnOption is given.
func defaultSpawnConfig() spawnConfig {
	return spawnConfig{controllingTTY: true}
}

// WithControllingTTY sets whether the PTY should become the child's controlling
// terminal. The default is true, which is what you normally want. Setting it to
// false is occasionally needed when crossing container boundaries (for example
// flatpak), where acquiring a controlling terminal fails.
//
// The controlling terminal is only acquired when this package assigns the slave
// to at least one of the child's standard streams. If the caller supplies all
// three, there is no slave to attach and this option has no effect.
func WithControllingTTY(enabled bool) SpawnOption {
	return func(c *spawnConfig) { c.controllingTTY = enabled }
}

// applySpawnOptions resolves opts over the defaults.
func applySpawnOptions(opts []SpawnOption) spawnConfig {
	cfg := defaultSpawnConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}
