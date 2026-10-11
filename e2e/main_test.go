//go:build e2e

// The final gate: build the flagman binary from this tree, then drive it the way an agent or a
// person would, against real git repositories created in temporary directories.
//
// Nothing here reaches the network. `gh` is replaced by a stub on PATH that records every call;
// TestIssueIsFetchedOnlyWhenNamed proves both that a task file never reaches it and that naming
// an issue does (the control).
package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..")
	dir, err := os.MkdirTemp("", "flagman-e2e-bin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "flagman")
	// Built from this tree on every run: there is no prebuilt artefact to go stale.
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/flagman")
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic("go build failed: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type repo struct {
	t    *testing.T
	dir  string
	stub string // the directory holding the gh stub and its call record
	env  []string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	r := &repo{t: t, dir: t.TempDir()}
	stub := t.TempDir()
	r.stub = stub
	// A gh stub that records each call and answers with a fixed issue.
	script := "#!/bin/sh\necho \"$@\" >> " + filepath.Join(stub, "calls") + "\n" +
		"cat <<'JSON'\n{\"state\":\"OPEN\",\"labels\":[{\"name\":\"needs-decision\"}],\"body\":\"## Goal\\nx\\n\",\"comments\":[]}\nJSON\n"
	if err := os.WriteFile(filepath.Join(stub, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r.env = append(os.Environ(),
		"PATH="+stub+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "FLAGMAN_LOG=")
	r.git("init", "-q", "-b", "main")
	r.write("README.md", "# app\n")
	r.write("internal/parser/lex.go", "package parser\n")
	r.write("internal/parser/lex_test.go", "package parser\n")
	r.write("internal/billing/charge.go", "package billing\n")
	r.git("add", ".")
	r.git("commit", "-q", "-m", "init")
	r.git("checkout", "-q", "-b", "feat/lexer")
	return r
}

func (r *repo) ghCalls() string {
	b, _ := os.ReadFile(filepath.Join(r.stub, "calls"))
	return string(b)
}

func (r *repo) git(args ...string) {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Env = r.dir, r.env
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (r *repo) write(p, body string) {
	r.t.Helper()
	full := filepath.Join(r.dir, p)
	os.MkdirAll(filepath.Dir(full), 0o755)
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// flagman runs the binary and returns stdout, stderr and the exit code.
func (r *repo) flagman(stdin string, args ...string) (string, string, int) {
	r.t.Helper()
	cmd := exec.Command(bin, append([]string{"-C", r.dir}[:0:0], args...)...)
	cmd.Dir, cmd.Env = r.dir, r.env
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatalf("flagman did not run: %v", err)
	}
	return out.String(), errb.String(), code
}

const task = `# Fix exponent lexing
## Goal
1e10 lexes as one token.
## Scope
- ` + "`internal/parser/`" + `
## Out of Scope
- ` + "`internal/billing/`" + `
## Acceptance Criteria
- [ ] lex_test covers 1e10
`

func (r *repo) scopedChange() {
	r.write("TASK.md", task)
	r.write("internal/parser/lex.go", "package parser\n\nfunc Lex() {}\n")
	r.write("internal/parser/lex_test.go", "package parser\n\nfunc TestLex() {}\n")
	r.git("add", "internal/parser")
}

func TestScopedTestedCommitContinues(t *testing.T) {
	r := newRepo(t)
	r.scopedChange()
	out, errOut, code := r.flagman("", "check", "--task", "TASK.md", "--tests", "pass", "--action", "commit")
	if code != 0 || !strings.Contains(out, "CONTINUE") {
		t.Fatalf("want exit 0 / CONTINUE, got %d\n%s%s", code, out, errOut)
	}
	if !strings.Contains(out, "task=TASK.md") || !strings.Contains(out, "base=main") {
		t.Fatalf("the context line must name the task and the base:\n%s", out)
	}
}

func TestMergeDefers(t *testing.T) {
	r := newRepo(t)
	r.scopedChange()
	out, _, code := r.flagman("", "check", "--task", "TASK.md", "--tests", "pass", "--action", "merge")
	if code != 20 || !strings.Contains(out, "HARD action.irreversible") {
		t.Fatalf("want exit 20 with action.irreversible, got %d\n%s", code, out)
	}
}

func TestOutOfScopeDefers(t *testing.T) {
	r := newRepo(t)
	r.scopedChange()
	r.write("internal/billing/charge.go", "package billing\n// changed\n")
	out, _, code := r.flagman("", "check", "--task", "TASK.md", "--tests", "pass")
	if code != 20 || !strings.Contains(out, "diff.outside-scope") || !strings.Contains(out, "internal/billing/charge.go") {
		t.Fatalf("want exit 20 naming the out-of-scope path, got %d\n%s", code, out)
	}
}

func TestUndecidedTaskDefers(t *testing.T) {
	r := newRepo(t)
	r.scopedChange()
	r.write("TASK.md", task+"\n## Human Decision\n- error format: TBD\n")
	out, _, code := r.flagman("", "check", "--task", "TASK.md", "--tests", "pass")
	if code != 20 || !strings.Contains(out, "task.open-decision") || !strings.Contains(out, "TASK.md:") {
		t.Fatalf("want exit 20 quoting where the open decision is, got %d\n%s", code, out)
	}
}

func TestFailingTestCommandOnDefaultBranchDefers(t *testing.T) {
	r := newRepo(t)
	r.git("checkout", "-q", "main")
	r.scopedChange()
	r.write(".flagman/config.json", "")
	os.Remove(filepath.Join(r.dir, ".flagman/config.json"))
	_, _, _ = r.flagman("", "init")
	cfgPath := filepath.Join(r.dir, ".flagman", "config.json")
	b, _ := os.ReadFile(cfgPath)
	var cfg map[string]any
	json.Unmarshal(b, &cfg)
	cfg["evidence"].(map[string]any)["tests"] = map[string]any{"command": "echo '1 test failed'; exit 1"}
	b, _ = json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(cfgPath, b, 0o644)
	r.git("add", ".flagman") // committing the config is the documented use; stage it so it is part of this change
	r.git("commit", "-q", "-m", "add flagman config")
	out, _, code := r.flagman("", "check", "--task", "TASK.md", "--run-tests")
	if code != 20 || !strings.Contains(out, "tests.failing") || !strings.Contains(out, "1 test failed") || !strings.Contains(out, "repo.default-branch") {
		t.Fatalf("want exit 20 with the failing command's last line and the default branch, got %d\n%s", code, out)
	}
}

func TestOutsideARepositoryItStillAnswers(t *testing.T) {
	r := newRepo(t)
	empty := t.TempDir()
	out, _, code := r.flagman("", "check", "-C", empty, "--no-log", "--action", "push")
	if code != 20 && code != 10 {
		t.Fatalf("an unreadable repository must not read as fine; got %d\n%s", code, out)
	}
	if !strings.Contains(out, "repo.not-obtained") || !strings.Contains(out, "Not obtained") {
		t.Fatalf("the missing evidence must be named:\n%s", out)
	}
}

// The whole feedback loop through the binary: decide, record the human and the outcome, replay,
// and reproduce a decision from the log alone.
func TestFeedbackLoop(t *testing.T) {
	r := newRepo(t)
	r.scopedChange()
	// No test result given: a weak signal (tests.not-obtained) that stays below review.
	if _, _, code := r.flagman("", "check", "--task", "TASK.md"); code != 0 {
		t.Fatalf("first decision should continue, got %d", code)
	}
	r.write("internal/billing/charge.go", "package billing\n// changed\n")
	if _, _, code := r.flagman("", "check", "--task", "TASK.md", "--tests", "pass"); code != 20 {
		t.Fatalf("second decision should defer, got %d", code)
	}
	out, _, code := r.flagman("", "human", "last", "changed", "--note", "billing must not move")
	if code != 0 || !strings.Contains(out, "truth is now defer") {
		t.Fatalf("human: %d %s", code, out)
	}
	logOut, _, _ := r.flagman("", "log")
	first := strings.Fields(strings.Split(logOut, "\n")[0])[0]
	out, _, code = r.flagman("", "outcome", first, "problem", "--note", "the lexer change broke parsing of hex")
	if code != 0 || !strings.Contains(out, "FALSE AUTONOMY") {
		t.Fatalf("a problem after a continue must be called a false autonomy: %d %s", code, out)
	}
	out, _, code = r.flagman("", "replay")
	if code != 0 {
		t.Fatalf("replay: %d %s", code, out)
	}
	for _, want := range []string{"false autonomy        1/2", "human needed, caught  1/2", "reproducibility: 2/2", "false autonomies: " + first} {
		if !strings.Contains(out, want) {
			t.Errorf("replay output lacks %q:\n%s", want, out)
		}
	}
	out, _, _ = r.flagman("", "show", first)
	if !strings.Contains(out, "reproduced:") || !strings.Contains(out, "truth: defer (from outcome)") {
		t.Fatalf("show must reproduce the decision and give its truth:\n%s", out)
	}
	// A second check in a fresh repository must not see anything flagman itself wrote.
	out, _, _ = r.flagman("", "check", "--task", "TASK.md", "--tests", "pass", "--no-log", "-v")
	if strings.Contains(out, ".flagman/") || strings.Contains(out, "TASK.md (") {
		t.Fatalf("flagman's own files reached its evidence:\n%s", out)
	}
	// The log stays out of git by default.
	if err := exec.Command("git", "-C", r.dir, "check-ignore", "-q", ".flagman/decisions.jsonl").Run(); err != nil {
		t.Fatalf("the decision log must be git-ignored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, ".flagman", ".gitignore")); err == nil {
		t.Fatal("flagman must not write ignore files into the working tree")
	}
	// A stricter config, compared on the same log.
	strict := filepath.Join(t.TempDir(), "strict.json")
	r.flagman("", "init", "--config", strict)
	b, _ := os.ReadFile(strict)
	b = bytes.Replace(b, []byte(`"review": 30`), []byte(`"review": 5`), 1)
	b = bytes.Replace(b, []byte(`"defer": 60`), []byte(`"defer": 10`), 1)
	os.WriteFile(strict, b, 0o644)
	out, _, _ = r.flagman("", "replay", "--compare", strict)
	if !strings.Contains(out, "B is better than A") || !strings.Contains(out, "continue → defer") {
		t.Fatalf("compare must find the stricter config better here and list the changed verdict:\n%s", out)
	}
}

// The log must hold paths and counts, never file contents.
func TestLogHoldsNoFileContents(t *testing.T) {
	r := newRepo(t)
	r.scopedChange()
	r.write("internal/parser/secret.go", "package parser\nconst token = \"ghp_SHOULD_NEVER_BE_LOGGED\"\n")
	r.flagman("", "check", "--task", "TASK.md", "--tests", "pass")
	b, err := os.ReadFile(filepath.Join(r.dir, ".flagman", "decisions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("ghp_SHOULD_NEVER_BE_LOGGED")) {
		t.Fatal("file contents reached the decision log")
	}
	if !bytes.Contains(b, []byte("internal/parser/secret.go")) {
		t.Fatal("control: the path itself should be logged")
	}
}

func TestIssueIsFetchedOnlyWhenNamed(t *testing.T) {
	r := newRepo(t)
	r.scopedChange()
	r.flagman("", "check", "--task", "TASK.md", "--tests", "pass", "--no-log")
	if calls := r.ghCalls(); calls != "" {
		t.Fatalf("a task file must not reach gh; it was called with: %s", calls)
	}
	// Control: naming an issue does reach gh (the stub), and its label is read.
	out, _, code := r.flagman("", "check", "--task", "acme/app#7", "--tests", "pass", "--no-log")
	if calls := r.ghCalls(); !strings.Contains(calls, "issue view 7 -R acme/app") {
		t.Fatalf("control: gh was not called for a named issue (calls: %q)", calls)
	}
	if code != 20 || !strings.Contains(out, "needs-decision") {
		t.Fatalf("the stubbed issue carries needs-decision; want defer, got %d\n%s", code, out)
	}
}

func TestClaudeCodeHook(t *testing.T) {
	r := newRepo(t)
	r.scopedChange()
	in := `{"tool_name":"Bash","cwd":"` + r.dir + `","tool_input":{"command":"gh pr merge 3 --squash"}}`
	out, _, code := r.flagman(in, "hook", "claude-code")
	if code != 0 {
		t.Fatalf("the hook must exit 0, got %d", code)
	}
	var resp struct {
		H struct {
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil || resp.H.Decision != "ask" || !strings.Contains(resp.H.Reason, "action.irreversible") {
		t.Fatalf("a merge must come back as ask with its reason; got %q (%v)", out, err)
	}
	b, _ := os.ReadFile(filepath.Join(r.dir, ".flagman", "decisions.jsonl"))
	if bytes.Contains(b, []byte("gh pr merge 3")) {
		t.Fatal("the raw command must not be logged")
	}
	// Review: no task and no test result on a push of a feature branch scores between the
	// thresholds. The hook shows the reasons but takes no permission decision.
	in = `{"tool_name":"Bash","cwd":"` + r.dir + `","tool_input":{"command":"git push origin feat/lexer"}}`
	out, _, code = r.flagman(in, "hook", "claude-code")
	var rev map[string]any
	if err := json.Unmarshal([]byte(out), &rev); err != nil || code != 0 {
		t.Fatalf("a review must come back as JSON with exit 0; got %d %q (%v)", code, out, err)
	}
	msg, _ := rev["systemMessage"].(string)
	hs, _ := rev["hookSpecificOutput"].(map[string]any)
	if !strings.Contains(msg, "REVIEW") || !strings.Contains(msg, "flagman outcome d-") || hs["additionalContext"] != msg {
		t.Fatalf("the review reasons must reach both the human and the agent:\n%s", out)
	}
	if _, has := hs["permissionDecision"]; has {
		t.Fatalf("a review must not take a permission decision:\n%s", out)
	}
	in = `{"tool_name":"Bash","cwd":"` + r.dir + `","tool_input":{"command":"go test ./..."}}`
	if out, _, code := r.flagman(in, "hook", "claude-code"); code != 0 || out != "" {
		t.Fatalf("a command flagman does not judge gets no opinion; got %d %q", code, out)
	}
}
