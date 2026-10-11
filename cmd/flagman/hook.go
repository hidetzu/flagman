package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/hidetzu/flagman/internal/report"
	"github.com/hidetzu/flagman/internal/score"
	"github.com/hidetzu/flagman/internal/snapshot"
)

// cmdHook adapts flagman to an agent's hook protocol. Only Claude Code's PreToolUse is
// implemented; the core does not know about it (docs/adr/0008).
//
// It never blocks the agent by failing: any error prints a note on stderr and exits 0, which
// leaves the agent's own permission flow in charge. It never answers "allow" either — that
// would widen what the agent may do. It answers "ask" on defer, shows the reasons without a
// permission decision on review, and stays silent on continue.
func cmdHook(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "claude-code" {
		fmt.Fprintln(stderr, "flagman hook: only `flagman hook claude-code` is implemented")
		return exitUsage
	}
	fs := flag.NewFlagSet("hook", flag.ContinueOnError)
	c := addCommon(fs)
	runTests := fs.Bool("run-tests", false, "run evidence.tests.command before judging")
	task := fs.String("task", "", "task file or owner/repo#N")
	if _, err := parse(fs, args[1:]); err != nil {
		fmt.Fprintf(stderr, "flagman hook: %v\n", err)
		return 0
	}
	var in struct {
		ToolName  string `json:"tool_name"`
		Cwd       string `json:"cwd"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&in); err != nil {
		fmt.Fprintf(stderr, "flagman hook: the hook input is not the expected JSON (%v); no opinion given\n", err)
		return 0
	}
	if in.ToolName != "Bash" {
		return 0
	}
	action := ActionForCommand(in.ToolInput.Command)
	if action == "" {
		return 0
	}
	if c.dir == "." && in.Cwd != "" {
		c.dir = in.Cwd
	}
	// The raw command is not stored: it can carry tokens or messages the owner did not mean to
	// log. The action it maps to is.
	l, err := decide(c, checkOpts{action: action, task: *task, runTests: *runTests, intent: "claude-code PreToolUse: " + action})
	if err != nil {
		fmt.Fprintf(stderr, "flagman hook: %v; no opinion given\n", err)
		return 0
	}
	msg := report.HookMessage(l)
	var out map[string]any
	switch l.Result.Verdict {
	case score.Defer:
		out = map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "ask",
			"permissionDecisionReason": msg,
		}}
	case score.Review:
		// No permissionDecision: the agent's own permission flow stays in charge. The human sees
		// the reasons (systemMessage); the agent gets them too (additionalContext), so it can
		// point the human at them.
		out = map[string]any{
			"systemMessage": msg,
			"hookSpecificOutput": map[string]any{
				"hookEventName":     "PreToolUse",
				"additionalContext": msg,
			},
		}
	default:
		return 0
	}
	_ = json.NewEncoder(stdout).Encode(out)
	return 0
}

var (
	segSplit = regexp.MustCompile(`&&|\|\||;|\||\n`)
	ws       = regexp.MustCompile(`\s+`)
)

// ActionForCommand maps a shell command line to the most irreversible action it contains, or ""
// when it contains none flagman judges.
func ActionForCommand(cmdline string) string {
	best := -1
	for _, seg := range segSplit.Split(cmdline, -1) {
		f := ws.Split(strings.TrimSpace(seg), -1)
		// Skip leading environment assignments (FOO=bar git push).
		for len(f) > 0 && strings.Contains(f[0], "=") && !strings.HasPrefix(f[0], "-") {
			f = f[1:]
		}
		if a := actionFor(f); a != "" {
			if i := rank(a); i > best {
				best = i
			}
		}
	}
	if best < 0 {
		return ""
	}
	return snapshot.KnownActions[best]
}

func rank(a string) int {
	for i, k := range snapshot.KnownActions {
		if k == a {
			return i
		}
	}
	return -1
}

func has(f []string, xs ...string) bool {
	for _, a := range f {
		for _, x := range xs {
			if a == x || strings.HasPrefix(a, x+"=") {
				return true
			}
		}
	}
	return false
}

func actionFor(f []string) string {
	if len(f) < 2 {
		return ""
	}
	switch f[0] {
	case "git":
		// Skip git's own options (git -C dir push).
		i := 1
		for i < len(f) && strings.HasPrefix(f[i], "-") {
			if f[i] == "-C" || f[i] == "-c" {
				i++
			}
			i++
		}
		if i >= len(f) {
			return ""
		}
		sub, rest := f[i], f[i+1:]
		switch sub {
		case "commit":
			return snapshot.ActionCommit
		case "push":
			if has(rest, "--force", "-f", "--force-with-lease", "--mirror") || hasPlusRefspec(rest) {
				return snapshot.ActionForcePush
			}
			if has(rest, "--delete", "-d") {
				return snapshot.ActionDelete
			}
			return snapshot.ActionPush
		case "merge":
			return snapshot.ActionMerge
		case "reset":
			if has(rest, "--hard") {
				return snapshot.ActionDelete
			}
		case "clean":
			for _, a := range rest {
				if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "f") {
					return snapshot.ActionDelete
				}
			}
			if has(rest, "--force") {
				return snapshot.ActionDelete
			}
		case "branch":
			if has(rest, "-D") {
				return snapshot.ActionDelete
			}
		case "tag":
			return snapshot.ActionRelease
		}
	case "gh":
		if len(f) >= 3 {
			switch f[1] + " " + f[2] {
			case "pr create":
				return snapshot.ActionPR
			case "pr merge":
				return snapshot.ActionMerge
			case "release create", "release upload":
				return snapshot.ActionRelease
			case "repo delete", "release delete":
				return snapshot.ActionDelete
			}
		}
	case "rm":
		for _, a := range f[1:] {
			if strings.HasPrefix(a, "-") && strings.Contains(a, "r") && strings.Contains(a, "f") {
				return snapshot.ActionDelete
			}
		}
	}
	return ""
}

func hasPlusRefspec(rest []string) bool {
	for _, a := range rest {
		if strings.HasPrefix(a, "+") {
			return true
		}
	}
	return false
}
