package replay

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hidetzu/flagman/internal/config"
	"github.com/hidetzu/flagman/internal/decisionlog"
	"github.com/hidetzu/flagman/internal/rules"
	"github.com/hidetzu/flagman/internal/score"
	"github.com/hidetzu/flagman/internal/snapshot"
)

func testdata(t *testing.T, name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", name)
}

// The scenario suite: hand-labelled cases where the AI should go on alone, where a human should
// look, and where a human must decide. Under the default config every one must get its label.
func TestScenariosGetTheirLabelUnderDefaults(t *testing.T) {
	es, err := LoadScenarios(testdata(t, "scenarios"))
	if err != nil {
		t.Fatal(err)
	}
	scored, _ := Run(es, config.Default())
	seen := map[score.Verdict]int{}
	for _, s := range scored {
		seen[s.Truth.Verdict]++
		t.Run(s.ID, func(t *testing.T) {
			if s.Verdict != s.Truth.Verdict {
				t.Errorf("verdict %s (score %d), labelled %s; fired: %v", s.Verdict, s.Score, s.Truth.Verdict, names(score.FiredOnly(s.Findings)))
			}
		})
	}
	for _, v := range []score.Verdict{score.Continue, score.Review, score.Defer} {
		if seen[v] < 3 {
			t.Errorf("the suite needs several %s cases, has %d", v, seen[v])
		}
	}
	m := Measure(scored, config.Default().Costs)
	if m.FalseAutonomy.Num != 0 || m.FalseAutonomy.Den == 0 {
		t.Errorf("false autonomy on the scenario suite: %d/%d", m.FalseAutonomy.Num, m.FalseAutonomy.Den)
	}
}

func names(fs []score.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	return out
}

// The limitations: cases where a human was needed and nothing flagman collects shows it. They
// are kept so the gap is measured, not hidden. If a rule starts catching one, this fails: move
// the case into testdata/scenarios and say so in docs/SPEC.md.
func TestLimitationsAreMeasuredAsFalseAutonomy(t *testing.T) {
	es, err := LoadScenarios(testdata(t, "limitations"))
	if err != nil {
		t.Fatal(err)
	}
	scored, _ := Run(es, config.Default())
	m := Measure(scored, config.Default().Costs)
	if m.FalseAutonomy.Num != len(es) {
		t.Fatalf("want every limitation counted as a false autonomy, got %d/%d (%v)", m.FalseAutonomy.Num, m.FalseAutonomy.Den, m.Mismatches)
	}
	d := Diagnose(scored)
	if len(d.Unseen) == 0 {
		t.Fatal("at least one limitation has no rule firing at all; Diagnose must report it as unseen")
	}
}

// The improvement loop: on a history where a pattern of false autonomies repeats over time,
// a suggestion fitted on the older part must do better on the newer part, and say what it costs.
func TestSuggestImprovesOnTheHeldOutPart(t *testing.T) {
	es, err := LoadScenarios(testdata(t, "history"))
	if err != nil {
		t.Fatal(err)
	}
	cur := config.Default()
	s, err := SuggestThresholds(es, cur, 0.3)
	if err != nil {
		t.Fatal(err)
	}
	if s.TestSize == 0 || s.TrainSize+s.TestSize != len(es) {
		t.Fatalf("split: train %d test %d of %d", s.TrainSize, s.TestSize, len(es))
	}
	if s.CurrentTest.FalseAutonomy.Num == 0 {
		t.Fatal("the fixture must contain a false autonomy in the held-out part under the defaults")
	}
	if s.ProposedTest.FalseAutonomy.Num >= s.CurrentTest.FalseAutonomy.Num || s.Verdict != "better" {
		t.Fatalf("proposal did not reduce false autonomy on held-out data: %d → %d (%s)",
			s.CurrentTest.FalseAutonomy.Num, s.ProposedTest.FalseAutonomy.Num, s.Verdict)
	}
	if len(Tradeoffs(s.CurrentTest, s.ProposedTest)) == 0 {
		t.Fatal("this proposal asks humans more often; Tradeoffs must say so")
	}
}

