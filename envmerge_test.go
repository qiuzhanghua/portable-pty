package pty

import (
	"testing"
)

// These exercise the Windows registry merge policy. They live in a
// platform-neutral file and run everywhere, so a mistake here fails on the
// Linux and macOS jobs too rather than only on Windows, where the failure is
// hard to read.

func TestMergeEnvEntriesReplacesLaterValues(t *testing.T) {
	env := []string{"A=process", "B=keep"}
	got := mergeEnvEntries(env, []envEntry{
		{name: "A", value: "machine"},
		{name: "A", value: "user"},
	})
	if want := []string{"A=user", "B=keep"}; !equalStrings(got, want) {
		t.Errorf("mergeEnvEntries = %v, want %v", got, want)
	}
}

func TestMergeEnvEntriesAddsNewNames(t *testing.T) {
	got := mergeEnvEntries([]string{"A=1"}, []envEntry{{name: "B", value: "2"}})
	if want := []string{"A=1", "B=2"}; !equalStrings(got, want) {
		t.Errorf("mergeEnvEntries = %v, want %v", got, want)
	}
}

func TestMergeEnvEntriesMatchesNamesCaseInsensitively(t *testing.T) {
	got := mergeEnvEntries([]string{"Path=process"}, []envEntry{{name: "PATH", value: "machine"}})
	want := []string{"PATH=process;machine"}
	if !equalStrings(got, want) {
		t.Errorf("mergeEnvEntries = %v, want %v", got, want)
	}
}

// Path accumulates rather than replacing, so machine and user Paths both
// survive; this is the one name upstream special-cases.
func TestMergeEnvEntriesAppendsPath(t *testing.T) {
	got := mergeEnvEntries(
		[]string{"Path=C:\\process"},
		[]envEntry{
			{name: "Path", value: "C:\\machine"},
			{name: "Path", value: "C:\\user"},
		},
	)
	want := []string{"Path=C:\\process;C:\\machine;C:\\user"}
	if !equalStrings(got, want) {
		t.Errorf("mergeEnvEntries = %v, want %v", got, want)
	}
}

func TestMergeEnvEntriesPathWhenAbsent(t *testing.T) {
	got := mergeEnvEntries(nil, []envEntry{{name: "Path", value: "C:\\machine"}})
	if want := []string{"Path=C:\\machine"}; !equalStrings(got, want) {
		t.Errorf("mergeEnvEntries = %v, want %v", got, want)
	}
}

// An empty existing Path must not produce a leading separator.
func TestMergeEnvEntriesEmptyPath(t *testing.T) {
	got := mergeEnvEntries([]string{"Path="}, []envEntry{{name: "Path", value: "C:\\machine"}})
	if want := []string{"Path=C:\\machine"}; !equalStrings(got, want) {
		t.Errorf("mergeEnvEntries = %v, want %v", got, want)
	}
}

func TestMergeEnvEntriesIgnoresNamesDifferingBeyondPath(t *testing.T) {
	got := mergeEnvEntries([]string{"TEMP=C:\\process"}, []envEntry{{name: "TEMP", value: "C:\\machine"}})
	if want := []string{"TEMP=C:\\machine"}; !equalStrings(got, want) {
		t.Errorf("mergeEnvEntries = %v, want %v", got, want)
	}
}
