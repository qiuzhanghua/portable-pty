package pty

import (
	"os"
	"strings"
	"unicode/utf16"
)

// encodeEnvBlock renders env as the UTF-16 block CreateProcess expects: a
// sequence of NUL-terminated "name=value" entries followed by one extra NUL.
//
// Two normalisations happen first, both matching what portable-pty does and
// what Windows itself assumes:
//
//   - names are collapsed case-insensitively with the later value winning,
//     because Windows does not distinguish them;
//   - SYSTEMROOT is added when absent, since CreateProcess and a great many
//     programs fail without it.
//
// Entries with no "=" are dropped: CreateProcess would treat them as malformed.
//
// This lives here, rather than in the Windows-only file, because encoding an
// environment block is easy to get subtly wrong and this way it is covered by
// every platform's test run.
func encodeEnvBlock(env []string) []uint16 {
	env = ensureSystemRoot(dedupEnvFold(env))

	var b strings.Builder
	for _, kv := range env {
		b.WriteString(kv)
		b.WriteByte(0)
	}
	b.WriteByte(0) // the block ends with an empty entry

	// The NULs survive as U+0000 code units, which is exactly what the block
	// format wants. Values that are not valid UTF-8 have their invalid bytes
	// replaced; os.Environ never produces those on Windows.
	return utf16.Encode([]rune(b.String()))
}

// dedupEnvFold collapses repeated names case-insensitively. The first position
// is kept and the last value wins, matching how Windows resolves names and how
// os/exec's own dedupEnvCase behaves.
func dedupEnvFold(env []string) []string {
	out := make([]string, 0, len(env))
	index := make(map[string]int, len(env))

	for _, kv := range env {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue // malformed; CreateProcess would reject the block
		}
		key := strings.ToLower(name)
		if at, dup := index[key]; dup {
			out[at] = kv
			continue
		}
		index[key] = len(out)
		out = append(out, kv)
	}
	return out
}

// ensureSystemRoot appends SYSTEMROOT when the environment does not already
// name it and this process knows the value.
func ensureSystemRoot(env []string) []string {
	if _, ok := envLookupFold(env, "SYSTEMROOT"); ok {
		return env
	}
	root := os.Getenv("SYSTEMROOT")
	if root == "" {
		return env
	}
	return append(env, "SYSTEMROOT="+root)
}
