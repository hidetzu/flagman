// Command flagman tells an AI coding agent whether it may go on alone, should have a human look,
// or must stop for a human decision — from evidence in the repository, not from the agent's own
// confidence — and keeps a log that lets the rules be measured and improved.
//
// The work happens in internal/; this file parses arguments, wires packages together and maps
// results to exit codes. Every sentence a human reads comes from internal/report.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/hidetzu/flagman/internal/collect"
	"github.com/hidetzu/flagman/internal/config"
	"github.com/hidetzu/flagman/internal/decisionlog"
	"github.com/hidetzu/flagman/internal/replay"
	"github.com/hidetzu/flagman/internal/report"
	"github.com/hidetzu/flagman/internal/rules"
	"github.com/hidetzu/flagman/internal/score"
	"github.com/hidetzu/flagman/internal/snapshot"
)

// version is set at build time (-ldflags "-X main.version=v0.1.0"); otherwise the module
// version from `go install`, otherwise "dev".
var version = ""

// Exit codes. check uses 0/10/20 so that a caller can branch on the verdict and still tell a
// verdict from an error (1) or a usage mistake (2).
const (
	exitContinue = 0
	exitError    = 1
	exitUsage    = 2
	exitReview   = 10
	exitDefer    = 20
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func toolVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

const usage = `flagman — should the AI go on alone, or ask a human?

usage:
  flagman check   [--action A] [--task FILE|owner/repo#N] [--tests pass|fail|not-run]
                  [--run-tests] [--base REF] [--external FILE] [--intent TEXT]
                  [--json] [--no-log] [-v]
  flagman human   <id|last> approved|changed|stopped [--note TEXT]
  flagman outcome <id|last> ok|problem [--note TEXT]
  flagman label   <id|last> continue|review|defer [--note TEXT]
  flagman log     [--json]
  flagman show    <id|last> [--json]
  flagman replay  [--config FILE] [--compare FILE] [--scenarios DIR] [--json]
  flagman suggest [--test-share 0.3] [--write FILE] [--scenarios DIR]
  flagman rules
  flagman init    [--force]
  flagman hook claude-code [--run-tests]
  flagman version

common flags: -C DIR (working copy, default .), --log FILE, --config FILE

check exits 0 (continue), 10 (review) or 20 (defer); 1 is an error, 2 a usage mistake.
actions: ` + "edit|commit|push|pr|merge|release|deploy|force-push|delete|migrate|external" + `
`

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		if len(args) == 0 {
			return exitUsage
		}
		return 0
	}
	cmd, rest := args[0], args[1:]
	var err error
	code := 0
	switch cmd {
	case "check":
		code, err = cmdCheck(rest, stdout)
	case "human":
		err = cmdFollow(rest, stdout, decisionlog.KindHuman, []string{decisionlog.HumanApproved, decisionlog.HumanChanged, decisionlog.HumanStopped})
	case "outcome":
		err = cmdFollow(rest, stdout, decisionlog.KindOutcome, []string{decisionlog.OutcomeOK, decisionlog.OutcomeProblem})
	case "label":
		err = cmdFollow(rest, stdout, decisionlog.KindLabel, []string{string(score.Continue), string(score.Review), string(score.Defer)})
	case "log":
		err = cmdLog(rest, stdout)
	case "show":
		err = cmdShow(rest, stdout)
	case "replay":
		err = cmdReplay(rest, stdout)
	case "suggest":
		err = cmdSuggest(rest, stdout)
	case "rules":
		err = cmdRules(rest, stdout)
	case "init":
		err = cmdInit(rest, stdout)
	case "hook":
		return cmdHook(rest, stdin, stdout, stderr)
	case "version", "--version":
		fmt.Fprintf(stdout, "flagman %s (rules v%s)\n", toolVersion(), decisionlog.RulesVersion)
	default:
		fmt.Fprintf(stderr, "flagman: there is no command %q\n\n%s", cmd, usage)
		return exitUsage
	}
	if err != nil {
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprintf(stderr, "flagman %s: %s\n", cmd, ue.msg)
			return exitUsage
		}
		fmt.Fprintf(stderr, "flagman %s: %v\n", cmd, err)
		return exitError
	}
	return code
}

