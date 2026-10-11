// Package report holds every sentence a human reads (CLAUDE.md §4). Commands compose data;
// this package words it. Nothing here decides anything.
package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/hidetzu/flagman/internal/decisionlog"
	"github.com/hidetzu/flagman/internal/replay"
	"github.com/hidetzu/flagman/internal/score"
	"github.com/hidetzu/flagman/internal/snapshot"
)

// Headline is the one line a human reads first.
func Headline(v score.Verdict) string {
	switch v {
	case score.Continue:
		return "CONTINUE — the AI may go on alone"
	case score.Review:
		return "REVIEW — the AI may go on; a human should look at this"
	case score.Defer:
		return "DEFER — a human decides before this step"
	}
	return string(v)
}

// Check prints the result of `flagman check`.
func Check(w io.Writer, l *decisionlog.Line, logged bool, verbose bool) {
	r := l.Result
	s := l.Snapshot
	fmt.Fprintf(w, "flagman: %s\n", Headline(r.Verdict))
	hard := ""
	if len(r.Hard) > 0 {
		hard = "; forced by " + strings.Join(r.Hard, ", ")
	}
	fmt.Fprintf(w, "  score %d/100 (review at %d, defer at %d%s)\n", r.Score, r.Thresholds.Review, r.Thresholds.Defer, hard)
	ctx := []string{"action=" + s.Action}
	if s.Repo != nil {
		ctx = append(ctx, "branch="+s.Repo.Branch)
	}
	if s.Diff != nil {
		ctx = append(ctx, fmt.Sprintf("base=%s files=%d +%d −%d", s.Diff.Base, len(s.Diff.Files), s.Diff.Insertions, s.Diff.Deletions))
	}
	if s.Task != nil {
		ctx = append(ctx, "task="+s.Task.Ref)
	}
	fmt.Fprintf(w, "  %s\n", strings.Join(ctx, "  "))

	fired := score.FiredOnly(l.Findings)
	if len(fired) > 0 {
		fmt.Fprintln(w, "\nWhy")
		for _, f := range fired {
			mark := fmt.Sprintf("+%-3d", f.Points)
			if f.Hard {
				mark = "HARD"
			}
			fmt.Fprintf(w, "  %s %s — %s\n", mark, f.Rule, f.Summary)
			for _, e := range f.Evidence {
				fmt.Fprintf(w, "         %s\n", e)
			}
			if verbose && f.Why != "" {
				fmt.Fprintf(w, "         grounds: %s\n", f.Why)
			}
		}
	} else {
		fmt.Fprintln(w, "\nWhy\n  no rule fired")
	}
	if no := score.NotObtainedOnly(l.Findings); len(no) > 0 || len(s.Missing) > 0 {
		fmt.Fprintln(w, "\nNot obtained (unknown is not the same as fine)")
		seen := map[string]bool{}
		for _, m := range s.Missing {
			if !seen[m.Source] {
				seen[m.Source] = true
				fmt.Fprintf(w, "  ? %s — %s\n", m.Source, m.Reason)
			}
		}
		// Rules that could not run, grouped by the reason they give.
		var order []string
		byWhy := map[string][]string{}
		for _, f := range no {
			if _, ok := byWhy[f.Summary]; !ok {
				order = append(order, f.Summary)
			}
			byWhy[f.Summary] = append(byWhy[f.Summary], f.Rule)
		}
		for _, why := range order {
			fmt.Fprintf(w, "  ? %s — %s\n", strings.Join(byWhy[why], ", "), why)
		}
	}
	if verbose {
		fmt.Fprintln(w, "\nClear")
		for _, f := range l.Findings {
			if f.Status == score.Clear || f.Status == score.Off {
				fmt.Fprintf(w, "  %-5s %s — %s\n", f.Status, f.Rule, f.Summary)
			}
		}
	}
	fmt.Fprintln(w)
	if logged {
		fmt.Fprintf(w, "decision %s  (config %s, rules v%s)\n", l.ID, l.ConfigHash, l.RulesVersion)
		switch r.Verdict {
		case score.Defer:
			fmt.Fprintf(w, "next: ask the human, then record what they decided:\n  flagman human %s approved|changed|stopped\n", l.ID)
		case score.Review:
			fmt.Fprintf(w, "next: go on, and point the human at the reasons above. Afterwards:\n  flagman outcome %s ok|problem\n", l.ID)
		default:
			fmt.Fprintf(w, "next: go on. If it later turns out a human was needed:\n  flagman outcome %s problem\n", l.ID)
		}
	} else {
		fmt.Fprintln(w, "not logged (--no-log)")
	}
}

