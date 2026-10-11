// Package replay re-scores recorded decisions and measures how good the verdicts were.
//
// It answers three questions (docs/adr/0006):
//
//  1. Reproducibility — scored again under the config stored with it, does every decision give
//     the verdict and score it gave at the time?
//  2. Quality — against the ground truth (decisionlog.TruthOf), how many false autonomies,
//     missed escalations, unnecessary deferrals; how much was left to the AI alone?
//  3. Comparison — under another config, is it better? False autonomy is compared first;
//     cost only breaks ties.
//
// Everything here is arithmetic over the log. Nothing is estimated or extrapolated, and every
// rate is printed with its numerator and denominator.
package replay

import (
	"fmt"
	"sort"

	"github.com/hidetzu/flagman/internal/config"
	"github.com/hidetzu/flagman/internal/decisionlog"
	"github.com/hidetzu/flagman/internal/rules"
	"github.com/hidetzu/flagman/internal/score"
)

// Scored is one decision scored under some config.
type Scored struct {
	ID       string
	Verdict  score.Verdict
	Score    int
	Findings []score.Finding
	Truth    *decisionlog.Truth
	Stored   *score.Result // what was recorded at the time, if any
}

// Run scores every entry under cfg. A nil cfg means "the config stored with each decision"
// (and entries without one are skipped and counted in skipped).
func Run(entries []*decisionlog.Entry, cfg *config.Config) (out []Scored, skipped []string) {
	for _, e := range entries {
		c := cfg
		if c == nil {
			c = e.Decision.Config
		}
		if c == nil {
			skipped = append(skipped, e.Decision.ID)
			continue
		}
		fs := rules.Evaluate(e.Decision.Snapshot, c)
		r := score.Compute(fs, c.Thresholds.Review, c.Thresholds.Defer)
		s := Scored{ID: e.Decision.ID, Verdict: r.Verdict, Score: r.Score, Findings: fs, Stored: e.Decision.Result}
		if t, ok := decisionlog.TruthOf(e); ok {
			s.Truth = &t
		}
		out = append(out, s)
	}
	return out, skipped
}

// Ratio is a count over a denominator. Rates are never printed without both.
type Ratio struct {
	Num, Den int
}

// Value is Num/Den, or -1 when Den is zero (printed as "n/a", never as 0%).
func (r Ratio) Value() float64 {
	if r.Den == 0 {
		return -1
	}
	return float64(r.Num) / float64(r.Den)
}

// Metrics summarise a set of scored decisions against their ground truth.
type Metrics struct {
	Decisions int
	Labelled  int
	// BySource counts labelled decisions by where the truth came from.
	BySource map[string]int
	// Confusion[pred][truth], indexed by Verdict.Rank().
	Confusion [3][3]int

	// FalseAutonomy: predicted continue, should have been defer. Over truth=defer.
	FalseAutonomy Ratio
	// MissedEscalation: predicted below the truth, other than false autonomy. Over all labelled.
	MissedEscalation Ratio
	// HumanNeededCaught: truth=defer predicted defer. Over truth=defer.
	HumanNeededCaught Ratio
	// UnnecessaryDeferral: predicted defer, truth below defer. Over truth below defer.
	UnnecessaryDeferral Ratio
	// OverReview: predicted review, truth continue. Over truth continue.
	OverReview Ratio
	// Autonomy: predicted continue. Over labelled. "How much was left to the AI alone."
	Autonomy Ratio
	// Exact: predicted equals truth. Over labelled.
	Exact Ratio
	// Cost is the weighted sum of mistakes (config.Costs); CostPer is per labelled decision.
	Cost    float64
	CostPer float64
	// FalseAutonomyIDs lists the decisions that were false autonomies.
	FalseAutonomyIDs []string
	// Mismatches lists every decision whose verdict differs from its truth, as
	// "id: given → truth (source)", so each number above can be traced to decisions.
	Mismatches []string
}