type usageError struct{ msg string }

func (u usageError) Error() string { return u.msg }

func usagef(f string, a ...any) error { return usageError{fmt.Sprintf(f, a...)} }

// common holds the flags every command shares.
type common struct {
	dir, log, cfg string
}

func addCommon(fs *flag.FlagSet) *common {
	c := &common{}
	fs.StringVar(&c.dir, "C", ".", "working copy")
	fs.StringVar(&c.log, "log", "", "decision log (default <repo>/.flagman/decisions.jsonl, or $FLAGMAN_LOG)")
	fs.StringVar(&c.cfg, "config", "", "config file (default <repo>/.flagman/config.json)")
	return c
}

// parse accepts flags before and after positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, usagef("%v", err)
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// root is the working copy's top level, or the directory itself outside git.
func (c *common) root() string {
	out, err := exec.Command("git", "-C", c.dir, "rev-parse", "--show-toplevel").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	abs, _ := filepath.Abs(c.dir)
	return abs
}

func (c *common) logPath() string {
	if c.log != "" {
		return c.log
	}
	if env := os.Getenv("FLAGMAN_LOG"); env != "" {
		return env
	}
	return filepath.Join(c.root(), ".flagman", "decisions.jsonl")
}

// loadConfig returns the config and a name for it. A missing default file means the built-in
// defaults; a missing file named explicitly is an error.
func (c *common) loadConfig() (*config.Config, string, error) {
	p := c.cfg
	explicit := p != ""
	if !explicit {
		p = filepath.Join(c.root(), ".flagman", "config.json")
	}
	cfg, err := config.Load(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !explicit {
			return config.Default(), "built-in defaults", nil
		}
		return nil, "", err
	}
	return cfg, filepath.Base(p), nil
}

func (c *common) readLog() (*decisionlog.ReadResult, error) {
	return decisionlog.Read(c.logPath())
}

// ensureIgnored keeps the default log out of git: it holds branch names, paths and quoted task
// lines, which are the owner's to publish or not (docs/adr/0007).
//
// It writes to the repository's own .git/info/exclude, never to a file in the working tree:
// a file flagman created there would show up as a change in flagman's next decision.
func ensureIgnored(c *common, logPath string) {
	root := c.root()
	rel, err := filepath.Rel(root, logPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return // the log lives outside the working copy; nothing to ignore
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return // not a git working copy
	}
	ex := strings.TrimSpace(string(out))
	if !filepath.IsAbs(ex) {
		ex = filepath.Join(root, ex)
	}
	line := "/" + filepath.ToSlash(rel)
	if b, err := os.ReadFile(ex); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(l) == line {
				return
			}
		}
	}
	_ = os.MkdirAll(filepath.Dir(ex), 0o755)
	f, err := os.OpenFile(ex, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString("# flagman: the decision log stays on this machine\n" + line + "\n")
}

// ── check ──────────────────────────────────────────────────────────────────

type checkOpts struct {
	action, task, tests, base, external, intent string
	runTests, asJSON, noLog, verbose            bool
}