// Log prints one line per decision.
func Log(w io.Writer, entries []*decisionlog.Entry, rr *decisionlog.ReadResult) {
	if len(entries) == 0 {
		fmt.Fprintln(w, "the log has no decisions yet")
	}
	for _, e := range entries {
		d := e.Decision
		v, sc := "-", "-"
		if d.Result != nil {
			v, sc = string(d.Result.Verdict), fmt.Sprint(d.Result.Score)
		}
		h, o, t := "-", "-", "-"
		if e.Human != nil {
			h = e.Human.Value
		}
		if e.Outcome != nil {
			o = e.Outcome.Value
		}
		if tr, ok := decisionlog.TruthOf(e); ok {
			t = string(tr.Verdict) + " (" + tr.Source + ")"
		}
		fmt.Fprintf(w, "%s  %-8s %-8s score=%-3s human=%-8s outcome=%-7s truth=%s\n",
			d.ID, d.Snapshot.Action, v, sc, h, o, t)
	}
	readNotes(w, rr)
}

func readNotes(w io.Writer, rr *decisionlog.ReadResult) {
	if rr == nil {
		return
	}
	if len(rr.Malformed) > 0 {
		fmt.Fprintf(w, "! %d line(s) of the log could not be read and were left out: %s\n", len(rr.Malformed), strings.Join(rr.Malformed, ", "))
	}
	if len(rr.Orphans) > 0 {
		fmt.Fprintf(w, "! %d follow-up line(s) name a decision that is not in the log: %s\n", len(rr.Orphans), strings.Join(rr.Orphans, ", "))
	}
}

// Show prints everything recorded about one decision.
func Show(w io.Writer, e *decisionlog.Entry, reproduced *bool) {
	d := e.Decision
	fmt.Fprintf(w, "decision %s at %s\n", d.ID, d.At.Format("2006-01-02 15:04:05Z07:00"))
	fmt.Fprintf(w, "  tool %s, rules v%s, config %s\n", d.ToolVersion, d.RulesVersion, d.ConfigHash)
	if d.Snapshot.Intent != "" {
		fmt.Fprintf(w, "  intent (the caller's words, not evidence): %s\n", d.Snapshot.Intent)
	}
	if d.Result != nil {
		Check(w, &d, true, true)
	}
	if reproduced != nil {
		if *reproduced {
			fmt.Fprintln(w, "reproduced: scoring the stored snapshot under the stored config gives the same verdict and score")
		} else {
			fmt.Fprintln(w, "NOT reproduced: scoring the stored snapshot under the stored config gives a different answer (the rules changed since, or the log was edited)")
		}
	}
	for _, l := range []*decisionlog.Line{e.Human, e.Outcome, e.Label} {
		if l != nil {
			note := ""
			if l.Note != "" {
				note = " — " + l.Note
			}
			fmt.Fprintf(w, "%s: %s at %s%s\n", l.Kind, l.Value, l.At.Format("2006-01-02 15:04"), note)
		}
	}
	if t, ok := decisionlog.TruthOf(e); ok {
		fmt.Fprintf(w, "truth: %s (from %s)\n", t.Verdict, t.Source)
	} else {
		fmt.Fprintln(w, "truth: not known yet (no human decision, outcome or label recorded)")
	}
}

func pct(r replay.Ratio) string {
	if r.Den == 0 {
		return fmt.Sprintf("%d/0 (n/a)", r.Num)
	}
	return fmt.Sprintf("%d/%d (%.0f%%)", r.Num, r.Den, 100*r.Value())
}

// Metrics prints a metrics block.
func Metrics(w io.Writer, title string, m replay.Metrics) {
	fmt.Fprintf(w, "%s\n", title)
	src := []string{}
	keys := make([]string, 0, len(m.BySource))
	for k := range m.BySource {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		src = append(src, fmt.Sprintf("%s %d", k, m.BySource[k]))
	}
	fmt.Fprintf(w, "  decisions %d, with a known truth %d (%s)\n", m.Decisions, m.Labelled, strings.Join(src, ", "))
	fmt.Fprintf(w, "  false autonomy        %s   continued alone where a human was needed (over: human needed)\n", pct(m.FalseAutonomy))
	fmt.Fprintf(w, "  human needed, caught  %s   (over: human needed)\n", pct(m.HumanNeededCaught))
	fmt.Fprintf(w, "  missed escalation     %s   verdict one step too low, other than the above (over: all known)\n", pct(m.MissedEscalation))
	fmt.Fprintf(w, "  unnecessary deferral  %s   stopped for a human who was not needed (over: human not needed)\n", pct(m.UnnecessaryDeferral))
	fmt.Fprintf(w, "  over-review           %s   asked for a look where none was needed (over: truth continue)\n", pct(m.OverReview))
	fmt.Fprintf(w, "  left to the AI alone  %s   (over: all known)\n", pct(m.Autonomy))
	fmt.Fprintf(w, "  exact verdict         %s   (over: all known)\n", pct(m.Exact))
	fmt.Fprintf(w, "  weighted cost         %.1f (%.2f per decision)\n", m.Cost, m.CostPer)
	fmt.Fprintln(w, "  confusion (rows: verdict given, columns: truth)")
	fmt.Fprintln(w, "              continue  review   defer")
	names := []string{"continue", "review", "defer"}
	for p := 0; p < 3; p++ {
		fmt.Fprintf(w, "    %-9s %8d %7d %7d\n", names[p], m.Confusion[p][0], m.Confusion[p][1], m.Confusion[p][2])
	}
	if len(m.FalseAutonomyIDs) > 0 {
		fmt.Fprintf(w, "  false autonomies: %s\n", strings.Join(m.FalseAutonomyIDs, ", "))
	}
	if len(m.Mismatches) > 0 {
		fmt.Fprintln(w, "  verdict differs from truth (given → truth):")
		for _, x := range m.Mismatches {
			fmt.Fprintf(w, "    %s\n", x)
		}
	}
}

