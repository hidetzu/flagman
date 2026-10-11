package replay

import (
	"fmt"
	"sort"

	"github.com/hidetzu/flagman/internal/config"
	"github.com/hidetzu/flagman/internal/decisionlog"
	"github.com/hidetzu/flagman/internal/score"
)

// MinLabelled is the number of labelled decisions below which a suggestion is printed with a
// warning that it rests on too little. It is a parameter, not a statistical threshold.
const MinLabelled = 20

// Suggestion is a proposed threshold pair with what it would have done, measured on a part of
// the log it was not fitted on.
type Suggestion struct {
	Review, Defer int
	// Train and Test are the chronological split: fitted on Train, judged on Test.
	TrainSize, TestSize         int
	CurrentTrain, ProposedTrain Metrics
	CurrentTest, ProposedTest   Metrics
	Verdict                     string // Better(CurrentTest, ProposedTest)
	TooLittleData               bool
}

// SuggestThresholds searches threshold pairs on the older part of the labelled log and judges
// the best one on the newer part (docs/adr/0006). It never changes rule points: weights carry
// reasons, and a search over them on a personal log would fit noise.
//
// The objective is lexicographic: fewest false autonomies, then fewest missed escalations,
// then lowest cost. Ties prefer the pair closest to the current one, so a suggestion moves
// only as far as the data asks.
func SuggestThresholds(entries []*decisionlog.Entry, cur *config.Config, testShare float64) (*Suggestion, error) {
	var labelled []*decisionlog.Entry
	for _, e := range entries {
		if _, ok := decisionlog.TruthOf(e); ok {
			labelled = append(labelled, e)
		}
	}
	if len(labelled) < 2 {
		return nil, fmt.Errorf("%d labelled decision(s) in the log; at least 2 are needed to fit on one part and judge on another", len(labelled))
	}
	nTest := int(float64(len(labelled))*testShare + 0.5)
	if nTest < 1 {
		nTest = 1
	}
	if nTest >= len(labelled) {
		nTest = len(labelled) - 1
	}
	train, test := labelled[:len(labelled)-nTest], labelled[len(labelled)-nTest:]

	// Findings do not depend on thresholds, so score once and re-threshold.
	trainScored, _ := Run(train, cur)
	testScored, _ := Run(test, cur)

	best := struct {
		r, d int
		m    Metrics
		dist int
	}{r: cur.Thresholds.Review, d: cur.Thresholds.Defer}
	best.m = Measure(trainScored, cur.Costs)
	for r := 0; r <= 100; r += 5 {
		for d := r; d <= 100; d += 5 {
			m := Measure(rethreshold(trainScored, r, d), cur.Costs)
			dist := abs(r-cur.Thresholds.Review) + abs(d-cur.Thresholds.Defer)
			b := Better(best.m, m)
			if b == "better" || (b == "same" && dist < best.dist) {
				best.r, best.d, best.m, best.dist = r, d, m, dist
			}
		}
	}
	s := &Suggestion{
		Review: best.r, Defer: best.d,
		TrainSize: len(train), TestSize: len(test),
		CurrentTrain:  Measure(trainScored, cur.Costs),
		ProposedTrain: best.m,
		CurrentTest:   Measure(testScored, cur.Costs),
		ProposedTest:  Measure(rethreshold(testScored, best.r, best.d), cur.Costs),
		TooLittleData: len(labelled) < MinLabelled,
	}
	s.Verdict = Better(s.CurrentTest, s.ProposedTest)
	return s, nil
}

func rethreshold(in []Scored, review, deferAt int) []Scored {
	out := make([]Scored, len(in))
	for i, s := range in {
		r := score.Compute(s.Findings, review, deferAt)
		s.Verdict, s.Score = r.Verdict, r.Score
		out[i] = s
	}
	return out
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// RuleStat says how one rule behaved across labelled decisions.
type RuleStat struct {
	Rule string
	// Fired counts decisions where the rule fired, split by the truth.
	FiredWhenDefer, FiredWhenReview, FiredWhenContinue int
	// InFalseAutonomy counts false-autonomy decisions where the rule fired but the total
	// stayed below the review threshold: raising this rule would have caught them.
	InFalseAutonomy int
	// NotObtained counts decisions where the rule could not run.
	NotObtained int
}

// Diagnosis is the per-rule view plus the false autonomies no rule saw.
type Diagnosis struct {
	Rules []RuleStat
	// Unseen lists false autonomies where no rule fired at all: the evidence that would have
	// caught them is not something flagman collects. These need a new rule or an external
	// finding, not a weight change.
	Unseen []string
	Truth  map[score.Verdict]int
}

// Diagnose counts, per rule, where it fired relative to the truth.
func Diagnose(scored []Scored) Diagnosis {
	stats := map[string]*RuleStat{}
	get := func(n string) *RuleStat {
		if s, ok := stats[n]; ok {
			return s
		}
		s := &RuleStat{Rule: n}
		stats[n] = s
		return s
	}
	d := Diagnosis{Truth: map[score.Verdict]int{}}
	for _, s := range scored {
		if s.Truth == nil {
			continue
		}
		d.Truth[s.Truth.Verdict]++
		falseAuto := s.Verdict == score.Continue && s.Truth.Verdict == score.Defer
		anyFired := false
		seen := map[string]bool{}
		for _, f := range s.Findings {
			st := get(f.Rule)
			if f.Status == score.NotObtained && !seen[f.Rule+"?"] {
				seen[f.Rule+"?"] = true
				st.NotObtained++
			}
			if f.Status != score.Fired || seen[f.Rule] {
				continue
			}
			seen[f.Rule] = true
			anyFired = true
			switch s.Truth.Verdict {
			case score.Defer:
				st.FiredWhenDefer++
			case score.Review:
				st.FiredWhenReview++
			default:
				st.FiredWhenContinue++
			}
			if falseAuto {
				st.InFalseAutonomy++
			}
		}
		if falseAuto && !anyFired {
			d.Unseen = append(d.Unseen, s.ID)
		}
	}
	for _, st := range stats {
		d.Rules = append(d.Rules, *st)
	}
	sort.Slice(d.Rules, func(i, j int) bool {
		a, b := d.Rules[i], d.Rules[j]
		if a.InFalseAutonomy != b.InFalseAutonomy {
			return a.InFalseAutonomy > b.InFalseAutonomy
		}
		return a.Rule < b.Rule
	})
	return d
}
