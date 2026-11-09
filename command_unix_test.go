//go:build unix

package pty

import (
	"os"
	"path"
	"strings"
	"testing"
)

// Shell looks at the parent chain, the login shell and $SHELL, in that order.
// Whatever it settles on must be runnable.
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
// access(X_OK) check does. Another layer may legitimately answer instead.
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

// Shell's precedence: the enclosing interactive shell beats the login shell,
// which beats $SHELL; an unusable candidate is skipped rather than returned.
func TestSelectShellPrecedence(t *testing.T) {
	bash := "/bin/bash"
	if !isExecutable(bash) {
		t.Skipf("%s is not executable here", bash)
	}
	cases := []struct {
		name                        string
		enclosing, login, env, want string
	}{
		{"enclosing shell wins", bash, "/bin/sh", "/bin/sh", bash},
		{"login shell when the chain has none", "", bash, "/bin/sh", bash},
		{"$SHELL when both databases are silent", "", "", bash, bash},
		{"unusable candidates are skipped", "/nonexistent", "", "", "/bin/sh"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := selectShell(tc.enclosing, tc.login, tc.env); got != tc.want {
				t.Errorf("selectShell(%q, %q, %q) = %q, want %q",
					tc.enclosing, tc.login, tc.env, got, tc.want)
			}
		})
	}
}

// The walk must skip wrappers (sh, python) and return the interactive shell
// above them.
func TestEnclosingShellFindsNearestInteractiveShell(t *testing.T) {
	bash := "/bin/bash"
	if !isExecutable(bash) {
		t.Skipf("%s is not executable here", bash)
	}
	rows := map[int]processRow{
		100: {ppid: 101, comm: "go"},
		101: {ppid: 102, comm: "/bin/sh"},
		102: {ppid: 1, comm: bash},
	}
	if got := enclosingShellFrom(100, rows); got != bash {
		t.Errorf("enclosingShellFrom() = %q, want %q", got, bash)
	}
}

// A bare `comm` is resolved through PATH, but only after its basename matched.
func TestEnclosingShellResolvesBareNames(t *testing.T) {
	rows := map[int]processRow{
		100: {ppid: 0, comm: "bash"},
	}
	got := enclosingShellFrom(100, rows)
	if got == "" {
		t.Skip("bash is not on PATH here")
	}
	if path.Base(got) != "bash" || !path.IsAbs(got) {
		t.Errorf("enclosingShellFrom() = %q, want an absolute bash path", got)
	}
}

// An entry that names a shell but cannot be executed must not be guessed at.
func TestEnclosingShellSkipsUnusableExecutable(t *testing.T) {
	rows := map[int]processRow{
		100: {ppid: 0, comm: "/nonexistent/definitely-not-zsh/zsh"},
	}
	if got := enclosingShellFrom(100, rows); got != "" {
		t.Errorf("enclosingShellFrom() = %q, want no answer for an unusable path", got)
	}
}

// Cycles and chains longer than the depth bound terminate without an answer.
func TestEnclosingShellTerminates(t *testing.T) {
	cycle := map[int]processRow{
		100: {ppid: 101, comm: "go"},
		101: {ppid: 100, comm: "go"},
	}
	if got := enclosingShellFrom(100, cycle); got != "" {
		t.Errorf("enclosingShellFrom(cycle) = %q, want \"\"", got)
	}

	deep := make(map[int]processRow, maxChainDepth+1)
	for pid := 100; pid < 100+maxChainDepth+1; pid++ {
		deep[pid] = processRow{ppid: pid + 1, comm: "go"}
	}
	deep[100+maxChainDepth+1] = processRow{ppid: 0, comm: "bash"}
	if got := enclosingShellFrom(100, deep); got != "" {
		t.Errorf("enclosingShellFrom(deep) = %q, want \"\" beyond the depth bound", got)
	}
}

// ps must at least report this process; without it the layer is skipped.
func TestReadProcessTableIncludesSelf(t *testing.T) {
	rows := readProcessTable()
	if rows == nil {
		t.Skip("ps is unavailable")
	}
	if _, ok := rows[os.Getpid()]; !ok {
		t.Errorf("readProcessTable() has no row for pid %d", os.Getpid())
	}
}

// passwd parsing is exercised through the injected reader, so it works on
// macOS too, where the real account is not in /etc/passwd.
func TestPasswdShellFromParsesEntries(t *testing.T) {
	const passwd = "# not a real entry\nroot:x:0:0:root:/root:/bin/sh\nalice:x:501:20::/home/alice:/bin/bash\n"
	if got := passwdShellFrom(501, strings.NewReader(passwd)); got != "/bin/bash" {
		t.Errorf("passwdShellFrom(501) = %q, want /bin/bash", got)
	}
	if got := passwdShellFrom(999, strings.NewReader(passwd)); got != "" {
		t.Errorf("passwdShellFrom(999) = %q, want \"\"", got)
	}
	if got := passwdShellFrom(0, strings.NewReader("bad:entry\n")); got != "" {
		t.Errorf("passwdShellFrom(malformed) = %q, want \"\"", got)
	}
}
