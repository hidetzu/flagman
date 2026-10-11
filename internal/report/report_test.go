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
