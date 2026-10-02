package pty

import "strings"

// envEntry is one environment variable read from a source other than the
// process environment.
type envEntry struct {
	name  string
	value string
}

// mergeEnvEntries applies the environment merge policy used for the Windows
// registry, in order.
//
// This lives in a platform-neutral file, and uses its own case-folding lookups
// rather than the exported Env* helpers, for one reason: the policy is the part
// worth testing, and both choices make it behave identically everywhere, so it
// can be exercised without a Windows machine. What remains Windows-only is the
// registry read itself.
//
// The rules, matching CommandBuilder::get_base_env:
//
//   - a later entry replaces an earlier one with the same name, so user values
//     win over machine values;
//   - Path is appended to rather than replaced, because the machine and user
//     Paths are both meaningful;
//   - names compare case-insensitively, as Windows does.
func mergeEnvEntries(env []string, entries []envEntry) []string {
	for _, e := range entries {
		if strings.EqualFold(e.name, "Path") {
			if existing, ok := envLookupFold(env, "Path"); ok && existing != "" {
				env = envSetFold(env, e.name, existing+";"+e.value)
				continue
			}
		}
		env = envSetFold(env, e.name, e.value)
	}
	return env
}

// envLookupFold looks a name up case-insensitively.
func envLookupFold(env []string, key string) (string, bool) {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}

// envSetFold sets a name case-insensitively, preserving the caller's spelling
// of the name.
func envSetFold(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, key) {
			if !replaced {
				out = append(out, key+"="+value)
				replaced = true
			}
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, key+"="+value)
	}
	return out
}