// With no pattern to learn, the suggestion keeps the current thresholds.
func TestSuggestKeepsThresholdsWhenNothingIsWrong(t *testing.T) {
	es, err := LoadScenarios(testdata(t, "scenarios"))
	if err != nil {
		t.Fatal(err)
	}
	cur := config.Default()
	s, err := SuggestThresholds(es, cur, 0.3)
	if err != nil {
		t.Fatal(err)
	}
	if s.Review != cur.Thresholds.Review || s.Defer != cur.Thresholds.Defer {
		t.Fatalf("moved thresholds without a reason: %d/%d", s.Review, s.Defer)
	}
}

func TestMeasureDenominators(t *testing.T) {
	mk := func(id string, got, truth score.Verdict) Scored {
		return Scored{ID: id, Verdict: got, Truth: &decisionlog.Truth{Verdict: truth, Source: "label"}}
	}
	scored := []Scored{
		mk("fa", score.Continue, score.Defer),
		mk("caught", score.Defer, score.Defer),
		mk("missed", score.Continue, score.Review),
		mk("unneeded", score.Defer, score.Continue),
		mk("over", score.Review, score.Continue),
		mk("ok", score.Continue, score.Continue),
		{ID: "unlabelled", Verdict: score.Continue},
	}
	c := config.Costs{FalseAutonomy: 10, MissedEscalation: 3, UnnecessaryDeferral: 1, OverReview: 0.5}
	m := Measure(scored, c)
	check := func(name string, r Ratio, num, den int) {
		if r.Num != num || r.Den != den {
			t.Errorf("%s = %d/%d, want %d/%d", name, r.Num, r.Den, num, den)
		}
	}
	if m.Decisions != 7 || m.Labelled != 6 {
		t.Fatalf("decisions %d labelled %d", m.Decisions, m.Labelled)
	}
	check("false autonomy", m.FalseAutonomy, 1, 2)
	check("caught", m.HumanNeededCaught, 1, 2)
	check("missed escalation", m.MissedEscalation, 1, 6)
	check("unnecessary deferral", m.UnnecessaryDeferral, 1, 4)
	check("over-review", m.OverReview, 1, 3)
	check("autonomy", m.Autonomy, 3, 6)
	check("exact", m.Exact, 2, 6)
	if m.Cost != 10+3+1+0.5 {
		t.Errorf("cost %v", m.Cost)
	}
	if len(m.FalseAutonomyIDs) != 1 || m.FalseAutonomyIDs[0] != "fa" || len(m.Mismatches) != 4 {
		t.Errorf("traceability: fa=%v mismatches=%v", m.FalseAutonomyIDs, m.Mismatches)
	}
	if (Ratio{}).Value() != -1 {
		t.Error("an empty denominator must not read as 0%")
	}
}

func TestBetterRanksFalseAutonomyFirst(t *testing.T) {
	a := Metrics{FalseAutonomy: Ratio{1, 5}, Cost: 10}
	b := Metrics{FalseAutonomy: Ratio{0, 5}, Cost: 50}
	if Better(a, b) != "better" || Better(b, a) != "worse" {
		t.Fatal("fewer false autonomies must win even at a higher cost")
	}
	if Better(a, a) != "same" {
		t.Fatal("identical metrics are the same")
	}
}

func TestReproduceDetectsAChangedVerdict(t *testing.T) {
	cfg := config.Default()
	s := &snapshot.Snapshot{Action: snapshot.ActionMerge}
	fs := rules.Evaluate(s, cfg)
	r := score.Compute(fs, cfg.Thresholds.Review, cfg.Thresholds.Defer)
	good := &decisionlog.Entry{Decision: decisionlog.Line{ID: "good", At: time.Now(), Config: cfg, Snapshot: s, Result: &r}}
	forged := r
	forged.Verdict = score.Continue
	bad := &decisionlog.Entry{Decision: decisionlog.Line{ID: "bad", At: time.Now(), Config: cfg, Snapshot: s, Result: &forged}}
	bare := &decisionlog.Entry{Decision: decisionlog.Line{ID: "bare", Snapshot: s}}
	rep := Reproduce([]*decisionlog.Entry{good, bad, bare})
	if rep.Checked != 2 || rep.Reproduced != 1 || len(rep.Differ) != 1 || rep.Differ[0] != "bad" || len(rep.NoStored) != 1 {
		t.Fatalf("%+v", rep)
	}
}
