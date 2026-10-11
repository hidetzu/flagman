// Package collect reads the evidence a decision is made from, and nothing else.
//
// It is the only package that touches the outside: the git working copy, the project's own
// test command, a task file, the decision-record directory, and — only when the caller names an
// issue — the `gh` CLI. Every source that cannot be read is recorded in Snapshot.Missing with
// the reason, never left out silently (.claude/rules/evidence.md: could not be obtained ≠ not
// there).
//
// Nothing here sends repository contents anywhere. Reading an issue runs `gh issue view`,
// which sends the issue reference to GitHub and nothing from the working copy
// (docs/adr/0007).
package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hidetzu/flagman/internal/config"
	"github.com/hidetzu/flagman/internal/snapshot"
)

// Options say what to collect.
type Options struct {
	Dir    string // the working copy
	Action string
	Intent string
	Base   string // diff base; empty = config, then the default branch
	// Task is a file path or "owner/repo#N". Empty = no task.
	Task string
	// Tests is "pass", "fail" or "not-run" when the caller already knows; empty = unknown.
	Tests string
	// RunTests runs config.Evidence.Tests.Command. It runs the project's own command in the
	// project's own directory.
	RunTests bool
	// ExternalFile is a JSON file of external findings.
	ExternalFile string
	Config       *config.Config
	Now          time.Time
	// IssueFetcher reads an issue; nil = the `gh` CLI. Replaced in tests.
	IssueFetcher func(ctx context.Context, repo string, number int) (*Issue, error)
}

// Issue is what is read from an issue tracker.
type Issue struct {
	State    string
	Labels   []string
	Body     string
	Comments []string
}

// Collect builds a Snapshot. It never fails as a whole: a source that cannot be read is
// recorded as missing and the rest goes on.
func Collect(ctx context.Context, o Options) *snapshot.Snapshot {
	s := &snapshot.Snapshot{TakenAt: o.Now.UTC(), Action: o.Action, Intent: o.Intent}
	miss := func(src, reason string) {
		s.Missing = append(s.Missing, snapshot.Missing{Source: src, Reason: reason})
	}

	repo, err := readRepo(ctx, o)
	if err != nil {
		miss(snapshot.SourceRepo, err.Error())
		miss(snapshot.SourceDiff, "the repository could not be read")
		miss(snapshot.SourceHistory, "the repository could not be read")
	} else {
		s.Repo = repo
		diff, err := readDiff(ctx, o, repo)
		if err != nil {
			miss(snapshot.SourceDiff, err.Error())
		} else {
			s.Diff = diff
			hist, err := readHistory(ctx, o, diff)
			if err != nil {
				miss(snapshot.SourceHistory, err.Error())
			} else {
				s.History = hist
			}
		}
	}

	switch {
	case o.Tests != "":
		s.Tests = &snapshot.Tests{Status: o.Tests, Source: "flag"}
	case o.RunTests:
		t, err := runTests(ctx, o)
		if err != nil {
			miss(snapshot.SourceTests, err.Error())
		} else {
			s.Tests = t
		}
	default:
		miss(snapshot.SourceTests, "no test result was given (--tests) and the tests were not run (--run-tests)")
	}

	if o.Task != "" {
		t, err := readTask(ctx, o)
		if err != nil {
			miss(snapshot.SourceTask, err.Error())
		} else {
			s.Task = t
		}
	}

	if s.Diff != nil {
		a, err := ReadADRs(filepath.Join(o.Dir, o.Config.Evidence.ADRDir), o.Config.Evidence.ADRDir, s.Diff.Files)
		if err != nil {
			miss(snapshot.SourceADRs, err.Error())
		} else {
			s.ADRs = a
		}
	} else {
		miss(snapshot.SourceADRs, "there is no diff to compare the decision records against")
	}

	if o.ExternalFile != "" {
		ext, err := ReadExternal(o.ExternalFile)
		if err != nil {
			miss("external", err.Error())
		} else {
			s.External = ext
		}
	}
	return s
}

// ── git ────────────────────────────────────────────────────────────────────

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], firstLine(msg))
	}
	return out.String(), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// emptyTree is git's well-known empty tree, used as the base of a repository with no commits.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

