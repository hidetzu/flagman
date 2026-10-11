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
	"regexp"
	"strconv"
	"strings"

	"github.com/hidetzu/flagman/internal/snapshot"
)

var issueRef = regexp.MustCompile(`^([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)#([0-9]+)$`)

func readTask(ctx context.Context, o Options) (*snapshot.Task, error) {
	if m := issueRef.FindStringSubmatch(o.Task); m != nil {
		n, _ := strconv.Atoi(m[2])
		fetch := o.IssueFetcher
		if fetch == nil {
			fetch = ghIssue
		}
		is, err := fetch(ctx, m[1], n)
		if err != nil {
			return nil, err
		}
		docs := []doc{{where: o.Task, text: is.Body}}
		for i, c := range is.Comments {
			docs = append(docs, doc{where: fmt.Sprintf("%s comment %d", o.Task, i+1), text: c})
		}
		t := ParseTask(o.Task, docs)
		t.State = strings.ToLower(is.State)
		t.Labels = is.Labels
		for _, l := range is.Labels {
			if undecidedLabel[strings.ToLower(l)] {
				t.OpenDecisions = append(t.OpenDecisions, snapshot.Quote{Where: o.Task + " label", Text: l})
			}
		}
		return t, nil
	}
	p := o.Task
	if !filepath.IsAbs(p) {
		p = filepath.Join(o.Dir, p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("the task file %s does not exist (a task is a file path or owner/repo#N)", o.Task)
		}
		return nil, err
	}
	// The ref is the path as the diff would name it, so rules can tell the task file from the
	// change (a task file is not a change outside its own scope).
	ref := filepath.ToSlash(filepath.Clean(o.Task))
	if filepath.IsAbs(o.Task) {
		ref = filepath.Base(o.Task)
	}
	return ParseTask(ref, []doc{{where: ref, text: string(b)}}), nil
}

// Labels that mean a human has not decided yet.
var undecidedLabel = map[string]bool{
	"needs-decision": true, "needs decision": true, "question": true, "needs-info": true,
	"needs info": true, "blocked": true, "discussion": true, "needs-discussion": true,
}

func ghIssue(ctx context.Context, repo string, n int) (*Issue, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errors.New("reading an issue needs the gh CLI, which is not installed")
	}
	cmd := exec.CommandContext(ctx, "gh", "issue", "view", strconv.Itoa(n), "-R", repo,
		"--json", "state,labels,body,comments")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh issue view %s#%d: %s", repo, n, firstLine(strings.TrimSpace(errb.String())))
	}
	var raw struct {
		State  string `json:"state"`
		Body   string `json:"body"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		Comments []struct {
			Body string `json:"body"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		return nil, fmt.Errorf("gh issue view returned something that is not the expected JSON: %v", err)
	}
	is := &Issue{State: raw.State, Body: raw.Body}
	for _, l := range raw.Labels {
		is.Labels = append(is.Labels, l.Name)
	}
	for _, c := range raw.Comments {
		is.Comments = append(is.Comments, c.Body)
	}
	return is, nil
}

type doc struct {
	where string
	text  string
}

type section int

const (
	secNone section = iota
	secGoal
	secScope
	secOutOfScope
	secAC
	secHumanDecision
	secOpenQuestions
	secOwnerDecisions
)

// Headings are matched on their lower-cased text with leading numbering removed.
var headingWords = []struct {
	sec   section
	words []string
}{
	// Order matters: "out of scope" must be tested before "scope".
	{secOutOfScope, []string{"out of scope", "out-of-scope", "non-goals", "non goals", "not in scope", "対象外", "スコープ外", "やらないこと"}},
	{secScope, []string{"scope", "対象範囲", "範囲", "変更範囲", "対象"}},
	{secGoal, []string{"goal", "goals", "problem", "summary", "目的", "概要", "ゴール"}},
	{secAC, []string{"acceptance criteria", "acceptance", "definition of done", "done when", "受け入れ条件", "完了条件", "受入条件"}},
	{secHumanDecision, []string{"human decision", "human decisions", "decision needed", "decisions needed", "人間の判断", "要判断", "人間が決めること"}},
	{secOpenQuestions, []string{"open questions", "open question", "unresolved", "未決事項", "未解決", "論点"}},
	{secOwnerDecisions, []string{"owner decisions", "owner decision", "decided", "決定事項"}},
}

