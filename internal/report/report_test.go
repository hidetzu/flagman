package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hidetzu/flagman/internal/config"
	"github.com/hidetzu/flagman/internal/decisionlog"
	"github.com/hidetzu/flagman/internal/rules"
	"github.com/hidetzu/flagman/internal/score"
	"github.com/hidetzu/flagman/internal/snapshot"
)

// A rule that could not run must be named under "Not obtained", even when no collector recorded
// a missing source (no task was given, so the task rules had nothing to read).
func TestNotObtainedIsNeverAnEmptyHeading(t *testing.T) {
	c := config.Default()
	s := &snapshot.Snapshot{Action: snapshot.ActionCommit, Repo: &snapshot.Repo{Branch: "f", DefaultBranch: "main"},
		Diff: &snapshot.Diff{}, History: &snapshot.History{}, Tests: &snapshot.Tests{Status: "pass", Source: "flag"}, ADRs: &snapshot.ADRs{}}
	fs := rules.Evaluate(s, c)
	r := score.Compute(fs, c.Thresholds.Review, c.Thresholds.Defer)
	var b bytes.Buffer
	Check(&b, &decisionlog.Line{ID: "d-x", Snapshot: s, Findings: fs, Result: &r}, true, false)
	out := b.String()
	i := strings.Index(out, "Not obtained")
	if i < 0 {
		t.Fatalf("task rules could not run and nothing says so:\n%s", out)
	}
	if !strings.Contains(out[i:], "task.closed") {
		t.Fatalf("the heading must name the rules that could not run:\n%s", out[i:])
	}
}

func entry(id string, v score.Verdict, human, outcome string) *decisionlog.Entry {
	e := &decisionlog.Entry{Decision: decisionlog.Line{ID: id, Snapshot: &snapshot.Snapshot{Action: "commit"}, Result: &score.Result{Verdict: v}}}
	if human != "" {
		e.Human = &decisionlog.Line{Kind: decisionlog.KindHuman, Value: human}
	}
	if outcome != "" {
		e.Outcome = &decisionlog.Line{Kind: decisionlog.KindOutcome, Value: outcome}
	}
	return e
}

func TestPendingListsOnlyDecisionsWithoutTruthNewestFirst(t *testing.T) {
	es := []*decisionlog.Entry{
		entry("d-1-old-continue", score.Continue, "", ""),
		entry("d-2-recorded", score.Defer, decisionlog.HumanApproved, ""),
		entry("d-3-ok", score.Continue, "", decisionlog.OutcomeOK),
		entry("d-4-new-defer", score.Defer, "", ""),
	}
	var b bytes.Buffer
	Pending(&b, es, 0)
	out := b.String()
	if !strings.Contains(out, "2 of 4 decisions have no known truth yet") {
		t.Fatalf("the count must carry its denominator:\n%s", out)
	}
	if strings.Contains(out, "d-2-recorded") || strings.Contains(out, "d-3-ok") {
		t.Fatalf("decisions with a truth must not be listed:\n%s", out)
	}
	if strings.Index(out, "d-4-new-defer") > strings.Index(out, "d-1-old-continue") {
		t.Fatalf("newest first:\n%s", out)
	}
	if !strings.Contains(out, "continue decisions need an outcome") {
		t.Fatalf("an unrecorded continue must be called out (false autonomy is counted from them):\n%s", out)
	}
	b.Reset()
	Pending(&b, es, 1)
	if strings.Contains(b.String(), "d-1-old-continue") || !strings.Contains(b.String(), "1 older not shown") {
		t.Fatalf("limit must show the newest and say how many are hidden:\n%s", b.String())
	}
}

func TestHookMessageNamesTheDecisionAndHowToRecordIt(t *testing.T) {
	l := &decisionlog.Line{ID: "d-42", Result: &score.Result{Verdict: score.Defer, Score: 80},
		Findings: []score.Finding{{Rule: "tests.failing", Status: score.Fired, Points: 40, Summary: "the tests fail"}}}
	m := HookMessage(l)
	for _, want := range []string{"DEFER", "d-42", "tests.failing", "flagman human d-42"} {
		if !strings.Contains(m, want) {
			t.Errorf("defer message lacks %q:\n%s", want, m)
		}
	}
	l.Result.Verdict = score.Review
	if m := HookMessage(l); !strings.Contains(m, "flagman outcome d-42") {
		t.Errorf("review message must say how to record the outcome:\n%s", m)
	}
}