func readRepo(ctx context.Context, o Options) (*snapshot.Repo, error) {
	top, err := git(ctx, o.Dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, errors.New("not a git working copy (" + err.Error() + ")")
	}
	r := &snapshot.Repo{Root: filepath.Base(strings.TrimSpace(top))}
	if b, err := git(ctx, o.Dir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		r.Branch = strings.TrimSpace(b)
	} else {
		r.Branch = "(detached)"
	}
	if h, err := git(ctx, o.Dir, "rev-parse", "--short", "HEAD"); err == nil {
		r.Head = strings.TrimSpace(h)
	}
	if st, err := git(ctx, o.Dir, "status", "--porcelain"); err == nil {
		r.Dirty = strings.TrimSpace(st) != ""
	}
	r.DefaultBranch = defaultBranch(ctx, o.Dir)
	return r, nil
}

func defaultBranch(ctx context.Context, dir string) string {
	if out, err := git(ctx, dir, "symbolic-ref", "--short", "-q", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(strings.TrimSpace(out), "origin/")
	}
	for _, b := range []string{"main", "master"} {
		if _, err := git(ctx, dir, "rev-parse", "--verify", "-q", "refs/heads/"+b); err == nil {
			return b
		}
	}
	return ""
}

func resolveBase(ctx context.Context, o Options, r *snapshot.Repo) string {
	if o.Base != "" {
		return o.Base
	}
	if o.Config.Evidence.DefaultBase != "" {
		return o.Config.Evidence.DefaultBase
	}
	if r.DefaultBranch != "" {
		if _, err := git(ctx, o.Dir, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+r.DefaultBranch); err == nil {
			return "origin/" + r.DefaultBranch
		}
		return r.DefaultBranch
	}
	return "HEAD"
}

func readDiff(ctx context.Context, o Options, r *snapshot.Repo) (*snapshot.Diff, error) {
	base := resolveBase(ctx, o, r)
	from := emptyTree
	if r.Head != "" {
		mb, err := git(ctx, o.Dir, "merge-base", base, "HEAD")
		if err != nil {
			return nil, fmt.Errorf("cannot compare against base %q: %v", base, err)
		}
		from = strings.TrimSpace(mb)
	}
	num, err := git(ctx, o.Dir, "diff", "--numstat", "-z", "-M", from)
	if err != nil {
		return nil, err
	}
	ns, err := git(ctx, o.Dir, "diff", "--name-status", "-z", "-M", from)
	if err != nil {
		return nil, err
	}
	files := ParseNumstatZ(num)
	status := ParseNameStatusZ(ns)
	for i := range files {
		if st, ok := status[files[i].Path]; ok {
			files[i].Status = st
		} else {
			files[i].Status = "M"
		}
	}
	untracked, err := git(ctx, o.Dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range strings.Split(untracked, "\x00") {
		if p == "" {
			continue
		}
		f := snapshot.ChangedFile{Path: p, Status: "?"}
		f.Added, f.Binary = countLines(filepath.Join(o.Dir, p))
		files = append(files, f)
	}
	d := &snapshot.Diff{Base: base, Files: files}
	for _, f := range files {
		d.Insertions += f.Added
		d.Deletions += f.Removed
	}
	return d, nil
}

// countLines counts newlines in a file up to 1 MiB; larger or unreadable files count as zero
// and binary files (a NUL byte) are flagged.
func countLines(p string) (int, bool) {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() || fi.Size() > 1<<20 {
		return 0, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, false
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return 0, true
	}
	n := bytes.Count(b, []byte{'\n'})
	if len(b) > 0 && b[len(b)-1] != '\n' {
		n++
	}
	return n, false
}

// ParseNumstatZ parses `git diff --numstat -z`. Renames come as "a\td\t\0old\0new\0".
func ParseNumstatZ(out string) []snapshot.ChangedFile {
	var files []snapshot.ChangedFile
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		rec := parts[i]
		if rec == "" {
			continue
		}
		fields := strings.SplitN(rec, "\t", 3)
		if len(fields) < 3 {
			continue
		}
		f := snapshot.ChangedFile{}
		if fields[0] == "-" {
			f.Binary = true
		} else {
			f.Added, _ = strconv.Atoi(fields[0])
			f.Removed, _ = strconv.Atoi(fields[1])
		}
		if fields[2] == "" { // rename: next two records are old and new path
			if i+2 < len(parts) {
				f.Path = parts[i+2]
				i += 2
			}
		} else {
			f.Path = fields[2]
		}
		if f.Path != "" {
			files = append(files, f)
		}
	}
	return files
}

