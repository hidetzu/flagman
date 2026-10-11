// Package score turns findings into a score and a verdict. It is pure: the same findings and
// thresholds always give the same answer, and nothing here reads the repository.
//
// The score is a deterministic sum of named findings, capped at 100. It is an ordering device
// for thresholds and replay, not a probability (docs/adr/0004).
package score

import "sort"

// Verdict is one of the three answers.
type Verdict string

// The three verdicts, in increasing need for a human.
const (
	Continue Verdict = "continue" // the AI may go on alone
	Review   Verdict = "review"   // the AI may go on; a human should look
	Defer    Verdict = "defer"    // a human decides before anything else happens
)

// Rank orders verdicts: Continue < Review < Defer.
func (v Verdict) Rank() int {
	switch v {
	case Continue:
		return 0
	case Review:
		return 1
	case Defer:
		return 2
	}
	return -1
}

// Parse accepts the three verdict words.
func Parse(s string) (Verdict, bool) {
	switch Verdict(s) {
	case Continue, Review, Defer:
		return Verdict(s), true
	}
	return "", false
}

// Status says what a rule found.
type Status string

// Finding statuses.
const (
	// Fired: the condition the rule looks for was observed.
	Fired Status = "fired"
	// Clear: the rule ran and the condition was not observed.
	Clear Status = "clear"
	// NotObtained: the rule could not run because its evidence was not obtained.
	NotObtained Status = "not-obtained"
	// Off: the project switched the rule off in its config.
	Off Status = "off"
)

// Finding is one named piece of evidence and what it contributes.
type Finding struct {
	Rule    string `json:"rule"`
	Status  Status `json:"status"`
	Points  int    `json:"points"`
	Hard    bool   `json:"hard,omitempty"`
	Summary string `json:"summary"`
	// Evidence is where it was seen: paths, "file:line", counts with their denominators.
	Evidence []string `json:"evidence,omitempty"`
	// Why is the rule's grounds, copied from the config so the log carries it.
	Why string `json:"why,omitempty"`
}

// Result is the scored decision.
type Result struct {
	Score   int     `json:"score"`
	Verdict Verdict `json:"verdict"`
	// Hard lists the rules that forced defer, if any.
	Hard []string `json:"hard,omitempty"`
	// Review and Defer are the thresholds the verdict was derived with.
	Thresholds struct {
		Review int `json:"review"`
		Defer  int `json:"defer"`
	} `json:"thresholds"`
}

// Compute sums the points of fired findings, caps at 100, and applies the thresholds.
// Any fired hard finding forces Defer whatever the sum.
func Compute(findings []Finding, review, defer_ int) Result {
	var r Result
	r.Thresholds.Review = review
	r.Thresholds.Defer = defer_
	sum := 0
	for _, f := range findings {
		if f.Status != Fired {
			continue
		}
		sum += f.Points
		if f.Hard {
			r.Hard = append(r.Hard, f.Rule)
		}
	}
	if sum > 100 {
		sum = 100
	}
	r.Score = sum
	sort.Strings(r.Hard)
	switch {
	case len(r.Hard) > 0 || sum >= defer_:
		r.Verdict = Defer
	case sum >= review:
		r.Verdict = Review
	default:
		r.Verdict = Continue
	}
	return r
}

// Fired returns only the findings that fired, highest points first (hard first), for display.
func FiredOnly(findings []Finding) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Status == Fired {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Hard != out[j].Hard {
			return out[i].Hard
		}
		return out[i].Points > out[j].Points
	})
	return out
}

// NotObtainedOnly returns the findings whose evidence was not obtained.
func NotObtainedOnly(findings []Finding) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Status == NotObtained {
			out = append(out, f)
		}
	}
	return out
}
