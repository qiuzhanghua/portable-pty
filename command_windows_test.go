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
// registry, so values absent from the process environment appear.
func TestEnvironMergesMachineRegistry(t *testing.T) {
	env := Environ()
	if _, ok := EnvGet(env, "Path"); !ok {
		t.Error("Environ() has no Path")
	}

	key, err := registry.OpenKey(registry.LOCAL_MACHINE, machineEnvKey, registry.QUERY_VALUE)
	if err != nil {
		t.Skipf("registry unavailable: %v", err)
	}
	defer key.Close()

	machinePath, _, err := key.GetStringValue("Path")
	if err != nil || machinePath == "" {
		t.Skipf("no machine Path in the registry: %v", err)
	}
	got, _ := EnvGet(env, "Path")
	if !strings.Contains(got, machinePath) {
		t.Errorf("Environ() Path does not contain the machine Path.\n got: %.200s\nwant substring: %.200s", got, machinePath)
	}
}

// REG_EXPAND_SZ values are handed back expanded, as ExpandEnvironmentStringsW
// does for portable-pty.
func TestEnvironExpandsExpandString(t *testing.T) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, machineEnvKey, registry.QUERY_VALUE)
	if err != nil {
		t.Skipf("registry unavailable: %v", err)
	}
	defer key.Close()

	names, err := key.ReadValueNames(0)
	if err != nil {
		t.Skipf("cannot enumerate the machine environment: %v", err)
	}

	env := Environ()
	checked := 0
	for _, name := range names {
		raw, valtype, err := key.GetStringValue(name)
		if err != nil || valtype != registry.EXPAND_SZ {
			continue
		}
		want, err := registry.ExpandString(raw)
		if err != nil || want == raw {
			continue // nothing actually changed, so this proves nothing
		}
		got, ok := EnvGet(env, name)
		if !ok {
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want the expanded form %q", name, got, want)
		}
		checked++
	}
	t.Logf("verified %d expanded value(s)", checked)
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
