package decisionlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hidetzu/flagman/internal/score"
	"github.com/hidetzu/flagman/internal/snapshot"
)

func dec(id string, at time.Time) Line {
	return Line{Kind: KindDecision, ID: id, At: at, Snapshot: &snapshot.Snapshot{Action: "commit"}}
}

func TestAppendReadRoundTripAndSupersede(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "log.jsonl")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, l := range []Line{
		dec("d-2", t0.Add(time.Hour)),
		dec("d-1", t0),
		{Kind: KindHuman, ID: "d-1", At: t0, Value: HumanApproved},
		{Kind: KindHuman, ID: "d-1", At: t0.Add(time.Minute), Value: HumanStopped},
		{Kind: KindOutcome, ID: "d-missing", At: t0, Value: OutcomeOK},
	} {
		if err := Append(p, l); err != nil {
			t.Fatal(err)
		}
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("not json\n{\"kind\":\"weird\",\"id\":\"x\"}\n")
	f.Close()

	rr, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Entries) != 2 || rr.Entries[0].Decision.ID != "d-1" {
		t.Fatalf("entries not sorted by time: %+v", rr.Entries)
	}
	if rr.Entries[0].Human.Value != HumanStopped {
		t.Fatalf("the later human line must supersede: %+v", rr.Entries[0].Human)
	}
	if len(rr.Malformed) != 2 || len(rr.Orphans) != 1 {
		t.Fatalf("unreadable lines must be counted: malformed=%v orphans=%v", rr.Malformed, rr.Orphans)
	}
	b, _ := os.ReadFile(p)
	if strings.Count(string(b), "\"kind\":\"human\"") != 2 {
		t.Fatal("the log must keep both human lines; superseding happens on read only")
	}
}

func TestReadMissingFileIsEmpty(t *testing.T) {
	rr, err := Read(filepath.Join(t.TempDir(), "none.jsonl"))
	if err != nil || len(rr.Entries) != 0 {
		t.Fatalf("%+v %v", rr, err)
	}
}

func TestTruthOf(t *testing.T) {
	l := func(kind, v string) *Line { return &Line{Kind: kind, Value: v} }
	cases := []struct {
		name string
		e    Entry
		want score.Verdict
		src  string
		ok   bool
	}{
		{"nothing known", Entry{}, "", "", false},
		{"label wins over everything", Entry{Label: l(KindLabel, "review"), Outcome: l(KindOutcome, OutcomeProblem), Human: l(KindHuman, HumanStopped)}, score.Review, "label", true},
		{"problem outcome means a human was needed", Entry{Outcome: l(KindOutcome, OutcomeProblem), Human: l(KindHuman, HumanApproved)}, score.Defer, "outcome", true},
		{"human changed course", Entry{Human: l(KindHuman, HumanChanged)}, score.Defer, "human", true},
		{"human stopped", Entry{Human: l(KindHuman, HumanStopped)}, score.Defer, "human", true},
		{"human approved as proposed", Entry{Human: l(KindHuman, HumanApproved), Outcome: l(KindOutcome, OutcomeOK)}, score.Continue, "human", true},
		{"ok outcome alone", Entry{Outcome: l(KindOutcome, OutcomeOK)}, score.Continue, "outcome", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := TruthOf(&c.e)
			if ok != c.ok || got.Verdict != c.want || got.Source != c.src {
				t.Fatalf("got %+v %v, want %s from %s (%v)", got, ok, c.want, c.src, c.ok)
			}
		})
	}
}

func TestFind(t *testing.T) {
	es := []*Entry{{Decision: Line{ID: "d-20260101-a"}}, {Decision: Line{ID: "d-20260101-b"}}, {Decision: Line{ID: "d-20260202-c"}}}
	if e, err := Find(es, "d-20260202"); err != nil || e.Decision.ID != "d-20260202-c" {
		t.Fatalf("unique prefix: %v %v", e, err)
	}
	if _, err := Find(es, "d-20260101"); err == nil {
		t.Fatal("an ambiguous prefix must be refused")
	}
	if _, err := Find(es, "zzz"); err == nil {
		t.Fatal("an unknown id must be refused")
	}
}
