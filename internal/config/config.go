// Package config holds the weights, thresholds and paths a project tunes.
//
// The file is JSON (standard library only, docs/adr/0002). Every number in it is a parameter,
// not a measurement: the defaults below are starting points, and `replay` is how a project
// finds out whether a change to them is an improvement (docs/adr/0006).
//
// The config is hashed (Hash) and the hash is stored with every decision, so the log can say
// under which parameters a verdict was produced.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
)

// Config is the whole file.
type Config struct {
	// Version of the config shape. Bumped only when a field changes meaning.
	Version int `json:"version"`

	// Thresholds turn a score into a verdict: score >= Defer → defer; >= Review → review.
	Thresholds Thresholds `json:"thresholds"`

	// Points per rule. A rule not listed scores zero (and says so). Hard rules force defer
	// regardless of points; which rules are hard is decided here, not in code.
	Rules map[string]RuleParams `json:"rules"`

	// Sensitive path groups: a changed path matching any pattern of a group fires
	// diff.sensitive-paths with that group's points. Patterns are path.Match globs with
	// "**" allowed as "any directory depth" (see internal/rules.matchPath).
	SensitivePaths []PathGroup `json:"sensitive_paths"`

	// TestPatterns say which changed paths count as tests (diff.no-tests).
	TestPatterns []string `json:"test_patterns"`

	// Evidence sources that need a setting.
	Evidence Evidence `json:"evidence"`

	// Replay cost weights (docs/adr/0006): what each kind of mistake costs, relative to each other.
	Costs Costs `json:"costs"`
}

// Thresholds are the two cut points on the 0–100 score.
type Thresholds struct {
	Review int `json:"review"`
	Defer  int `json:"defer"`
}

// RuleParams is what a project may tune per rule.
type RuleParams struct {
	Points int  `json:"points"`
	Hard   bool `json:"hard,omitempty"`
	// Off disables the rule: it still runs and is reported as "off", scoring nothing.
	Off bool `json:"off,omitempty"`
	// Why is the project's own note, kept verbatim through load/save so a tuned weight carries
	// its reason (rules/evidence.md: a number without its grounds is deleted, not corrected).
	Why string `json:"why,omitempty"`
}

// PathGroup is one named set of sensitive paths.
type PathGroup struct {
	Name     string   `json:"name"`
	Patterns []string `json:"patterns"`
	Points   int      `json:"points"`
	Why      string   `json:"why,omitempty"`
}

// Evidence configures collectors.
type Evidence struct {
	// DefaultBase is the ref the diff is taken against when --base is not given.
	// Empty means: the remote's default branch when it can be found, else "main".
	DefaultBase string `json:"default_base,omitempty"`
	// HistoryWindowDays bounds the hotspot and revert search.
	HistoryWindowDays int `json:"history_window_days"`
	// HotspotTouches is how many commits in the window make a path a hotspot.
	HotspotTouches int `json:"hotspot_touches"`
	// LargeChangeFiles / LargeChangeLines are the size thresholds for diff.large.
	LargeChangeFiles int `json:"large_change_files"`
	LargeChangeLines int `json:"large_change_lines"`
	// Tests is the command that runs the project's tests, as a shell line. Empty means the
	// tests are not run by the tool (reported as not obtained unless --tests says otherwise).
	// It is the project's own command, run in the project's own directory: opt-in, local.
	Tests TestsEvidence `json:"tests"`
	// ADRDir is where decision records live.
	ADRDir string `json:"adr_dir"`
}

