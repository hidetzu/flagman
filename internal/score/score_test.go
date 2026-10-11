package score

import "testing"

func f(rule string, st Status, pts int, hard bool) Finding {
	return Finding{Rule: rule, Status: st, Points: pts, Hard: hard}
}

func TestCompute(t *testing.T) {
	cases := []struct {
		name  string
		in    []Finding
		score int
		want  Verdict
	}{
		{"nothing fired", []Finding{f("a", Clear, 50, false)}, 0, Continue},
		{"below review", []Finding{f("a", Fired, 29, false)}, 29, Continue},
		{"at review", []Finding{f("a", Fired, 15, false), f("b", Fired, 15, false)}, 30, Review},
		{"at defer", []Finding{f("a", Fired, 60, false)}, 60, Defer},
		{"capped at 100", []Finding{f("a", Fired, 80, false), f("b", Fired, 80, false)}, 100, Defer},
		{"hard forces defer at low score", []Finding{f("a", Fired, 0, true)}, 0, Defer},
		{"not-obtained scores nothing", []Finding{f("a", NotObtained, 90, true)}, 0, Continue},
		{"off scores nothing", []Finding{f("a", Off, 90, true)}, 0, Continue},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Compute(c.in, 30, 60)
			if r.Score != c.score || r.Verdict != c.want {
				t.Fatalf("got score %d verdict %s, want %d %s", r.Score, r.Verdict, c.score, c.want)
			}
		})
	}
}

func TestComputeRecordsHardRules(t *testing.T) {
	r := Compute([]Finding{f("z", Fired, 1, true), f("a", Fired, 1, true)}, 30, 60)
	if len(r.Hard) != 2 || r.Hard[0] != "a" {
		t.Fatalf("hard rules not recorded sorted: %v", r.Hard)
	}
	if r.Thresholds.Review != 30 || r.Thresholds.Defer != 60 {
		t.Fatalf("thresholds not recorded: %+v", r.Thresholds)
	}
}

func TestRankAndParse(t *testing.T) {
	if !(Continue.Rank() < Review.Rank() && Review.Rank() < Defer.Rank()) {
		t.Fatal("verdicts are not ordered continue < review < defer")
	}
	if _, ok := Parse("maybe"); ok {
		t.Fatal("Parse accepted an unknown verdict")
	}
}
