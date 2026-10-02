package pty

import (
	"testing"
)

func TestEnvGet(t *testing.T) {
	env := []string{"A=1", "B=2", "WITHOUT_EQUALS"}

	if v, ok := EnvGet(env, "A"); !ok || v != "1" {
		t.Errorf(`EnvGet(A) = %q, %v; want "1", true`, v, ok)
	}
	if _, ok := EnvGet(env, "MISSING"); ok {
		t.Error("EnvGet(MISSING) reported the variable as present")
	}
	if _, ok := EnvGet(env, "WITHOUT_EQUALS"); ok {
		t.Error("EnvGet matched an entry that has no separator")
	}
}

func TestEnvGetEmptyValue(t *testing.T) {
	v, ok := EnvGet([]string{"EMPTY="}, "EMPTY")
	if !ok || v != "" {
		t.Errorf(`EnvGet(EMPTY) = %q, %v; want "", true`, v, ok)
	}
}

func TestEnvSetReplacesInPlace(t *testing.T) {
	got := EnvSet([]string{"A=1", "B=2"}, "B", "changed")
	if want := []string{"A=1", "B=changed"}; !equalStrings(got, want) {
		t.Errorf("EnvSet = %v, want %v", got, want)
	}
}

func TestEnvSetAppendsWhenAbsent(t *testing.T) {
	got := EnvSet([]string{"A=1"}, "B", "2")
	if want := []string{"A=1", "B=2"}; !equalStrings(got, want) {
		t.Errorf("EnvSet = %v, want %v", got, want)
	}
}

// CommandBuilder::env records the caller's spelling of the name rather than
// reusing whatever case the existing entry happened to use.
func TestEnvSetKeepsCallerSpelling(t *testing.T) {
	got := EnvSet([]string{"Key=old"}, "Key", "new")
	if want := []string{"Key=new"}; !equalStrings(got, want) {
		t.Errorf("EnvSet = %v, want %v", got, want)
	}
}

func TestEnvSetDoesNotMutateInput(t *testing.T) {
	env := []string{"A=1"}
	_ = EnvSet(env, "B", "2")
	if want := []string{"A=1"}; !equalStrings(env, want) {
		t.Errorf("EnvSet mutated its input: %v", env)
	}
}

func TestEnvUnsetRemovesEveryMatch(t *testing.T) {
	got := EnvUnset([]string{"A=1", "B=2", "A=3"}, "A")
	if want := []string{"B=2"}; !equalStrings(got, want) {
		t.Errorf("EnvUnset = %v, want %v", got, want)
	}
}