// TestsEvidence configures the test collector.
type TestsEvidence struct {
	Command string `json:"command,omitempty"`
	// TimeoutSeconds bounds the command. Zero means the default (internal/collect).
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// Costs weight the replay confusion matrix (internal/replay). They are relative: only the ratios
// matter. The defaults say a false autonomy costs ten unnecessary deferrals.
type Costs struct {
	FalseAutonomy       float64 `json:"false_autonomy"`
	MissedEscalation    float64 `json:"missed_escalation"`
	UnnecessaryDeferral float64 `json:"unnecessary_deferral"`
	OverReview          float64 `json:"over_review"`
}

// Default returns the starting configuration. Every value here is a parameter to be measured
// against a project's own log, not a claim.
func Default() *Config {
	return &Config{
		Version:    1,
		Thresholds: Thresholds{Review: 30, Defer: 60},
		Rules: map[string]RuleParams{
			"action.irreversible": {Points: 100, Hard: true,
				Why: "merge, release, deploy, force-push, delete, migrate and sending data out cannot be walked back; the human gate sits there (.claude/rules/owner-decisions.md)"},
			"action.publishes": {Points: 20,
				Why: "push and PR are visible to others and leave history, but can be followed up"},
			"task.open-decision": {Points: 100, Hard: true,
				Why: "an unresolved owner decision means the owner has not decided; proceeding decides in their place"},
			"task.closed": {Points: 100, Hard: true,
				Why: "work on a closed issue is work nobody asked for; confirm first"},
			"task.missing": {Points: 15,
				Why: "without a task there is no declared scope, so nothing bounds the change"},
			"task.no-scope": {Points: 10,
				Why: "scope and out-of-scope bound what may be touched; without them drift is invisible"},
			"task.no-acceptance-criteria": {Points: 10,
				Why: "without acceptance criteria nobody can say when it is done"},
			"diff.outside-scope": {Points: 100, Hard: true,
				Why: "a change to a path the task names as out of scope crosses a boundary the owner drew"},
			"diff.not-in-scope": {Points: 20,
				Why: "a change to a path outside the declared scope needs a human to widen the scope"},
			"diff.large": {Points: 15,
				Why: "large changes are harder to review and more often fail (Ehsani et al. 2026, failed agent PRs are larger)"},
			"diff.no-tests": {Points: 15,
				Why: "code changed without a test changing leaves nothing behind that asserts the claim"},
			"diff.deletes-files": {Points: 10,
				Why: "deleted files are recoverable from git but easy to miss in review"},
			"diff.sensitive-paths": {Points: 0,
				Why: "points come from the matching sensitive_paths group; this entry only switches the rule"},
			"diff.dependencies": {Points: 15,
				Why: "a dependency change brings in code and a licence nobody in this repository wrote"},
			"repo.default-branch": {Points: 25,
				Why: "committing on the default branch skips the review a branch gets (.claude/rules/git.md)"},
			"history.hotspot": {Points: 10,
				Why: "a path changed many times recently is where defects cluster (Tornhill, Your Code as a Crime Scene)"},
			"history.recent-revert": {Points: 20,
				Why: "a recent revert on the same path means the last attempt here was wrong"},
			"tests.failing": {Points: 40,
				Why: "failing tests before commit, push or merge is the clearest objective signal to stop"},
			"tests.not-obtained": {Points: 10,
				Why: "unknown is not pass; unmeasured evidence pushes toward asking (selective prediction: abstain when unsure)"},
			"adr.names-changed-path": {Points: 15,
				Why: "a decision record that names a changed path is a boundary; read it before changing the path"},
			"repo.not-obtained": {Points: 20,
				Why: "with no repository there is no diff, no history and no base to judge against"},
			"external": {Points: 0,
				Why: "points come from each external finding; this entry only switches the rule"},
		},
		SensitivePaths: []PathGroup{
			{Name: "flagman-config", Points: 60, Patterns: []string{".flagman/**"},
				Why: "a change to the gate's own rules or thresholds is the owner's to make; an agent loosening its own gate is the case this tool exists for"},
			{Name: "ci", Points: 20, Patterns: []string{".github/**", ".gitlab-ci.yml", ".circleci/**", "Jenkinsfile"},
				Why: "CI changes run with the repository's credentials"},
			{Name: "secrets-and-auth", Points: 25, Patterns: []string{"**/auth/**", "**/*secret*", "**/*credential*", "**/*password*", "**/*.pem", "**/*.key", "**/oauth/**", "**/security/**"},
				Why: "authentication and secrets are where a mistake is a breach"},
			{Name: "database", Points: 20, Patterns: []string{"**/migrations/**", "**/migrate/**", "**/*.sql", "**/schema/**"},
				Why: "schema changes are hard to walk back once applied"},
			{Name: "infrastructure", Points: 20, Patterns: []string{"**/*.tf", "**/terraform/**", "**/k8s/**", "**/helm/**", "Dockerfile", "**/Dockerfile", "docker-compose*.yml", "**/deploy/**"},
				Why: "infrastructure changes reach running systems"},
			{Name: "billing", Points: 25, Patterns: []string{"**/billing/**", "**/payment*/**", "**/invoice*/**"},
				Why: "money moves"},
			{Name: "licence-and-policy", Points: 15, Patterns: []string{"LICENSE*", "SECURITY.md", "CODE_OF_CONDUCT.md", "docs/adr/**"},
				Why: "what the project promises outsiders is the owner's to change"},
		},
		TestPatterns: []string{
			"**/*_test.go", "**/*.test.*", "**/*.spec.*", "**/test/**", "**/tests/**", "**/__tests__/**",
			"**/testdata/**", "**/spec/**", "**/e2e/**",
		},
		Evidence: Evidence{
			HistoryWindowDays: 90,
			HotspotTouches:    5,
			LargeChangeFiles:  10,
			LargeChangeLines:  400,
			ADRDir:            "docs/adr",
		},
		Costs: Costs{FalseAutonomy: 10, MissedEscalation: 3, UnnecessaryDeferral: 1, OverReview: 0.5},
	}
}

// Load reads a config file. A missing file is an error the caller distinguishes with
// errors.Is(err, os.ErrNotExist).
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &Config{}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Save writes the config, indented, so a human can edit it.
func Save(path string, c *Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Validate rejects a config whose numbers cannot mean anything.
func (c *Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("config version %d is not supported (this build reads version 1)", c.Version)
	}
	t := c.Thresholds
	if t.Review < 0 || t.Defer > 100 || t.Review > t.Defer {
		return errors.New("thresholds must satisfy 0 <= review <= defer <= 100")
	}
	for name, r := range c.Rules {
		if r.Points < 0 || r.Points > 100 {
			return fmt.Errorf("rule %q: points must be within 0..100", name)
		}
	}
	for _, g := range c.SensitivePaths {
		if g.Name == "" {
			return errors.New("every sensitive_paths group needs a name")
		}
		if g.Points < 0 || g.Points > 100 {
			return fmt.Errorf("sensitive_paths %q: points must be within 0..100", g.Name)
		}
	}
	if c.Evidence.HistoryWindowDays <= 0 {
		return errors.New("evidence.history_window_days must be positive")
	}
	return nil
}

// Hash is a stable digest of the parameters, stored with each decision.
func (c *Config) Hash() string {
	// json.Marshal sorts map keys, so the encoding is stable for equal configs.
	b, _ := json.Marshal(c)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:8])
}

// RuleNames returns the configured rule names, sorted.
func (c *Config) RuleNames() []string {
	names := make([]string, 0, len(c.Rules))
	for n := range c.Rules {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