func cmdCheck(args []string, stdout io.Writer) (int, error) {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	c := addCommon(fs)
	o := checkOpts{}
	fs.StringVar(&o.action, "action", snapshot.ActionCommit, "the step about to be taken")
	fs.StringVar(&o.task, "task", "", "task file or owner/repo#N")
	fs.StringVar(&o.tests, "tests", "", "known test result: pass, fail or not-run")
	fs.BoolVar(&o.runTests, "run-tests", false, "run evidence.tests.command from the config")
	fs.StringVar(&o.base, "base", "", "diff base ref")
	fs.StringVar(&o.external, "external", "", "JSON file of external findings")
	fs.StringVar(&o.intent, "intent", "", "one line describing the step (stored, never scored)")
	fs.BoolVar(&o.asJSON, "json", false, "print the decision as JSON")
	fs.BoolVar(&o.noLog, "no-log", false, "do not append to the decision log")
	fs.BoolVar(&o.verbose, "v", false, "also print grounds and the rules that stayed clear")
	pos, err := parse(fs, args)
	if err != nil {
		return 0, err
	}
	if len(pos) > 0 {
		return 0, usagef("check takes no positional arguments (got %q)", pos[0])
	}
	if !snapshot.IsKnownAction(o.action) {
		return 0, usagef("--action %q is not one of %s", o.action, report.ActionList())
	}
	switch o.tests {
	case "", snapshot.TestsPass, snapshot.TestsFail, snapshot.TestsNotRun:
	default:
		return 0, usagef("--tests must be pass, fail or not-run")
	}
	l, err := decide(c, o)
	if err != nil {
		return 0, err
	}
	if o.asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(l); err != nil {
			return 0, err
		}
	} else {
		report.Check(stdout, l, !o.noLog, o.verbose)
	}
	return exitFor(l.Result.Verdict), nil
}

// decide collects, scores and (unless told not to) logs one decision.
func decide(c *common, o checkOpts) (*decisionlog.Line, error) {
	cfg, _, err := c.loadConfig()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	s := collect.Collect(context.Background(), collect.Options{
		Dir: c.dir, Action: o.action, Intent: o.intent, Base: o.base, Task: o.task,
		Tests: o.tests, RunTests: o.runTests, ExternalFile: o.external, Config: cfg, Now: now,
	})
	fs := rules.Evaluate(s, cfg)
	r := score.Compute(fs, cfg.Thresholds.Review, cfg.Thresholds.Defer)
	l := &decisionlog.Line{
		Kind: decisionlog.KindDecision, ID: decisionlog.NewID(now), At: now.UTC(),
		ToolVersion: toolVersion(), RulesVersion: decisionlog.RulesVersion,
		ConfigHash: cfg.Hash(), Config: cfg, Snapshot: s, Findings: fs, Result: &r,
	}
	if !o.noLog {
		lp := c.logPath()
		ensureIgnored(c, lp)
		if err := decisionlog.Append(lp, *l); err != nil {
			return nil, fmt.Errorf("the decision could not be logged (%v); use --no-log to decide without logging", err)
		}
	}
	return l, nil
}

func exitFor(v score.Verdict) int {
	switch v {
	case score.Review:
		return exitReview
	case score.Defer:
		return exitDefer
	}
	return exitContinue
}

// ── follow-ups ─────────────────────────────────────────────────────────────

func cmdFollow(args []string, stdout io.Writer, kind string, allowed []string) error {
	fs := flag.NewFlagSet(kind, flag.ContinueOnError)
	c := addCommon(fs)
	note := fs.String("note", "", "why, in a sentence")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return usagef("usage: flagman %s <id|last> %s [--note TEXT]", kind, strings.Join(allowed, "|"))
	}
	ok := false
	for _, a := range allowed {
		ok = ok || pos[1] == a
	}
	if !ok {
		return usagef("%q is not one of %s", pos[1], strings.Join(allowed, ", "))
	}
	rr, err := c.readLog()
	if err != nil {
		return err
	}
	e, err := resolve(rr.Entries, pos[0])
	if err != nil {
		return err
	}
	l := decisionlog.Line{Kind: kind, ID: e.Decision.ID, At: time.Now().UTC(), Value: pos[1], Note: *note}
	if err := decisionlog.Append(c.logPath(), l); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "recorded %s=%s for %s\n", kind, pos[1], e.Decision.ID)
	e2 := *e
	switch kind {
	case decisionlog.KindHuman:
		e2.Human = &l
	case decisionlog.KindOutcome:
		e2.Outcome = &l
	case decisionlog.KindLabel:
		e2.Label = &l
	}
	if t, ok := decisionlog.TruthOf(&e2); ok && e.Decision.Result != nil {
		given := e.Decision.Result.Verdict
		fmt.Fprintf(stdout, "truth is now %s (from %s); the verdict given was %s", t.Verdict, t.Source, given)
		if given == score.Continue && t.Verdict == score.Defer {
			fmt.Fprint(stdout, " — a FALSE AUTONOMY: run `flagman suggest` to see which evidence would have caught it")
		}
		fmt.Fprintln(stdout)
	}
	return nil
}