var (
	headingRe  = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.*?)\s*#*\s*$`)
	numberRe   = regexp.MustCompile(`^[0-9.]+\s*`)
	taskListRe = regexp.MustCompile(`^\s*[-*]\s+\[[ xX]\]\s+`)
	backtickRe = regexp.MustCompile("`([^`]+)`")
	pathLikeRe = regexp.MustCompile(`(?:^|\s)((?:\.{0,2}/)?[A-Za-z0-9_.*-]+(?:/[A-Za-z0-9_.*-]*)+|[A-Za-z0-9_*-]+\.[A-Za-z0-9]{1,6})(?:$|[\s,;:)])`)
	// Words that mark an undecided point anywhere in the text.
	undecidedRe = regexp.MustCompile(`(?i)\bTBD\b|\bTBC\b|\bundecided\b|\bnot yet decided\b|\bneeds? (?:a |an owner |the owner'?s? )?decision\b|未定|要決定|要判断|決めていない|未決定`)
	noneRe      = regexp.MustCompile(`(?i)^(?:[-*]\s*)?(?:none|n/a|no|nothing|なし|無し|特になし|ありません|—|-)\.?$`)
)

func classify(heading string) section {
	h := strings.ToLower(strings.TrimSpace(numberRe.ReplaceAllString(heading, "")))
	h = strings.Trim(h, ":：*_ ")
	for _, hw := range headingWords {
		for _, w := range hw.words {
			if h == w || strings.HasPrefix(h, w+" ") || strings.HasPrefix(h, w+"(") || strings.HasPrefix(h, w+"（") || strings.HasPrefix(h, w+":") {
				return hw.sec
			}
		}
	}
	return secNone
}

// ParseTask reads markdown documents (an issue body and its comments, or a task file) for the
// sections a task should have. It never guesses intent: a section counts only when its heading
// is present and something non-empty sits under it.
func ParseTask(ref string, docs []doc) *snapshot.Task {
	t := &snapshot.Task{Ref: ref}
	for _, d := range docs {
		cur := secNone
		inFence := false
		for i, raw := range strings.Split(d.text, "\n") {
			line := strings.TrimRight(raw, "\r")
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			if m := headingRe.FindStringSubmatch(line); m != nil {
				cur = classify(m[1])
				continue
			}
			if trimmed == "" || strings.HasPrefix(trimmed, "<!--") {
				continue
			}
			where := fmt.Sprintf("%s:%d", d.where, i+1)
			if taskListRe.MatchString(line) {
				t.HasAcceptanceCriteria = true
			}
			switch cur {
			case secGoal:
				t.HasGoal = true
			case secScope:
				t.HasScope = true
				t.Scope = append(t.Scope, pathTokens(trimmed)...)
			case secOutOfScope:
				t.HasOutOfScope = true
				t.OutOfScope = append(t.OutOfScope, pathTokens(trimmed)...)
			case secAC:
				t.HasAcceptanceCriteria = true
			case secHumanDecision, secOpenQuestions:
				if !noneRe.MatchString(trimmed) {
					t.OpenDecisions = append(t.OpenDecisions, snapshot.Quote{Where: where, Text: clip(trimmed)})
					continue
				}
			}
			// Owner decisions are settled by definition; their wording is not scanned.
			if cur != secOwnerDecisions && undecidedRe.MatchString(trimmed) {
				t.OpenDecisions = append(t.OpenDecisions, snapshot.Quote{Where: where, Text: clip(trimmed)})
			}
		}
	}
	t.Scope = dedup(t.Scope)
	t.OutOfScope = dedup(t.OutOfScope)
	return t
}

// pathTokens pulls path-like tokens out of one line: backticked text first, then bare tokens
// that contain a slash or a file extension.
func pathTokens(line string) []string {
	var out []string
	for _, m := range backtickRe.FindAllStringSubmatch(line, -1) {
		tok := strings.TrimSpace(m[1])
		if tok != "" && !strings.Contains(tok, " ") {
			out = append(out, strings.TrimPrefix(tok, "./"))
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, m := range pathLikeRe.FindAllStringSubmatch(line, -1) {
		tok := strings.TrimPrefix(m[1], "./")
		if strings.HasPrefix(tok, "http") || strings.Contains(tok, "://") {
			continue
		}
		out = append(out, tok)
	}
	return out
}

func clip(s string) string {
	if r := []rune(s); len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return s
}

func dedup(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
