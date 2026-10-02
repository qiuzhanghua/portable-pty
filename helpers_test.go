package pty

// Test helpers that stand in for the slices package, which arrived in Go 1.21.
// The module keeps a Go 1.20 floor, so these two are used instead of
// slices.Equal and slices.Contains throughout the tests.

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
