package rules

import (
	"strings"
	"testing"

	"github.com/hidetzu/flagman/internal/config"
	"github.com/hidetzu/flagman/internal/score"
	"github.com/hidetzu/flagman/internal/snapshot"
)

func TestMatchPath(t *testing.T) {
	cases := []struct {
		pat, p string
		want   bool
	}{
		{".github/**", ".github/workflows/ci.yml", true},
		{"**/auth/**", "internal/auth/login.go", true},
		{"**/auth/**", "auth/login.go", true},
		{"**/auth/**", "internal/author.go", false},
		{"**/*_test.go", "a/b/c_test.go", true},
		{"**/*_test.go", "c_test.go", true},
		{"Dockerfile", "deploy/Dockerfile", true},
		{"LICENSE*", "LICENSE.md", true},
		{"docs/adr/**", "docs/adr/0001-x.md", true},
		{"docs/adr/**", "docs/guide.md", false},
		{"**/*.sql", "db/migrations/1.sql", true},
	}
	for _, c := range cases {
		if got := MatchPath(c.pat, c.p); got != c.want {
			t.Errorf("MatchPath(%q, %q) = %v, want %v", c.pat, c.p, got, c.want)
		}
	}
}

func find(fs []score.Finding, rule string) []score.Finding {
	var out []score.Finding
	for _, f := range fs {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

func base() *snapshot.Snapshot {
	return &snapshot.Snapshot{
		Action:  snapshot.ActionCommit,
		Repo:    &snapshot.Repo{Branch: "feat", DefaultBranch: "main"},
		Diff:    &snapshot.Diff{Files: []snapshot.ChangedFile{{Path: "a/x.go", Status: "M", Added: 1}, {Path: "a/x_test.go", Status: "M", Added: 1}}, Insertions: 2},
		History: &snapshot.History{WindowDays: 90},
		Tests:   &snapshot.Tests{Status: snapshot.TestsPass, Source: "flag"},
		Task:    &snapshot.Task{Ref: "T", HasGoal: true, HasScope: true, HasOutOfScope: true, HasAcceptanceCriteria: true, Scope: []string{"a/"}, OutOfScope: []string{"b/"}},
		ADRs:    &snapshot.ADRs{Dir: "docs/adr"},
	}
}

func TestEveryRuleRunsOnEverySnapshot(t *testing.T) {
	// The emptiest snapshot: nothing obtained. Every rule must still report, and nothing
	// obtained must not be reported as clear.
	s := &snapshot.Snapshot{Action: snapshot.ActionCommit}
	fs := Evaluate(s, config.Default())
	for _, n := range Names() {
		if len(find(fs, n)) == 0 {
			t.Errorf("rule %s reported nothing", n)
		}
	}
	for _, n := range []string{"diff.large", "history.hotspot", "tests.failing", "adr.names-changed-path", "task.closed"} {
		if got := find(fs, n)[0].Status; got != score.NotObtained {
			t.Errorf("%s with no evidence reported %s, want not-obtained", n, got)
		}
	}
	if got := find(fs, "repo.not-obtained")[0].Status; got != score.Fired {
		t.Errorf("repo.not-obtained = %s, want fired", got)
	}
}

func TestCleanSnapshotContinues(t *testing.T) {
	c := config.Default()
	fs := Evaluate(base(), c)
	r := score.Compute(fs, c.Thresholds.Review, c.Thresholds.Defer)
	if r.Verdict != score.Continue || r.Score != 0 {
		t.Fatalf("clean snapshot: %s %d, fired: %v", r.Verdict, r.Score, score.FiredOnly(fs))
	}
}

func TestOutsideScopeIsHard(t *testing.T) {
	s := base()
	s.Diff.Files = append(s.Diff.Files, snapshot.ChangedFile{Path: "b/y.go", Status: "M"})
	fs := Evaluate(s, config.Default())
	f := find(fs, "diff.outside-scope")[0]
	if f.Status != score.Fired || !f.Hard || !strings.Contains(strings.Join(f.Evidence, " "), "b/y.go") {
		t.Fatalf("outside-scope finding: %+v", f)
	}
	nis := find(fs, "diff.not-in-scope")[0]
	if nis.Status != score.Fired {
		t.Fatalf("b/y.go is not in scope a/ but not-in-scope did not fire: %+v", nis)
	}
}

func TestOffRuleScoresNothingButStillReports(t *testing.T) {
	c := config.Default()
	p := c.Rules["action.irreversible"]
	p.Off = true
	c.Rules["action.irreversible"] = p
	s := base()
	s.Action = snapshot.ActionMerge
	fs := Evaluate(s, c)
	f := find(fs, "action.irreversible")[0]
	if f.Status != score.Off || f.Points != 0 || f.Hard {
		t.Fatalf("off rule: %+v", f)
	}
}

func TestUnconfiguredRuleScoresZeroAndSaysSo(t *testing.T) {
	c := config.Default()
	delete(c.Rules, "diff.deletes-files")
	s := base()
	s.Diff.Files = append(s.Diff.Files, snapshot.ChangedFile{Path: "a/old.go", Status: "D"})
	f := find(Evaluate(s, c), "diff.deletes-files")[0]
	if f.Status != score.Fired || f.Points != 0 || !strings.Contains(f.Summary, "no entry in the config") {
		t.Fatalf("unconfigured rule: %+v", f)
	}
}

func TestSensitiveGroupsCarryTheirOwnPoints(t *testing.T) {
	s := base()
	s.Diff.Files = []snapshot.ChangedFile{{Path: ".github/workflows/ci.yml", Status: "M"}, {Path: "internal/auth/x.go", Status: "M"}}
	fs := find(Evaluate(s, config.Default()), "diff.sensitive-paths")
	if len(fs) != 2 {
		t.Fatalf("want one finding per group, got %d: %+v", len(fs), fs)
	}
	total := fs[0].Points + fs[1].Points
	if total != 20+25 {
		t.Fatalf("group points not used: %d", total)
	}
}

func TestNoTestsIgnoresDocsDeletionsManifestsAndEdits(t *testing.T) {
	s := base()
	s.Diff.Files = []snapshot.ChangedFile{{Path: "README.md", Status: "M"}, {Path: "go.mod", Status: "M"}, {Path: "a/gone.go", Status: "D"}}
	if f := find(Evaluate(s, config.Default()), "diff.no-tests")[0]; f.Status != score.Clear {
		t.Fatalf("docs/manifests/deletions counted as untested code: %+v", f)
	}
	s.Diff.Files = []snapshot.ChangedFile{{Path: "a/new.go", Status: "M"}}
	if f := find(Evaluate(s, config.Default()), "diff.no-tests")[0]; f.Status != score.Fired {
		t.Fatalf("untested code not reported: %+v", f)
	}
	s.Action = snapshot.ActionEdit
	if f := find(Evaluate(s, config.Default()), "diff.no-tests")[0]; f.Status != score.Clear {
		t.Fatalf("an edit in progress was judged for tests: %+v", f)
	}
}

func TestUnknownTestsCountForLessThanFailingTests(t *testing.T) {
	c := config.Default()
	s := base()
	s.Tests = nil
	unknown := score.Compute(Evaluate(s, c), 30, 60).Score
	s.Tests = &snapshot.Tests{Status: snapshot.TestsFail, Source: "flag"}
	failing := score.Compute(Evaluate(s, c), 30, 60).Score
	s.Tests = &snapshot.Tests{Status: snapshot.TestsPass, Source: "flag"}
	passing := score.Compute(Evaluate(s, c), 30, 60).Score
	if !(passing < unknown && unknown < failing) {
		t.Fatalf("want pass < unknown < fail, got %d %d %d", passing, unknown, failing)
	}
}

func TestEvaluateIsDeterministic(t *testing.T) {
	c := config.Default()
	s := base()
	s.Diff.Files = append(s.Diff.Files, snapshot.ChangedFile{Path: ".github/x.yml"}, snapshot.ChangedFile{Path: "b/z.go"})
	a, b := Evaluate(s, c), Evaluate(s, c)
	if len(a) != len(b) {
		t.Fatal("different number of findings")
	}
	for i := range a {
		if a[i].Rule != b[i].Rule || a[i].Status != b[i].Status || a[i].Points != b[i].Points || a[i].Summary != b[i].Summary {
			t.Fatalf("finding %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestMergedOrClosedTaskIsHard(t *testing.T) {
	for _, st := range []string{"closed", "merged"} {
		s := base()
		s.Task.State = st
		f := find(Evaluate(s, config.Default()), "task.closed")[0]
		if f.Status != score.Fired || !f.Hard {
			t.Errorf("state %s: %+v", st, f)
		}
	}
	s := base()
	s.Task.State = "open"
	if f := find(Evaluate(s, config.Default()), "task.closed")[0]; f.Status != score.Clear {
		t.Errorf("open task: %+v", f)
	}
}

func TestTheTaskFileIsNotOutsideItsOwnScope(t *testing.T) {
	s := base()
	s.Task.Ref = "TASK.md"
	s.Diff.Files = append(s.Diff.Files, snapshot.ChangedFile{Path: "TASK.md", Status: "?"})
	if f := find(Evaluate(s, config.Default()), "diff.not-in-scope")[0]; f.Status != score.Clear {
		t.Fatalf("the task file was counted as out of scope: %+v", f)
	}
}
