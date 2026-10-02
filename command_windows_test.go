//go:build windows

package pty

import (
	"os"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

const machineEnvKey = `System\CurrentControlSet\Control\Session Manager\Environment`

// CommandBuilder::get_shell uses ComSpec on Windows.
func TestShellIsComSpec(t *testing.T) {
	want := os.Getenv("ComSpec")
	if want == "" {
		want = "cmd.exe"
	}
	if got := Shell(); got != want {
		t.Errorf("Shell() = %q, want %q", got, want)
	}
}

// Windows environment names are case-insensitive, unlike Unix.
func TestEnvCaseInsensitiveOnWindows(t *testing.T) {
	env := []string{"Key=1"}

	if v, ok := EnvGet(env, "KEY"); !ok || v != "1" {
		t.Errorf(`EnvGet(KEY) = %q, %v; want "1", true`, v, ok)
	}
	if got := EnvSet(env, "KEY", "2"); !slices.Equal(got, []string{"KEY=2"}) {
		t.Errorf("EnvSet = %v, want [KEY=2]", got)
	}
	if got := EnvUnset(env, "KEY"); len(got) != 0 {
		t.Errorf("EnvUnset = %v, want empty", got)
	}
}

// CommandBuilder::get_base_env merges the machine environment from the
// registry, so its Path is present in the merged Path.
//
// The comparison has to be against the *expanded* machine Path: the registry
// stores Path as REG_EXPAND_SZ, and the merged value is expanded.
func TestEnvironMergesMachineRegistry(t *testing.T) {
	env := Environ()
	if _, ok := EnvGet(env, "Path"); !ok {
		t.Fatal("Environ() has no Path")
	}

	key, err := registry.OpenKey(registry.LOCAL_MACHINE, machineEnvKey, registry.QUERY_VALUE)
	if err != nil {
		t.Skipf("registry unavailable: %v", err)
	}
	defer key.Close()

	machinePath, valtype, err := key.GetStringValue("Path")
	if err != nil || machinePath == "" {
		t.Skipf("no machine Path in the registry: %v", err)
	}
	if valtype == registry.EXPAND_SZ {
		expanded, err := registry.ExpandString(machinePath)
		if err != nil {
			t.Skipf("cannot expand the machine Path: %v", err)
		}
		machinePath = expanded
	}

	got, _ := EnvGet(env, "Path")
	if !strings.Contains(got, machinePath) {
		t.Errorf("Environ() Path does not contain the machine Path")
		t.Errorf("  got:  %.300s", got)
		t.Errorf("  want: %.300s", machinePath)
	}
}

// REG_EXPAND_SZ values must come back expanded.
//
// This asserts the property rather than comparing against
// registry.ExpandString, which would only restate the implementation: no merged
// value may still reference a variable that the merged environment defines.
func TestEnvironHasNoUnexpandedReferences(t *testing.T) {
	env := Environ()

	inspected := 0
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || !strings.Contains(value, "%") {
			continue
		}
		inspected++
		for _, ref := range percentRefs(value) {
			if _, defined := EnvGet(env, ref); !defined {
				continue // cannot have been expanded from here anyway
			}
			t.Errorf("%s = %q still contains an unexpanded %%%s%%, and the environment defines %s",
				name, value, ref, ref)
		}
	}
	t.Logf("inspected %d value(s) containing a percent sign", inspected)
}

// percentRefs returns the %NAME% references in value.
func percentRefs(value string) []string {
	var refs []string
	for {
		_, rest, ok := strings.Cut(value, "%")
		if !ok {
			return refs
		}
		ref, after, ok := strings.Cut(rest, "%")
		if !ok {
			return refs
		}
		if ref != "" {
			refs = append(refs, ref)
		}
		value = after
	}
}

// portable-pty skips USERNAME during the machine pass so the process value
// survives.
func TestEnvironKeepsProcessUsername(t *testing.T) {
	want := os.Getenv("USERNAME")
	if want == "" {
		t.Skip("no USERNAME in the process environment")
	}
	got, ok := EnvGet(Environ(), "USERNAME")
	if !ok || got != want {
		t.Errorf("USERNAME = %q, %v; want the process value %q", got, ok, want)
	}
}

// A login shell is a Unix notion; Windows argv[0] is left alone.
func TestLoginShellArgv0UnchangedOnWindows(t *testing.T) {
	cmd := LoginShell()
	if len(cmd.Args) == 0 {
		t.Fatal("LoginShell().Args is empty")
	}
	if strings.HasPrefix(cmd.Args[0], "-") {
		t.Errorf("argv[0] = %q, want no leading dash on Windows", cmd.Args[0])
	}
}
