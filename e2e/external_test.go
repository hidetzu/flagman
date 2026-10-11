//go:build external

// The external tier: the other end is something this project did not write — GitHub, through
// the real gh CLI, and a real third-party repository's history. It reaches the network.
//
// It asserts only flagman's own correctness: a verdict comes back with a known exit code, and
// every source is either obtained or named as not obtained with a reason. It never asserts what
// the other side contains today (.claude/rules/verification.md).
package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	_, file, _, _ := runtime.Caller(0)
	dir, _ := os.MkdirTemp("", "flagman-ext-bin")
	bin = filepath.Join(dir, "flagman")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/flagman")
	cmd.Dir = filepath.Join(filepath.Dir(file), "..")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type decision struct {
	Snapshot struct {
		Task    *struct{ Ref, State string } `json:"task"`
		History *struct {
			Touches map[string]int `json:"touches"`
		} `json:"history"`
		Missing []struct{ Source, Reason string } `json:"missing"`
	} `json:"snapshot"`
	Result struct {
		Verdict string `json:"verdict"`
	} `json:"result"`
}

func runJSON(t *testing.T, dir string, args ...string) (decision, int) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"check", "-C", dir, "--json", "--no-log"}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	var d decision
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatalf("not JSON (%v): %s %s", err, out.String(), errb.String())
	}
	if code != 0 && code != 10 && code != 20 {
		t.Fatalf("exit %d is not a verdict: %s", code, errb.String())
	}
	t.Logf("verdict %s (exit %d)", d.Result.Verdict, code)
	return d, code
}

// A real issue through the real gh. Either the task is read, or its absence is named.
func TestRealIssueThroughGh(t *testing.T) {
	d, _ := runJSON(t, t.TempDir(), "--task", "cli/cli#1", "--tests", "pass")
	if d.Snapshot.Task != nil {
		t.Logf("task read: %s state=%s", d.Snapshot.Task.Ref, d.Snapshot.Task.State)
		return
	}
	for _, m := range d.Snapshot.Missing {
		if m.Source == "task" && m.Reason != "" {
			t.Logf("task not obtained, and said why: %s", m.Reason)
			return
		}
	}
	t.Fatal("the task was neither read nor named as not obtained")
}

// A real repository's history, with a change on top.
func TestThirdPartyRepositoryHistory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pflag")
	if out, err := exec.Command("git", "clone", "-q", "--depth", "200", "https://github.com/spf13/pflag", dir).CombinedOutput(); err != nil {
		t.Skipf("NOT-VERIFIED: the clone failed (external): %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "flag.go"), []byte("package pflag\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, _ := runJSON(t, dir, "--tests", "pass", "--action", "commit")
	if d.Snapshot.History == nil {
		t.Fatalf("history was not obtained from a real repository: %+v", d.Snapshot.Missing)
	}
	t.Logf("flag.go touched by %d commits in the window", d.Snapshot.History.Touches["flag.go"])
}
