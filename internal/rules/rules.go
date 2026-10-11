// Package rules turns a Snapshot into named findings.
//
// Every rule is a pure function of (Snapshot, Config): no I/O, no clock, no randomness
// (docs/adr/0003). That is what lets `flagman replay` re-score a decision recorded months ago
// and get exactly the answer the same rules would have given then.
//
// A rule reports one of four statuses (internal/score): fired, clear, not-obtained, off.
// "not-obtained" is never folded into "clear": evidence that could not be read is not evidence
// of absence (.claude/rules/evidence.md).
package rules

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/hidetzu/flagman/internal/config"
	"github.com/hidetzu/flagman/internal/score"
	"github.com/hidetzu/flagman/internal/snapshot"
)

// rule is one named check.
type rule struct {
	name string
	run  func(s *snapshot.Snapshot, c *config.Config) []score.Finding
}

// all is the closed list of rules, in the order they are reported.
var all = []rule{
	{"action.irreversible", actionIrreversible},
	{"action.publishes", actionPublishes},
	{"task.closed", taskClosed},
	{"task.open-decision", taskOpenDecision},
	{"task.missing", taskMissing},
	{"task.no-scope", taskNoScope},
	{"task.no-acceptance-criteria", taskNoAC},
	{"diff.outside-scope", diffOutsideScope},
	{"diff.not-in-scope", diffNotInScope},
	{"diff.large", diffLarge},
	{"diff.no-tests", diffNoTests},
	{"diff.deletes-files", diffDeletes},
	{"diff.sensitive-paths", diffSensitive},
	{"diff.dependencies", diffDependencies},
	{"repo.not-obtained", repoNotObtained},
	{"repo.default-branch", repoDefaultBranch},
	{"history.hotspot", historyHotspot},
	{"history.recent-revert", historyRevert},
	{"tests.failing", testsFailing},
	{"tests.not-obtained", testsNotObtained},
	{"adr.names-changed-path", adrNamesPath},
	{"external", external},
}

// Names returns every rule name, in report order.
func Names() []string {
	out := make([]string, len(all))
	for i, r := range all {
		out[i] = r.name
	}
	return out
}

// Evaluate runs every rule and applies the config's points, hard flags and off switches.
// A rule absent from the config scores zero and says so in its summary.
func Evaluate(s *snapshot.Snapshot, c *config.Config) []score.Finding {
	var out []score.Finding
	for _, r := range all {
		params, configured := c.Rules[r.name]
		fs := r.run(s, c)
		for i := range fs {
			f := &fs[i]
			if f.Rule == "" {
				f.Rule = r.name
			}
			if params.Off {
				f.Status = score.Off
				f.Points = 0
				f.Hard = false
				continue
			}
			if f.Status != score.Fired {
				f.Points = 0
				continue
			}
			// diff.sensitive-paths and external carry their own points; every other rule takes
			// the configured points.
			if r.name != "diff.sensitive-paths" && r.name != "external" {
				f.Points = params.Points
				f.Hard = params.Hard
			} else if params.Hard {
				f.Hard = true
			}
			if f.Why == "" {
				f.Why = params.Why
			}
			if !configured {
				f.Summary += " (this rule has no entry in the config, so it scores 0)"
			}
		}
		out = append(out, fs...)
	}
	return out
}

// ── helpers ────────────────────────────────────────────────────────────────

func fired(summary string, evidence ...string) []score.Finding {
	return []score.Finding{{Status: score.Fired, Summary: summary, Evidence: evidence}}
}

func clear(summary string) []score.Finding {
	return []score.Finding{{Status: score.Clear, Summary: summary}}
}

func notObtained(source string, s *snapshot.Snapshot) []score.Finding {
	why := "the " + source + " evidence was not obtained"
	for _, m := range s.Missing {
		if m.Source == source {
			why += ": " + m.Reason
		}
	}
	return []score.Finding{{Status: score.NotObtained, Summary: why}}
}