// ParseNameStatusZ parses `git diff --name-status -z` into path → one-letter status.
func ParseNameStatusZ(out string) map[string]string {
	m := map[string]string{}
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		st := parts[i]
		if st == "" {
			continue
		}
		letter := st[:1]
		if letter == "R" || letter == "C" {
			if i+2 < len(parts) {
				m[parts[i+2]] = letter
				i += 2
			}
			continue
		}
		if i+1 < len(parts) {
			m[parts[i+1]] = letter
			i++
		}
	}
	return m
}

func readHistory(ctx context.Context, o Options, d *snapshot.Diff) (*snapshot.History, error) {
	days := o.Config.Evidence.HistoryWindowDays
	h := &snapshot.History{WindowDays: days, Touches: map[string]int{}}
	if len(d.Files) == 0 {
		return h, nil
	}
	since := o.Now.Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
	out, err := git(ctx, o.Dir, "log", "--since="+since, "-n", "5000", "--format=\x01%h\t%s", "--name-only", "HEAD")
	if err != nil {
		// A repository with no commits has no history; that is an observation, not a failure.
		// git has no typed errors, so its stderr text is matched here (.claude/rules/go.md).
		if strings.Contains(err.Error(), "does not have any commits") || strings.Contains(err.Error(), "unknown revision") {
			return h, nil
		}
		return nil, err
	}
	changed := map[string]bool{}
	for _, f := range d.Files {
		changed[f.Path] = true
	}
	h.Touches, h.Reverts = ParseLog(out, changed)
	return h, nil
}

// ParseLog counts commits per changed path and collects revert commits that touched one.
func ParseLog(out string, changed map[string]bool) (map[string]int, []snapshot.Revert) {
	touches := map[string]int{}
	var reverts []snapshot.Revert
	for _, block := range strings.Split(out, "\x01") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		head := strings.SplitN(lines[0], "\t", 2)
		subject := ""
		if len(head) == 2 {
			subject = head[1]
		}
		var hit []string
		for _, l := range lines[1:] {
			l = strings.TrimSpace(l)
			if l != "" && changed[l] {
				touches[l]++
				hit = append(hit, l)
			}
		}
		if len(hit) > 0 && strings.HasPrefix(subject, "Revert") {
			reverts = append(reverts, snapshot.Revert{Commit: head[0], Subject: subject, Paths: hit})
		}
	}
	return touches, reverts
}

// ── tests ──────────────────────────────────────────────────────────────────

// DefaultTestTimeout bounds the test command when the config sets none.
const DefaultTestTimeout = 10 * time.Minute

func runTests(ctx context.Context, o Options) (*snapshot.Tests, error) {
	cmdline := o.Config.Evidence.Tests.Command
	if cmdline == "" {
		return nil, errors.New("--run-tests was given but evidence.tests.command is empty in the config")
	}
	timeout := DefaultTestTimeout
	if s := o.Config.Evidence.Tests.TimeoutSeconds; s > 0 {
		timeout = time.Duration(s) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdline)
	cmd.Dir = o.Dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	t := &snapshot.Tests{Source: "command", Command: cmdline, Summary: lastLine(out.String())}
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		// A timeout is the absence of an answer, not a failing answer.
		return nil, fmt.Errorf("the test command did not finish within %s", timeout)
	case err == nil:
		t.Status = snapshot.TestsPass
	default:
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return nil, fmt.Errorf("the test command could not start: %v", err)
		}
		t.Status = snapshot.TestsFail
	}
	return t, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			if len(l) > 200 {
				l = l[:200] + "…"
			}
			return l
		}
	}
	return ""
}

// ── external ───────────────────────────────────────────────────────────────

// ReadExternal reads a JSON array of findings handed in from another tool.
func ReadExternal(p string) ([]snapshot.ExternalFinding, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var fs []snapshot.ExternalFinding
	if err := json.Unmarshal(b, &fs); err != nil {
		return nil, fmt.Errorf("%s is not a JSON array of findings: %v", filepath.Base(p), err)
	}
	for i, f := range fs {
		if f.ID == "" || f.Summary == "" {
			return nil, fmt.Errorf("%s: finding %d needs an id and a summary", filepath.Base(p), i+1)
		}
	}
	return fs, nil
}
