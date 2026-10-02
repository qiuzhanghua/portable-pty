//go:build windows

package pty

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Shell returns the command processor this package would run for the user:
// %ComSpec%, or cmd.exe when that is unset. This is CommandBuilder::get_shell.
func Shell() string {
	if comspec := os.Getenv("ComSpec"); comspec != "" {
		return comspec
	}
	return "cmd.exe"
}

// shellArgv0 is a no-op on Windows. The "-" prefix that turns a Unix shell into
// a login shell has no equivalent here, and portable-pty does not attempt one.
func shellArgv0(shell string) string { return shell }

// envKeyEqual compares environment variable names. Windows treats them as
// case-insensitive.
func envKeyEqual(a, b string) bool { return strings.EqualFold(a, b) }

// registryEnvLocations are the two places portable-pty merges from, machine
// first and then user.
//
// skipUsername mirrors upstream, which drops a USERNAME value only in the
// machine pass. The user pass keeps everything.
var registryEnvLocations = []struct {
	root         registry.Key
	path         string
	skipUsername bool
}{
	{registry.LOCAL_MACHINE, `System\CurrentControlSet\Control\Session Manager\Environment`, true},
	{registry.CURRENT_USER, `Environment`, false},
}

// baseEnviron merges the process environment with the machine and user
// environments held in the registry, matching CommandBuilder's get_base_env.
//
// The process environment is the base. Registry entries overwrite it, and Path
// is special-cased so that the later value is appended rather than replacing.
func baseEnviron() []string {
	env := os.Environ()
	for _, loc := range registryEnvLocations {
		env = mergeRegistryEnv(env, loc.root, loc.path, loc.skipUsername)
	}
	return env
}

func mergeRegistryEnv(env []string, root registry.Key, path string, skipUsername bool) []string {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		// A missing or unreadable key is not fatal: portable-pty logs and
		// carries on, and the process environment is still usable.
		return env
	}
	defer key.Close()

	names, err := key.ReadValueNames(0)
	if err != nil {
		return env
	}

	entries := make([]envEntry, 0, len(names))
	for _, name := range names {
		// portable-pty skips this one because the process environment already
		// carries the right value.
		if skipUsername && strings.EqualFold(name, "username") {
			continue
		}

		value, valtype, err := key.GetStringValue(name)
		if err != nil {
			// Not a string value (a DWORD, say). There is nothing to forward.
			continue
		}
		if valtype == registry.EXPAND_SZ {
			// GetStringValue hands back EXPAND_SZ unexpanded.
			if expanded, err := registry.ExpandString(value); err == nil {
				value = expanded
			}
		}
		entries = append(entries, envEntry{name: name, value: value})
	}
	return mergeEnvEntries(env, entries)
}