// Reproduction prints the reproducibility check.
func Reproduction(w io.Writer, r replay.Reproduction) {
	if r.Checked == 0 {
		fmt.Fprintf(w, "reproducibility: nothing to check (%d decision(s) carry no stored config and result)\n", len(r.NoStored))
		return
	}
	fmt.Fprintf(w, "reproducibility: %d/%d decisions give the same verdict and score when re-scored under their stored config\n", r.Reproduced, r.Checked)
	if len(r.Differ) > 0 {
		fmt.Fprintf(w, "  differ: %s\n", strings.Join(r.Differ, ", "))
	}
	if len(r.NoStored) > 0 {
		fmt.Fprintf(w, "  %d decision(s) carry no stored config and result and were not checked\n", len(r.NoStored))
	}
}

// Compare prints two metric blocks and the verdict changes between them.
func Compare(w io.Writer, nameA, nameB string, a, b replay.Metrics, changes []replay.Change) {
	Metrics(w, "A: "+nameA, a)
	fmt.Fprintln(w)
	Metrics(w, "B: "+nameB, b)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "B is %s than A", replay.Better(a, b))
	if replay.Better(a, b) == "same" {
		fmt.Fprint(w, " as")
	}
	fmt.Fprintln(w, " (judged on false autonomy first, then missed escalation, then cost)")
	tradeoffs(w, a, b)
	if len(changes) == 0 {
		fmt.Fprintln(w, "no decision changes verdict")
		return
	}
	fmt.Fprintf(w, "%d decision(s) change verdict:\n", len(changes))
	for _, c := range changes {
		t := "truth unknown"
		if c.Truth != nil {
			t = "truth " + string(c.Truth.Verdict)
		}
		fmt.Fprintf(w, "  %s  %s → %s  (%s)\n", c.ID, c.From, c.To, t)
	}
}

// Suggest prints a threshold suggestion and the rule diagnosis.
func Suggest(w io.Writer, s *replay.Suggestion, cur [2]int, d replay.Diagnosis) {
	if s.TooLittleData {
		fmt.Fprintf(w, "! fewer than %d decisions with a known truth: treat this as a hint, not a measurement\n\n", replay.MinLabelled)
	}
	fmt.Fprintf(w, "thresholds: current review=%d defer=%d, proposed review=%d defer=%d\n", cur[0], cur[1], s.Review, s.Defer)
	fmt.Fprintf(w, "fitted on the older %d decision(s), judged on the newer %d\n\n", s.TrainSize, s.TestSize)
	Metrics(w, "judged part, current thresholds", s.CurrentTest)
	fmt.Fprintln(w)
	Metrics(w, "judged part, proposed thresholds", s.ProposedTest)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "on the part it was not fitted on, the proposal is %s\n", s.Verdict)
	tradeoffs(w, s.CurrentTest, s.ProposedTest)
	if s.Verdict != "better" {
		fmt.Fprintln(w, "→ keep the current thresholds")
	}
	fmt.Fprintln(w, "\nrules (fired, split by the truth)")
	fmt.Fprintln(w, "  rule                          defer review continue  in-false-autonomy  not-obtained")
	for _, r := range d.Rules {
		if r.FiredWhenDefer+r.FiredWhenReview+r.FiredWhenContinue+r.NotObtained == 0 {
			continue
		}
		fmt.Fprintf(w, "  %-29s %5d %6d %8d  %17d  %12d\n", r.Rule, r.FiredWhenDefer, r.FiredWhenReview, r.FiredWhenContinue, r.InFalseAutonomy, r.NotObtained)
	}
	for _, r := range d.Rules {
		if r.InFalseAutonomy > 0 {
			fmt.Fprintf(w, "→ %s fired in %d false autonom(y/ies) without reaching review: consider raising its points\n", r.Rule, r.InFalseAutonomy)
		}
		if r.FiredWhenContinue > 0 && r.FiredWhenDefer == 0 && r.FiredWhenReview == 0 && r.FiredWhenContinue >= 5 {
			fmt.Fprintf(w, "→ %s fired %d time(s), only where the AI could have gone on alone: consider lowering its points\n", r.Rule, r.FiredWhenContinue)
		}
	}
	if len(d.Unseen) > 0 {
		fmt.Fprintf(w, "→ %d false autonom(y/ies) where no rule fired at all: %s\n", len(d.Unseen), strings.Join(d.Unseen, ", "))
		fmt.Fprintln(w, "  the evidence that would have caught them is not collected; a weight change cannot fix this. Add a rule or hand it in with --external")
	}
}