func resolve(entries []*decisionlog.Entry, id string) (*decisionlog.Entry, error) {
	if id == "last" {
		if e := decisionlog.Last(entries); e != nil {
			return e, nil
		}
		return nil, errors.New("the log has no decisions yet")
	}
	return decisionlog.Find(entries, id)
}

// ── log / show ─────────────────────────────────────────────────────────────

func cmdLog(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	c := addCommon(fs)
	asJSON := fs.Bool("json", false, "print entries as JSON")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	rr, err := c.readLog()
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rr)
	}
	report.Log(stdout, rr.Entries, rr)
	return nil
}

func cmdShow(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	c := addCommon(fs)
	asJSON := fs.Bool("json", false, "print the entry as JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("usage: flagman show <id|last>")
	}
	rr, err := c.readLog()
	if err != nil {
		return err
	}
	e, err := resolve(rr.Entries, pos[0])
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(e)
	}
	var rep *bool
	if r := replay.Reproduce([]*decisionlog.Entry{e}); r.Checked == 1 {
		ok := r.Reproduced == 1
		rep = &ok
	}
	report.Show(stdout, e, rep)
	return nil
}

// ── replay / suggest ───────────────────────────────────────────────────────

func entriesFor(c *common, scenarios string) ([]*decisionlog.Entry, *decisionlog.ReadResult, string, error) {
	if scenarios != "" {
		es, err := replay.LoadScenarios(scenarios)
		return es, nil, "scenarios in " + scenarios, err
	}
	rr, err := c.readLog()
	if err != nil {
		return nil, nil, "", err
	}
	return rr.Entries, rr, "the decision log", nil
}

func cmdReplay(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	c := addCommon(fs)
	compare := fs.String("compare", "", "a second config to compare against the first")
	scen := fs.String("scenarios", "", "replay a directory of scenario files instead of the log")
	asJSON := fs.Bool("json", false, "print metrics as JSON")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	entries, rr, src, err := entriesFor(c, *scen)
	if err != nil {
		return err
	}
	cfgA, nameA, err := c.loadConfig()
	if err != nil {
		return err
	}
	scoredA, _ := replay.Run(entries, cfgA)
	mA := replay.Measure(scoredA, cfgA.Costs)
	rep := replay.Reproduce(entries)

	if *compare == "" {
		if *asJSON {
			return json.NewEncoder(stdout).Encode(map[string]any{"source": src, "config": nameA, "metrics": mA, "reproducibility": rep})
		}
		fmt.Fprintf(stdout, "replaying %s under %s (rules v%s)\n\n", src, nameA, decisionlog.RulesVersion)
		report.Metrics(stdout, "metrics", mA)
		fmt.Fprintln(stdout)
		report.Reproduction(stdout, rep)
		if rr != nil && (len(rr.Malformed) > 0 || len(rr.Orphans) > 0) {
			fmt.Fprintf(stdout, "! the log had %d unreadable line(s) and %d orphan follow-up(s), left out of every number above\n", len(rr.Malformed), len(rr.Orphans))
		}
		return nil
	}
	cfgB, err := config.Load(*compare)
	if err != nil {
		return err
	}
	scoredB, _ := replay.Run(entries, cfgB)
	// Costs are the yardstick, so both sides are measured with A's.
	mB := replay.Measure(scoredB, cfgA.Costs)
	changes := replay.Diff(scoredA, scoredB)
	if *asJSON {
		return json.NewEncoder(stdout).Encode(map[string]any{"source": src, "a": mA, "b": mB, "b_is": replay.Better(mA, mB), "changes": changes})
	}
	fmt.Fprintf(stdout, "replaying %s, comparing two configs (costs from A)\n\n", src)
	report.Compare(stdout, nameA, filepath.Base(*compare), mA, mB, changes)
	return nil
}