// MatchPath reports whether p matches a glob pattern. "**" matches any number of path
// segments (including none); other segments follow path.Match. A pattern without "/" is
// matched against the base name as well, so "Dockerfile" matches "deploy/Dockerfile".
func MatchPath(pattern, p string) bool {
	if !strings.Contains(pattern, "/") {
		if ok, _ := path.Match(pattern, path.Base(p)); ok {
			return true
		}
	}
	return matchSegs(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

func matchSegs(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			if len(pat) == 1 {
				return true
			}
			for i := 0; i <= len(segs); i++ {
				if matchSegs(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// inScope reports whether p is covered by a scope entry: an exact path, a directory prefix,
// or a glob.
func inScope(entry, p string) bool {
	e := strings.TrimPrefix(strings.TrimSpace(entry), "./")
	if e == "" {
		return false
	}
	if strings.ContainsAny(e, "*?[") {
		return MatchPath(e, p)
	}
	e = strings.TrimSuffix(e, "/")
	return p == e || strings.HasPrefix(p, e+"/")
}

func isTest(c *config.Config, p string) bool {
	for _, pat := range c.TestPatterns {
		if MatchPath(pat, p) {
			return true
		}
	}
	return false
}

// isDoc reports paths that are documentation only; they do not need a test to change.
func isDoc(p string) bool {
	ext := strings.ToLower(path.Ext(p))
	switch ext {
	case ".md", ".txt", ".rst", ".adoc":
		return true
	}
	return false
}

func paths(files []snapshot.ChangedFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return out
}

func limit(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return append(append([]string{}, xs[:n]...), fmt.Sprintf("… and %d more", len(xs)-n))
}

// ── action ─────────────────────────────────────────────────────────────────

// Irreversible actions cannot be walked back by the agent alone once done.
var irreversible = map[string]string{
	snapshot.ActionMerge:     "merging lands the change on the default branch others build on",
	snapshot.ActionRelease:   "a published release is downloaded and cannot be recalled",
	snapshot.ActionDeploy:    "a deploy reaches running systems and their users",
	snapshot.ActionForcePush: "a force push discards history others may hold",
	snapshot.ActionDelete:    "deleting data or branches outside git cannot be undone",
	snapshot.ActionMigrate:   "an applied migration changes stored data",
	snapshot.ActionExternal:  "data sent to an outside service cannot be called back",
}

func actionIrreversible(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if why, ok := irreversible[s.Action]; ok {
		return fired(fmt.Sprintf("the next step is %q, which cannot be walked back: %s", s.Action, why), "action="+s.Action)
	}
	return clear(fmt.Sprintf("the next step %q can be walked back", s.Action))
}

func actionPublishes(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	switch s.Action {
	case snapshot.ActionPush, snapshot.ActionPR:
		return fired(fmt.Sprintf("the next step %q makes the change visible to others", s.Action), "action="+s.Action)
	}
	return clear("the next step does not publish anything by itself")
}

// ── task ───────────────────────────────────────────────────────────────────

func taskClosed(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.Task == nil {
		return notObtained(snapshot.SourceTask, s)
	}
	// gh reports a pull request named as owner/repo#N with state "merged"; finished either way.
	if s.Task.State == "closed" || s.Task.State == "merged" {
		return fired("the task "+s.Task.Ref+" is "+s.Task.State, s.Task.Ref+" state="+s.Task.State)
	}
	return clear("the task is not closed")
}

func taskOpenDecision(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.Task == nil {
		return notObtained(snapshot.SourceTask, s)
	}
	if len(s.Task.OpenDecisions) == 0 {
		return clear("the task names no undecided question")
	}
	ev := make([]string, 0, len(s.Task.OpenDecisions))
	for _, q := range s.Task.OpenDecisions {
		ev = append(ev, q.Where+": "+q.Text)
	}
	return fired(fmt.Sprintf("the task says something is still undecided (%d place(s))", len(ev)), limit(ev, 5)...)
}

func taskMissing(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.Task == nil {
		return fired("no task was given, so nothing declares what this change is for or where it may reach",
			"pass --task <file> or --task owner/repo#N")
	}
	return clear("a task was given: " + s.Task.Ref)
}

func taskNoScope(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.Task == nil {
		return notObtained(snapshot.SourceTask, s)
	}
	var missing []string
	if !s.Task.HasScope {
		missing = append(missing, "Scope")
	}
	if !s.Task.HasOutOfScope {
		missing = append(missing, "Out of Scope")
	}
	if len(missing) == 0 {
		return clear("the task declares Scope and Out of Scope")
	}
	return fired("the task does not declare "+strings.Join(missing, " or "), s.Task.Ref)
}

func taskNoAC(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.Task == nil {
		return notObtained(snapshot.SourceTask, s)
	}
	if s.Task.HasAcceptanceCriteria {
		return clear("the task has acceptance criteria")
	}
	return fired("the task has no acceptance criteria", s.Task.Ref)
}

// ── diff ───────────────────────────────────────────────────────────────────

func diffOutsideScope(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.Diff == nil {
		return notObtained(snapshot.SourceDiff, s)
	}
	if s.Task == nil {
		return notObtained(snapshot.SourceTask, s)
	}
	if len(s.Task.OutOfScope) == 0 {
		return clear("the task names no out-of-scope paths to compare against")
	}
	var hit []string
	for _, f := range s.Diff.Files {
		for _, e := range s.Task.OutOfScope {
			if inScope(e, f.Path) {
				hit = append(hit, f.Path+" (out of scope: "+e+")")
				break
			}
		}
	}
	if len(hit) == 0 {
		return clear("no changed path is named out of scope")
	}
	return fired(fmt.Sprintf("%d changed path(s) are named out of scope by the task", len(hit)), limit(hit, 8)...)
}

func diffNotInScope(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.Diff == nil {
		return notObtained(snapshot.SourceDiff, s)
	}
	if s.Task == nil {
		return notObtained(snapshot.SourceTask, s)
	}
	if len(s.Task.Scope) == 0 {
		return clear("the task names no in-scope paths to compare against")
	}
	var out []string
	n := 0
	for _, f := range s.Diff.Files {
		if f.Path == s.Task.Ref {
			continue // the task file describes the change; it is not part of it
		}
		n++
		ok := false
		for _, e := range s.Task.Scope {
			if inScope(e, f.Path) {
				ok = true
				break
			}
		}
		if !ok {
			out = append(out, f.Path)
		}
	}
	if len(out) == 0 {
		return clear(fmt.Sprintf("every changed path is inside the declared scope (%d of %d)", n, n))
	}
	return fired(fmt.Sprintf("%d of %d changed path(s) are outside the declared scope", len(out), n), limit(out, 8)...)
}

func diffLarge(s *snapshot.Snapshot, c *config.Config) []score.Finding {
	if s.Diff == nil {
		return notObtained(snapshot.SourceDiff, s)
	}
	n := len(s.Diff.Files)
	lines := s.Diff.Insertions + s.Diff.Deletions
	fl, ll := c.Evidence.LargeChangeFiles, c.Evidence.LargeChangeLines
	ev := fmt.Sprintf("%d files, %d lines changed (+%d −%d); thresholds %d files / %d lines",
		n, lines, s.Diff.Insertions, s.Diff.Deletions, fl, ll)
	if (fl > 0 && n >= fl) || (ll > 0 && lines >= ll) {
		return fired("the change is large", ev)
	}
	return clear("the change is within the size thresholds: " + ev)
}

func diffNoTests(s *snapshot.Snapshot, c *config.Config) []score.Finding {
	if s.Diff == nil {
		return notObtained(snapshot.SourceDiff, s)
	}
	// An edit in progress is not yet a claim that the change is done; the test is asked for
	// when the change is committed or published.
	if s.Action == snapshot.ActionEdit {
		return clear("editing: tests are judged when the change is committed")
	}
	var code []string
	tests := 0
	for _, f := range s.Diff.Files {
		switch {
		case isTest(c, f.Path):
			tests++
		// Dependency manifests are judged by diff.dependencies; counting them here too would
		// score one fact twice.
		case isDoc(f.Path), f.Status == "D", depFiles[path.Base(f.Path)]:
		default:
			code = append(code, f.Path)
		}
	}
	if len(code) == 0 {
		return clear("no non-test, non-document file changed")
	}
	if tests > 0 {
		return clear(fmt.Sprintf("code changed and %d test file(s) changed with it", tests))
	}
	return fired(fmt.Sprintf("%d code file(s) changed and no test file changed", len(code)), limit(code, 6)...)
}

func diffDeletes(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.Diff == nil {
		return notObtained(snapshot.SourceDiff, s)
	}
	var del []string
	for _, f := range s.Diff.Files {
		if f.Status == "D" {
			del = append(del, f.Path)
		}
	}
	if len(del) == 0 {
		return clear("no file is deleted")
	}
	return fired(fmt.Sprintf("%d file(s) are deleted", len(del)), limit(del, 8)...)
}

// diffSensitive reports one finding per matching group, each with the group's points.
func diffSensitive(s *snapshot.Snapshot, c *config.Config) []score.Finding {
	if s.Diff == nil {
		return notObtained(snapshot.SourceDiff, s)
	}
	var out []score.Finding
	for _, g := range c.SensitivePaths {
		var hit []string
		for _, f := range s.Diff.Files {
			for _, p := range g.Patterns {
				if MatchPath(p, f.Path) {
					hit = append(hit, f.Path)
					break
				}
			}
		}
		if len(hit) > 0 {
			out = append(out, score.Finding{
				Rule:     "diff.sensitive-paths",
				Status:   score.Fired,
				Points:   g.Points,
				Summary:  fmt.Sprintf("sensitive area %q is changed (%d path(s))", g.Name, len(hit)),
				Evidence: limit(hit, 6),
				Why:      g.Why,
			})
		}
	}
	if len(out) == 0 {
		return clear("no changed path is in a sensitive area")
	}
	return out
}

// Dependency manifests and lock files, by base name.
var depFiles = map[string]bool{
	"go.mod": true, "go.sum": true, "package.json": true, "package-lock.json": true,
	"pnpm-lock.yaml": true, "yarn.lock": true, "Cargo.toml": true, "Cargo.lock": true,
	"requirements.txt": true, "pyproject.toml": true, "poetry.lock": true, "uv.lock": true,
	"Gemfile": true, "Gemfile.lock": true, "composer.json": true, "composer.lock": true,
	"pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
}

func diffDependencies(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.Diff == nil {
		return notObtained(snapshot.SourceDiff, s)
	}
	var hit []string
	for _, f := range s.Diff.Files {
		if depFiles[path.Base(f.Path)] {
			hit = append(hit, f.Path)
		}
	}
	if len(hit) == 0 {
		return clear("no dependency manifest changed")
	}
	return fired("a dependency manifest changed", hit...)
}

// ── repo ───────────────────────────────────────────────────────────────────

func repoNotObtained(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.Repo == nil {
		r := notObtained(snapshot.SourceRepo, s)
		// This rule exists to make the absence count: an unreadable repository is itself a
		// reason to ask, so it fires rather than reporting not-obtained.
		r[0].Status = score.Fired
		r[0].Summary = "the repository could not be read, so nothing about the change is known"
		return r
	}
	return clear("the repository was read")
}

func repoDefaultBranch(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.Repo == nil {
		return notObtained(snapshot.SourceRepo, s)
	}
	if s.Repo.DefaultBranch == "" {
		return []score.Finding{{Status: score.NotObtained, Summary: "the default branch could not be determined"}}
	}
	if s.Action == snapshot.ActionEdit {
		return clear("editing files on any branch is reversible")
	}
	if s.Repo.Branch == s.Repo.DefaultBranch {
		return fired(fmt.Sprintf("the step %q would happen on the default branch %q", s.Action, s.Repo.Branch), "branch="+s.Repo.Branch)
	}
	return clear(fmt.Sprintf("on branch %q, not the default %q", s.Repo.Branch, s.Repo.DefaultBranch))
}

// ── history ────────────────────────────────────────────────────────────────

func historyHotspot(s *snapshot.Snapshot, c *config.Config) []score.Finding {
	if s.Diff == nil {
		return notObtained(snapshot.SourceDiff, s)
	}
	if s.History == nil {
		return notObtained(snapshot.SourceHistory, s)
	}
	var hot []string
	for _, f := range s.Diff.Files {
		if n := s.History.Touches[f.Path]; c.Evidence.HotspotTouches > 0 && n >= c.Evidence.HotspotTouches {
			hot = append(hot, fmt.Sprintf("%s (%d commits in %d days)", f.Path, n, s.History.WindowDays))
		}
	}
	if len(hot) == 0 {
		return clear(fmt.Sprintf("no changed path reached %d commits in the last %d days", c.Evidence.HotspotTouches, s.History.WindowDays))
	}
	sort.Strings(hot)
	return fired(fmt.Sprintf("%d changed path(s) change often", len(hot)), limit(hot, 6)...)
}

func historyRevert(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.History == nil {
		return notObtained(snapshot.SourceHistory, s)
	}
	if len(s.History.Reverts) == 0 {
		return clear(fmt.Sprintf("no revert touched a changed path in the last %d days", s.History.WindowDays))
	}
	var ev []string
	for _, r := range s.History.Reverts {
		ev = append(ev, r.Commit+" "+r.Subject+" — "+strings.Join(limit(r.Paths, 3), ", "))
	}
	return fired(fmt.Sprintf("%d recent revert(s) touched a changed path", len(ev)), limit(ev, 4)...)
}

// ── tests ──────────────────────────────────────────────────────────────────

func testsFailing(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.Tests == nil {
		return notObtained(snapshot.SourceTests, s)
	}
	if s.Tests.Status == snapshot.TestsFail {
		ev := []string{"source=" + s.Tests.Source}
		if s.Tests.Command != "" {
			ev = append(ev, "command: "+s.Tests.Command)
		}
		if s.Tests.Summary != "" {
			ev = append(ev, "last line: "+s.Tests.Summary)
		}
		return fired("the tests fail", ev...)
	}
	if s.Tests.Status == snapshot.TestsPass {
		return clear("the tests pass (" + s.Tests.Source + ")")
	}
	return []score.Finding{{Status: score.NotObtained, Summary: "the tests were declared not run"}}
}

// testsNotObtained makes unknown test status count for something, at its own (lower) weight.
func testsNotObtained(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.Action == snapshot.ActionEdit {
		return clear("editing does not need a test result yet")
	}
	if s.Tests == nil || s.Tests.Status == snapshot.TestsNotRun {
		why := "no test result is known"
		for _, m := range s.Missing {
			if m.Source == snapshot.SourceTests {
				why += ": " + m.Reason
			}
		}
		return fired(why, "pass --tests pass|fail, or set evidence.tests.command and use --run-tests")
	}
	return clear("a test result is known")
}

// ── adr ────────────────────────────────────────────────────────────────────

func adrNamesPath(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if s.ADRs == nil {
		return notObtained(snapshot.SourceADRs, s)
	}
	if len(s.ADRs.Mentions) == 0 {
		return clear(fmt.Sprintf("none of the %d decision record(s) in %s names a changed path", s.ADRs.Count, s.ADRs.Dir))
	}
	var ev []string
	for _, m := range s.ADRs.Mentions {
		ev = append(ev, fmt.Sprintf("%s:%d names %s", m.ADR, m.Line, m.Path))
	}
	return fired("a decision record names a changed path; read it before changing that path", limit(ev, 5)...)
}

// ── external ───────────────────────────────────────────────────────────────

func external(s *snapshot.Snapshot, _ *config.Config) []score.Finding {
	if len(s.External) == 0 {
		return clear("no external finding was handed in")
	}
	var out []score.Finding
	for _, e := range s.External {
		p := e.Points
		if p < 0 {
			p = 0
		}
		if p > 100 {
			p = 100
		}
		out = append(out, score.Finding{
			Rule:     "external",
			Status:   score.Fired,
			Points:   p,
			Hard:     e.Hard,
			Summary:  "external finding " + e.ID + ": " + e.Summary,
			Evidence: e.Evidence,
			Why:      "handed in from outside; its points and hard flag are the provider's",
		})
	}
	return out
}
