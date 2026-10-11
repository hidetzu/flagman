package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hidetzu/flagman/internal/decisionlog"
	"github.com/hidetzu/flagman/internal/score"
	"github.com/hidetzu/flagman/internal/snapshot"
)

// Scenario is a hand-written case: a snapshot and the verdict a careful human would give.
// Scenarios are the labelled log a project starts with, before it has a history of its own
// (testdata/scenarios/).
type Scenario struct {
	Name     string             `json:"name"`
	Should   string             `json:"should"`
	Why      string             `json:"why"`
	Snapshot *snapshot.Snapshot `json:"snapshot"`
}

// LoadScenarios reads every *.json file in dir as a Scenario and returns them as labelled log
// entries, in file-name order (which is also their "time" order for SuggestThresholds).
func LoadScenarios(dir string) ([]*decisionlog.Entry, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no scenario files (*.json) in %s", dir)
	}
	sort.Strings(matches)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var out []*decisionlog.Entry
	for i, p := range matches {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var sc Scenario
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&sc); err != nil {
			return nil, fmt.Errorf("%s: %v", filepath.Base(p), err)
		}
		if sc.Snapshot == nil {
			return nil, fmt.Errorf("%s: no snapshot", filepath.Base(p))
		}
		if _, ok := score.Parse(sc.Should); !ok {
			return nil, fmt.Errorf("%s: should must be continue, review or defer", filepath.Base(p))
		}
		at := base.Add(time.Duration(i) * time.Hour)
		id := strings.TrimSuffix(filepath.Base(p), ".json")
		out = append(out, &decisionlog.Entry{
			Decision: decisionlog.Line{Kind: decisionlog.KindDecision, ID: id, At: at, Snapshot: sc.Snapshot, Note: sc.Name},
			Label:    &decisionlog.Line{Kind: decisionlog.KindLabel, ID: id, At: at, Value: sc.Should, Note: sc.Why},
		})
	}
	return out, nil
}