func cmdSuggest(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("suggest", flag.ContinueOnError)
	c := addCommon(fs)
	share := fs.Float64("test-share", 0.3, "share of the newest decisions held out to judge the proposal")
	write := fs.String("write", "", "write the current config with the proposed thresholds to FILE")
	scen := fs.String("scenarios", "", "use a directory of scenario files instead of the log")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	if *share <= 0 || *share >= 1 {
		return usagef("--test-share must be between 0 and 1")
	}
	entries, _, src, err := entriesFor(c, *scen)
	if err != nil {
		return err
	}
	cfg, name, err := c.loadConfig()
	if err != nil {
		return err
	}
	s, err := replay.SuggestThresholds(entries, cfg, *share)
	if err != nil {
		return err
	}
	scored, _ := replay.Run(entries, cfg)
	fmt.Fprintf(stdout, "suggesting from %s under %s\n\n", src, name)
	report.Suggest(stdout, s, [2]int{cfg.Thresholds.Review, cfg.Thresholds.Defer}, replay.Diagnose(scored))
	if *write != "" {
		nc := *cfg
		nc.Thresholds = config.Thresholds{Review: s.Review, Defer: s.Defer}
		if err := config.Save(*write, &nc); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "\nwrote %s. Compare before adopting it:\n  flagman replay --compare %s\n", *write, *write)
	}
	return nil
}

// ── rules / init ───────────────────────────────────────────────────────────

func cmdRules(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rules", flag.ContinueOnError)
	c := addCommon(fs)
	if _, err := parse(fs, args); err != nil {
		return err
	}
	cfg, name, err := c.loadConfig()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "rules v%s under %s: review at %d, defer at %d\n\n", decisionlog.RulesVersion, name, cfg.Thresholds.Review, cfg.Thresholds.Defer)
	for _, n := range rules.Names() {
		p, ok := cfg.Rules[n]
		state := fmt.Sprintf("%3d", p.Points)
		switch {
		case !ok:
			state = "  0 (not in config)"
		case p.Off:
			state = "off"
		case p.Hard:
			state = "HARD"
		}
		fmt.Fprintf(stdout, "  %-28s %s  %s\n", n, state, p.Why)
	}
	fmt.Fprintln(stdout, "\nsensitive paths")
	for _, g := range cfg.SensitivePaths {
		fmt.Fprintf(stdout, "  %-20s %3d  %s\n", g.Name, g.Points, strings.Join(g.Patterns, " "))
	}
	return nil
}

func cmdInit(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	c := addCommon(fs)
	force := fs.Bool("force", false, "overwrite an existing config")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	p := c.cfg
	if p == "" {
		p = filepath.Join(c.root(), ".flagman", "config.json")
	}
	if _, err := os.Stat(p); err == nil && !*force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", p)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := config.Save(p, config.Default()); err != nil {
		return err
	}
	ensureIgnored(c, c.logPath())
	fmt.Fprintf(stdout, "wrote %s with the default rules and thresholds.\n", p)
	fmt.Fprintln(stdout, "Set evidence.tests.command to your test command to use --run-tests.")
	fmt.Fprintln(stdout, "The decision log (.flagman/decisions.jsonl) is excluded in .git/info/exclude; commit the config.")
	return nil
}
