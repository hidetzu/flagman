// Package decisionlog is the append-only record of every decision and of what happened after.
//
// One JSON object per line. Four kinds of line, all keyed by the decision id:
//
//	decision   what was observed, under which config, what was scored and recommended
//	human      what the human actually decided (approved / changed / stopped)
//	outcome    what was observed afterwards (ok / problem)
//	label      what the verdict should have been, set by a human after the fact
//
// Lines are never rewritten. A later line of the same kind for the same id supersedes the
// earlier one when read; the earlier line stays as the record of what was believed then
// (docs/adr/0005).
//
// The log stores paths, counts, quoted task lines and test summaries. It never stores file
// contents or diffs, and it never leaves the machine: nothing in this package opens a socket.
package decisionlog

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hidetzu/flagman/internal/config"
	"github.com/hidetzu/flagman/internal/score"
	"github.com/hidetzu/flagman/internal/snapshot"
)

// RulesVersion names the meaning of the rule set. It changes when a rule's meaning changes, so
// a replay can say that a stored verdict was produced by different rules.
const RulesVersion = "1"

// Kinds of line.
const (
	KindDecision = "decision"
	KindHuman    = "human"
	KindOutcome  = "outcome"
	KindLabel    = "label"
)

// Human decisions.
const (
	HumanApproved = "approved" // the human let the step go ahead as proposed
	HumanChanged  = "changed"  // the human changed what was done
	HumanStopped  = "stopped"  // the human stopped it
)

// Outcomes.
const (
	OutcomeOK      = "ok"      // nothing went wrong that a human had to step in for
	OutcomeProblem = "problem" // reverted, broke something, or a human had to step in
)

// Line is one line of the log. Fields not used by a kind are omitted.
type Line struct {
	Kind string    `json:"kind"`
	ID   string    `json:"id"`
	At   time.Time `json:"at"`

	// decision
	ToolVersion  string             `json:"tool_version,omitempty"`
	RulesVersion string             `json:"rules_version,omitempty"`
	ConfigHash   string             `json:"config_hash,omitempty"`
	Config       *config.Config     `json:"config,omitempty"`
	Snapshot     *snapshot.Snapshot `json:"snapshot,omitempty"`
	Findings     []score.Finding    `json:"findings,omitempty"`
	Result       *score.Result      `json:"result,omitempty"`

	// human / outcome / label
	Value string `json:"value,omitempty"`
	Note  string `json:"note,omitempty"`
}

// Entry is one decision with the latest of each follow-up.
type Entry struct {
	Decision Line
	Human    *Line
	Outcome  *Line
	Label    *Line
}

// NewID returns a sortable id: d-YYYYMMDD-HHMMSS-xxxx.
func NewID(now time.Time) string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return "d-" + now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// Append writes one line, creating the directory and file if needed.
func Append(path string, l Line) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReadResult is what Read found, including what it could not read.
type ReadResult struct {
	Entries []*Entry
	// Malformed counts lines that are not valid JSON or have no kind/id. They are counted,
	// never silently dropped (.claude/rules/evidence.md: an uncounted rejection is
	// indistinguishable from something that never arrived).
	Malformed []string
	// Orphans counts follow-up lines whose decision id is not in the log.
	Orphans []string
}

// Read reads a log file. A missing file is an empty log.
func Read(path string) (*ReadResult, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &ReadResult{}, nil
		}
		return nil, err
	}
	defer f.Close()
	return ReadFrom(f)
}

// ReadFrom reads a log from r.
func ReadFrom(r io.Reader) (*ReadResult, error) {
	res := &ReadResult{}
	byID := map[string]*Entry{}
	var follow []Line
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var l Line
		if err := json.Unmarshal([]byte(raw), &l); err != nil || l.Kind == "" || l.ID == "" {
			res.Malformed = append(res.Malformed, fmt.Sprintf("line %d", n))
			continue
		}
		switch l.Kind {
		case KindDecision:
			if l.Snapshot == nil {
				res.Malformed = append(res.Malformed, fmt.Sprintf("line %d (decision without a snapshot)", n))
				continue
			}
			if _, dup := byID[l.ID]; dup {
				res.Malformed = append(res.Malformed, fmt.Sprintf("line %d (decision id %s appears twice)", n, l.ID))
				continue
			}
			e := &Entry{Decision: l}
			byID[l.ID] = e
			res.Entries = append(res.Entries, e)
		case KindHuman, KindOutcome, KindLabel:
			follow = append(follow, l)
		default:
			res.Malformed = append(res.Malformed, fmt.Sprintf("line %d (unknown kind %q)", n, l.Kind))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for i := range follow {
		l := follow[i]
		e, ok := byID[l.ID]
		if !ok {
			res.Orphans = append(res.Orphans, l.ID)
			continue
		}
		switch l.Kind {
		case KindHuman:
			e.Human = &l
		case KindOutcome:
			e.Outcome = &l
		case KindLabel:
			e.Label = &l
		}
	}
	sort.SliceStable(res.Entries, func(i, j int) bool {
		return res.Entries[i].Decision.At.Before(res.Entries[j].Decision.At)
	})
	return res, nil
}

// Find resolves an id or a unique prefix of one.
func Find(entries []*Entry, id string) (*Entry, error) {
	var hit []*Entry
	for _, e := range entries {
		if e.Decision.ID == id {
			return e, nil
		}
		if strings.HasPrefix(e.Decision.ID, id) {
			hit = append(hit, e)
		}
	}
	switch len(hit) {
	case 0:
		return nil, fmt.Errorf("no decision with id %q in the log", id)
	case 1:
		return hit[0], nil
	}
	return nil, fmt.Errorf("%q matches %d decisions; give more of the id", id, len(hit))
}

// Last returns the most recent decision, or nil.
func Last(entries []*Entry) *Entry {
	if len(entries) == 0 {
		return nil
	}
	return entries[len(entries)-1]
}

// Truth is what a decision should have been, and where that came from.
type Truth struct {
	Verdict score.Verdict
	// Source is "label", "outcome" or "human". It is printed with every metric so a reader can
	// see how much of the ground truth is a convention rather than an explicit label.
	Source string
}

// TruthOf derives the ground truth for a decision, or ok=false when nothing says.
//
// The order, and the two conventions it contains, are docs/adr/0005:
//
//  1. an explicit label wins
//  2. outcome=problem                → defer   (something went wrong that a human had to handle)
//  3. human=changed or stopped       → defer   (the human's judgement changed the course)
//  4. human=approved                 → continue (the human would have let it go as proposed)
//  5. outcome=ok                     → continue
func TruthOf(e *Entry) (Truth, bool) {
	if e.Label != nil {
		if v, ok := score.Parse(e.Label.Value); ok {
			return Truth{v, "label"}, true
		}
	}
	if e.Outcome != nil && e.Outcome.Value == OutcomeProblem {
		return Truth{score.Defer, "outcome"}, true
	}
	if e.Human != nil {
		switch e.Human.Value {
		case HumanChanged, HumanStopped:
			return Truth{score.Defer, "human"}, true
		case HumanApproved:
			return Truth{score.Continue, "human"}, true
		}
	}
	if e.Outcome != nil && e.Outcome.Value == OutcomeOK {
		return Truth{score.Continue, "outcome"}, true
	}
	return Truth{}, false
}
