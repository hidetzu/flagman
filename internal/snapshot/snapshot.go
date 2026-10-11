// Package snapshot holds what was observed at the moment a decision was asked for.
//
// A Snapshot is data, never behaviour: collectors fill it (internal/collect), rules read it
// (internal/rules), and the decision log stores it verbatim (internal/decisionlog). Because the
// whole snapshot is stored, a decision can be re-scored later under other rules or weights
// without touching the repository again — that is what `replay` relies on
// (docs/adr/0003-rules-are-pure-functions-of-a-stored-snapshot.md).
//
// Every source distinguishes "observed" from "could not be obtained" (.claude/rules/evidence.md:
// could not be obtained ≠ not there). A nil pointer below means the source was not obtained,
// and Missing says why.
package snapshot

import "time"

// Snapshot is everything a decision was made from.
type Snapshot struct {
	TakenAt time.Time `json:"taken_at"`
	// Action is what the agent is about to do (see Action constants). Declared by the caller;
	// the mapping from action to irreversibility is the rule's, not the caller's.
	Action string `json:"action"`
	// Intent is the caller's one-line description of the step. Free text, stored for the human
	// reading the log; no rule reads it (an agent's own words are not evidence).
	Intent string `json:"intent,omitempty"`

	Repo     *Repo             `json:"repo,omitempty"`
	Diff     *Diff             `json:"diff,omitempty"`
	History  *History          `json:"history,omitempty"`
	Tests    *Tests            `json:"tests,omitempty"`
	Task     *Task             `json:"task,omitempty"`
	ADRs     *ADRs             `json:"adrs,omitempty"`
	External []ExternalFinding `json:"external,omitempty"`

	// Missing names every source that could not be obtained, with the reason.
	Missing []Missing `json:"missing,omitempty"`
}

// Missing records a source that was asked for and could not be read.
type Missing struct {
	Source string `json:"source"` // one of the Source* constants
	Reason string `json:"reason"` // a sentence for the human
}

// Source names, used in Missing and in rule evidence.
const (
	SourceRepo    = "repo"
	SourceDiff    = "diff"
	SourceHistory = "history"
	SourceTests   = "tests"
	SourceTask    = "task"
	SourceADRs    = "adrs"
)

// Repo is the state of the working copy.
type Repo struct {
	Root          string `json:"root"` // basename only; never an absolute path (rules/git.md)
	Branch        string `json:"branch"`
	DefaultBranch string `json:"default_branch"`
	// Dirty is true when the working tree has uncommitted changes (tracked or untracked).
	Dirty bool   `json:"dirty"`
	Head  string `json:"head,omitempty"` // short commit id
}

// Diff is the change under judgement: working tree (and index) against Base.
type Diff struct {
	Base       string        `json:"base"`
	Files      []ChangedFile `json:"files"`
	Insertions int           `json:"insertions"`
	Deletions  int           `json:"deletions"`
}

// ChangedFile is one path in the diff.
type ChangedFile struct {
	Path    string `json:"path"`
	Status  string `json:"status"` // A added, M modified, D deleted, R renamed, ? untracked
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Binary  bool   `json:"binary,omitempty"`
}

// History is what the log says about the changed paths.
type History struct {
	WindowDays int `json:"window_days"`
	// Touches is how many commits in the window touched each changed path.
	Touches map[string]int `json:"touches,omitempty"`
	// Reverts lists revert commits in the window that touched a changed path.
	Reverts []Revert `json:"reverts,omitempty"`
}

// Revert is one revert commit that touched a changed path.
type Revert struct {
	Commit  string   `json:"commit"`
	Subject string   `json:"subject"`
	Paths   []string `json:"paths"`
}

// Tests is the result of the project's test command, or a result handed in.
type Tests struct {
	// Status is "pass", "fail" or "not-run" (not-run means the caller said so explicitly).
	Status string `json:"status"`
	// Source says where the status came from: "command", "flag" or "file".
	Source  string `json:"source"`
	Command string `json:"command,omitempty"`
	// Summary is the last line of output, kept so a human can see what failed.
	Summary string `json:"summary,omitempty"`
}

// Test statuses.
const (
	TestsPass   = "pass"
	TestsFail   = "fail"
	TestsNotRun = "not-run"
)

// Task is the work context: an issue, a task file, or a brief.
type Task struct {
	Ref    string   `json:"ref"`             // "owner/repo#N" or the file name given
	State  string   `json:"state,omitempty"` // "open", "closed" or "" when unknown (a file)
	Labels []string `json:"labels,omitempty"`
	// Sections found by heading, with the lines under each (trimmed, non-empty).
	HasGoal               bool `json:"has_goal"`
	HasScope              bool `json:"has_scope"`
	HasOutOfScope         bool `json:"has_out_of_scope"`
	HasAcceptanceCriteria bool `json:"has_acceptance_criteria"`
	// Scope and OutOfScope are the path-like tokens found under those headings.
	Scope      []string `json:"scope,omitempty"`
	OutOfScope []string `json:"out_of_scope,omitempty"`
	// OpenDecisions are lines that say a human has not decided yet.
	OpenDecisions []Quote `json:"open_decisions,omitempty"`
}

// Quote is one line of a document, with where it was found.
type Quote struct {
	Where string `json:"where"` // "file:line" or "issue#N:line"
	Text  string `json:"text"`
}

// ADRs lists the decision records that name a changed path.
type ADRs struct {
	Dir      string       `json:"dir"`
	Count    int          `json:"count"` // how many ADR files were read
	Mentions []ADRMention `json:"mentions,omitempty"`
}

// ADRMention is one ADR line that names one changed path.
type ADRMention struct {
	ADR  string `json:"adr"`
	Line int    `json:"line"`
	Path string `json:"path"`
	Text string `json:"text"`
}

// ExternalFinding is evidence handed in from outside (another tool, a CI result, the owner's
// instruction). It is stored as received and reported under its own id.
type ExternalFinding struct {
	ID       string   `json:"id"`
	Summary  string   `json:"summary"`
	Points   int      `json:"points"`
	Hard     bool     `json:"hard,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
}

// Actions the caller can declare. The irreversibility of each is decided in internal/rules.
const (
	ActionEdit      = "edit"
	ActionCommit    = "commit"
	ActionPush      = "push"
	ActionPR        = "pr"
	ActionMerge     = "merge"
	ActionRelease   = "release"
	ActionDeploy    = "deploy"
	ActionForcePush = "force-push"
	ActionDelete    = "delete"
	ActionMigrate   = "migrate"
	ActionExternal  = "external"
)

// KnownActions is the closed list, in the order a human reads them (least to most irreversible).
var KnownActions = []string{
	ActionEdit, ActionCommit, ActionPush, ActionPR,
	ActionMerge, ActionRelease, ActionDeploy, ActionForcePush, ActionDelete, ActionMigrate, ActionExternal,
}

// IsKnownAction reports whether a is in KnownActions.
func IsKnownAction(a string) bool {
	for _, k := range KnownActions {
		if k == a {
			return true
		}
	}
	return false
}
