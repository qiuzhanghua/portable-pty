//go:build unix

package pty

import (
	"os"
	"path"
	"testing"
)

// CommandBuilder::get_shell: $SHELL when it is executable, otherwise the passwd
// shell, otherwise /bin/sh. Whatever it settles on must be runnable.
func TestShellIsExecutable(t *testing.T) {
	sh := Shell()
	if !path.IsAbs(sh) {
		t.Errorf("Shell() = %q, want an absolute path", sh)
	}
	info, err := os.Stat(sh)
	if err != nil {
		t.Fatalf("Shell() = %q: %v", sh, err)
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		t.Errorf("Shell() = %q is not executable", sh)
	}
}

// $SHELL pointing at something unusable must be rejected, as upstream's
// access(X_OK) check does.
func TestShellRejectsUnusableSHELL(t *testing.T) {
	const bogus = "/nonexistent/definitely-not-a-shell"
	t.Setenv("SHELL", bogus)

	if got := Shell(); got == bogus {
		t.Errorf("Shell() = %q, want a fallback: a $SHELL that is not executable must be rejected", got)
	} else if got == "" {
		t.Error("Shell() returned empty")
	}
}

// CommandBuilder::get_base_env inserts SHELL when the process environment has
// no such variable.
func TestEnvironAlwaysHasShell(t *testing.T) {
	v, ok := EnvGet(Environ(), "SHELL")
	if !ok {
		t.Fatal("Environ() has no SHELL")
	}
	if v == "" {
		t.Fatal("Environ() has an empty SHELL")
	}
}

func TestEnvironIsSnapshotOfProcessEnv(t *testing.T) {
	t.Setenv("PTY_TEST_MARKER", "present")
	if v, ok := EnvGet(Environ(), "PTY_TEST_MARKER"); !ok || v != "present" {
		t.Errorf("Environ() missed PTY_TEST_MARKER: %q, %v", v, ok)
	}
}

// passwdShell must either find an executable shell or decline; it must never
// return something that cannot be run.
func TestPasswdShellIsUsableOrEmpty(t *testing.T) {
	sh := passwdShell()
	if sh == "" {
		t.Skip("this account is not in /etc/passwd, which is expected on macOS")
	}
	info, err := os.Stat(sh)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		t.Errorf("passwdShell() = %q, which is not executable (%v)", sh, err)
	}
}

// CommandBuilder::new: an explicit environment, and a home-directory cwd.
func TestCommandDefaults(t *testing.T) {
	cmd := Command("/bin/sh")

	if cmd.Env == nil {
		t.Error("Command().Env is nil; want an explicit snapshot")
	}
	if want := []string{"/bin/sh"}; !equalStrings(cmd.Args, want) {
		t.Errorf("Command().Args = %v, want %v", cmd.Args, want)
	}

	home, err := HomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	if cmd.Dir != home {
		t.Errorf("Command().Dir = %q, want the home directory %q (set cmd.Dir to override)", cmd.Dir, home)
	}
}

func TestCommandPassesArgumentsThrough(t *testing.T) {
	cmd := Command("/bin/sh", "-c", "true")
	if want := []string{"/bin/sh", "-c", "true"}; !equalStrings(cmd.Args, want) {
		t.Errorf("Command().Args = %v, want %v", cmd.Args, want)
	}
}

// CommandBuilder::new_default_prog: argv[0] is the shell's basename prefixed
// with "-", which is what makes it a login shell.
func TestLoginShellArgv0(t *testing.T) {
	cmd := LoginShell()
	if len(cmd.Args) == 0 {
		t.Fatal("LoginShell().Args is empty")
	}
	want := "-" + path.Base(cmd.Path)
	if cmd.Args[0] != want {
		t.Errorf("argv[0] = %q, want %q", cmd.Args[0], want)
	}
}

// Unix environment names are case-sensitive, unlike Windows.
func TestEnvCaseSensitiveOnUnix(t *testing.T) {
	env := []string{"Key=1"}
	if _, ok := EnvGet(env, "KEY"); ok {
		t.Error("EnvGet matched KEY against Key; Unix names are case-sensitive")
	}
	if got := EnvSet(env, "KEY", "2"); len(got) != 2 {
		t.Errorf("EnvSet = %v, want two separate entries on Unix", got)
	}
}