// ActionList is the closed list of actions, for usage text.
func ActionList() string {
	return strings.Join(snapshot.KnownActions, "|")
}

func tradeoffs(w io.Writer, a, b replay.Metrics) {
	t := replay.Tradeoffs(a, b)
	if len(t) == 0 {
		fmt.Fprintln(w, "it is worse on no measure")
		return
	}
	fmt.Fprintln(w, "what it costs (worse on these measures; whether that is worth it is the owner's call):")
	for _, x := range t {
		fmt.Fprintf(w, "  %s\n", x)
	}
}

// HookMessage is what an agent hook shows for a decision: the headline, the decision id, the
// strongest reasons, and the command that records what the human did. The id is spelled out
// because "last" can point at a later, silent decision by the time the human answers.
func HookMessage(l *decisionlog.Line) string {
	var b strings.Builder
	fmt.Fprintf(&b, "flagman: %s (score %d, decision %s)", Headline(l.Result.Verdict), l.Result.Score, l.ID)
	n := 0
	for _, f := range score.FiredOnly(l.Findings) {
		fmt.Fprintf(&b, "\n- %s: %s", f.Rule, f.Summary)
		if n++; n == 4 {
			break
		}
	}
	if l.Result.Verdict == score.Defer {
		fmt.Fprintf(&b, "\nrecord what you decide: flagman human %s approved|changed|stopped", l.ID)
	} else {
		fmt.Fprintf(&b, "\nafterwards: flagman outcome %s ok|problem", l.ID)
	}
	return b.String()
}

// Pending lists the decisions whose truth is not known yet, newest first, so the human and
// outcome can be recorded in one sitting. Continue decisions are listed too: without their
// outcomes a false autonomy cannot be counted at all.
func Pending(w io.Writer, entries []*decisionlog.Entry, limit int) {
	var open []*decisionlog.Entry
	for _, e := range entries {
		if _, ok := decisionlog.TruthOf(e); !ok {
			open = append(open, e)
		}
	}
	fmt.Fprintf(w, "%d of %d decisions have no known truth yet (no human decision, outcome or label)\n", len(open), len(entries))
	if len(open) == 0 {
		return
	}
	byVerdict := map[score.Verdict]int{}
	for _, e := range open {
		if e.Decision.Result != nil {
			byVerdict[e.Decision.Result.Verdict]++
		}
	}
	fmt.Fprintf(w, "  continue %d, review %d, defer %d\n\n", byVerdict[score.Continue], byVerdict[score.Review], byVerdict[score.Defer])
	shown := 0
	for i := len(open) - 1; i >= 0 && (limit <= 0 || shown < limit); i-- {
		e := open[i]
		d := e.Decision
		v, sc := "-", 0
		if d.Result != nil {
			v, sc = string(d.Result.Verdict), d.Result.Score
		}
		branch := ""
		if d.Snapshot.Repo != nil {
			branch = d.Snapshot.Repo.Root + "@" + d.Snapshot.Repo.Branch
		}
		fmt.Fprintf(w, "%s  %s  %-8s %-8s %3d  %s\n", d.ID, d.At.Local().Format("01-02 15:04"), d.Snapshot.Action, v, sc, branch)
		var top []string
		for _, f := range score.FiredOnly(d.Findings) {
			top = append(top, f.Rule)
			if len(top) == 3 {
				break
			}
		}
		if len(top) > 0 {
			fmt.Fprintf(w, "      %s\n", strings.Join(top, ", "))
		}
		shown++
	}
	if shown < len(open) {
		fmt.Fprintf(w, "… %d older not shown (--all)\n", len(open)-shown)
	}
	fmt.Fprintln(w, "\nrecord:  flagman outcome <id> ok|problem    flagman human <id> approved|changed|stopped")
	if byVerdict[score.Continue] > 0 {
		fmt.Fprintln(w, "⚠ continue decisions need an outcome: without one, a false autonomy cannot be counted")
	}
}