// Measure computes Metrics.
func Measure(scored []Scored, costs config.Costs) Metrics {
	m := Metrics{Decisions: len(scored), BySource: map[string]int{}}
	needDefer, belowDefer, truthContinue := 0, 0, 0
	for _, s := range scored {
		if s.Truth == nil {
			continue
		}
		m.Labelled++
		m.BySource[s.Truth.Source]++
		p, t := s.Verdict.Rank(), s.Truth.Verdict.Rank()
		m.Confusion[p][t]++
		if t == 2 {
			needDefer++
		} else {
			belowDefer++
		}
		if t == 0 {
			truthContinue++
		}
		if p == 0 {
			m.Autonomy.Num++
		}
		if p == t {
			m.Exact.Num++
		} else {
			m.Mismatches = append(m.Mismatches, fmt.Sprintf("%s: %s → %s (%s)", s.ID, s.Verdict, s.Truth.Verdict, s.Truth.Source))
		}
		switch {
		case p == 0 && t == 2:
			m.FalseAutonomy.Num++
			m.FalseAutonomyIDs = append(m.FalseAutonomyIDs, s.ID)
			m.Cost += costs.FalseAutonomy
		case p < t:
			m.MissedEscalation.Num++
			m.Cost += costs.MissedEscalation
		case p == 2 && t < 2:
			m.UnnecessaryDeferral.Num++
			m.Cost += costs.UnnecessaryDeferral
		case p == 1 && t == 0:
			m.OverReview.Num++
			m.Cost += costs.OverReview
		}
		if p == 2 && t == 2 {
			m.HumanNeededCaught.Num++
		}
	}
	m.FalseAutonomy.Den = needDefer
	m.HumanNeededCaught.Den = needDefer
	m.MissedEscalation.Den = m.Labelled
	m.UnnecessaryDeferral.Den = belowDefer
	m.OverReview.Den = truthContinue
	m.Autonomy.Den = m.Labelled
	m.Exact.Den = m.Labelled
	if m.Labelled > 0 {
		m.CostPer = m.Cost / float64(m.Labelled)
	}
	return m
}

// Reproduction is the result of re-scoring under each decision's stored config.
type Reproduction struct {
	Checked    int
	Reproduced int
	// Differ lists decisions whose verdict or score changed: the rules changed since, or the
	// log was edited.
	Differ []string
	// NoStored lists decisions recorded without a result or config (hand-written fixtures).
	NoStored []string
}

// Reproduce re-scores every decision under its own stored config and compares.
func Reproduce(entries []*decisionlog.Entry) Reproduction {
	var r Reproduction
	for _, e := range entries {
		if e.Decision.Config == nil || e.Decision.Result == nil {
			r.NoStored = append(r.NoStored, e.Decision.ID)
			continue
		}
		r.Checked++
		fs := rules.Evaluate(e.Decision.Snapshot, e.Decision.Config)
		got := score.Compute(fs, e.Decision.Config.Thresholds.Review, e.Decision.Config.Thresholds.Defer)
		if got.Verdict == e.Decision.Result.Verdict && got.Score == e.Decision.Result.Score {
			r.Reproduced++
		} else {
			r.Differ = append(r.Differ, e.Decision.ID)
		}
	}
	return r
}

// Change is one decision whose verdict differs between two configs.
type Change struct {
	ID       string
	From, To score.Verdict
	Truth    *decisionlog.Truth
}

// Better says how B compares with A. False autonomy decides first; then missed escalation;
// then cost. "same" means no difference on any of the three.
func Better(a, b Metrics) string {
	switch {
	case b.FalseAutonomy.Num < a.FalseAutonomy.Num:
		return "better"
	case b.FalseAutonomy.Num > a.FalseAutonomy.Num:
		return "worse"
	case b.MissedEscalation.Num < a.MissedEscalation.Num:
		return "better"
	case b.MissedEscalation.Num > a.MissedEscalation.Num:
		return "worse"
	case b.Cost < a.Cost:
		return "better"
	case b.Cost > a.Cost:
		return "worse"
	}
	return "same"
}

// Tradeoffs lists every measure on which B is worse than A, so a "better" never hides what it
// costs: more humans asked, less left to the AI, fewer exact verdicts.
func Tradeoffs(a, b Metrics) []string {
	var out []string
	add := func(name string, ra, rb Ratio, higherIsWorse bool) {
		worse := rb.Num > ra.Num
		if !higherIsWorse {
			worse = rb.Num < ra.Num
		}
		if worse {
			out = append(out, fmt.Sprintf("%s %d/%d → %d/%d", name, ra.Num, ra.Den, rb.Num, rb.Den))
		}
	}
	add("unnecessary deferral", a.UnnecessaryDeferral, b.UnnecessaryDeferral, true)
	add("over-review", a.OverReview, b.OverReview, true)
	add("left to the AI alone", a.Autonomy, b.Autonomy, false)
	add("exact verdict", a.Exact, b.Exact, false)
	add("false autonomy", a.FalseAutonomy, b.FalseAutonomy, true)
	add("missed escalation", a.MissedEscalation, b.MissedEscalation, true)
	return out
}

// Diff lists decisions whose verdict differs between two scorings of the same entries.
func Diff(a, b []Scored) []Change {
	byID := map[string]Scored{}
	for _, s := range b {
		byID[s.ID] = s
	}
	var out []Change
	for _, s := range a {
		o, ok := byID[s.ID]
		if !ok || o.Verdict == s.Verdict {
			continue
		}
		out = append(out, Change{ID: s.ID, From: s.Verdict, To: o.Verdict, Truth: s.Truth})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
