package collect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hidetzu/flagman/internal/snapshot"
)

func TestParseNumstatZ(t *testing.T) {
	out := "3\t1\ta.go\x00-\t-\timg.png\x0010\t0\t\x00old/b.go\x00new/b.go\x00"
	fs := ParseNumstatZ(out)
	if len(fs) != 3 {
		t.Fatalf("want 3 files, got %d: %+v", len(fs), fs)
	}
	if fs[0].Path != "a.go" || fs[0].Added != 3 || fs[0].Removed != 1 {
		t.Errorf("plain: %+v", fs[0])
	}
	if !fs[1].Binary || fs[1].Path != "img.png" {
		t.Errorf("binary: %+v", fs[1])
	}
	if fs[2].Path != "new/b.go" || fs[2].Added != 10 {
		t.Errorf("rename should take the new path: %+v", fs[2])
	}
}

func TestParseNameStatusZ(t *testing.T) {
	m := ParseNameStatusZ("M\x00a.go\x00D\x00gone.go\x00R100\x00old.go\x00new.go\x00A\x00add.go\x00")
	want := map[string]string{"a.go": "M", "gone.go": "D", "new.go": "R", "add.go": "A"}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s: got %q want %q", k, m[k], v)
		}
	}
	if _, ok := m["old.go"]; ok {
		t.Error("a rename's old path must not appear as changed")
	}
}

func TestParseLog(t *testing.T) {
	out := "\x01abc\tfix lexer\na.go\nb.go\n\x01def\tRevert \"x\"\na.go\n\x01ghi\tunrelated\nc.go\n"
	touches, reverts := ParseLog(out, map[string]bool{"a.go": true})
	if touches["a.go"] != 2 || touches["c.go"] != 0 {
		t.Fatalf("touches: %v", touches)
	}
	if len(reverts) != 1 || reverts[0].Commit != "def" || reverts[0].Paths[0] != "a.go" {
		t.Fatalf("reverts: %+v", reverts)
	}
}

func TestParseTaskEnglish(t *testing.T) {
	md := `# Fix the lexer

## Goal
Numbers with exponents lex correctly.

## Scope
- ` + "`internal/lexer/`" + `
- cmd/tool/main.go

## Out of Scope
- ` + "`internal/billing/`" + `

## Acceptance Criteria
- [ ] 1e10 lexes as one token

## Human Decision
None

` + "```" + `
## Scope
not a heading inside a fence: TBD
` + "```" + `
`
	tk := ParseTask("TASK.md", []doc{{"TASK.md", md}})
	if !tk.HasGoal || !tk.HasScope || !tk.HasOutOfScope || !tk.HasAcceptanceCriteria {
		t.Fatalf("sections not found: %+v", tk)
	}
	if strings.Join(tk.Scope, ",") != "internal/lexer/,cmd/tool/main.go" {
		t.Errorf("scope tokens: %v", tk.Scope)
	}
	if strings.Join(tk.OutOfScope, ",") != "internal/billing/" {
		t.Errorf("out-of-scope tokens: %v", tk.OutOfScope)
	}
	if len(tk.OpenDecisions) != 0 {
		t.Errorf("'None' under Human Decision, and TBD inside a fence, are not open decisions: %+v", tk.OpenDecisions)
	}
}

func TestParseTaskJapaneseAndOpenDecisions(t *testing.T) {
	md := "## 目的\n速くする。上限値は未定\n\n## 対象外\n- web/\n\n## 未決事項\n- エラー形式をどうするか\n\n## 決定事項\n- TBD という語はここでは決定済みの文脈\n"
	tk := ParseTask("t.md", []doc{{"t.md", md}})
	if !tk.HasGoal || !tk.HasOutOfScope || tk.HasScope {
		t.Fatalf("sections: %+v", tk)
	}
	if len(tk.OpenDecisions) != 2 {
		t.Fatalf("want 2 open decisions (the open question, and 未定 in the body), got %+v", tk.OpenDecisions)
	}
	if !strings.HasPrefix(tk.OpenDecisions[0].Where, "t.md:") {
		t.Errorf("an open decision must say where it was found: %+v", tk.OpenDecisions[0])
	}
}

func TestParseTaskWithoutHeadingsFindsNothing(t *testing.T) {
	tk := ParseTask("t.md", []doc{{"t.md", "please make it faster, scope is the parser"}})
	if tk.HasScope || tk.HasAcceptanceCriteria || len(tk.OpenDecisions) != 0 {
		t.Fatalf("prose without headings must not be read as sections: %+v", tk)
	}
}

func TestReadADRs(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "index naming internal/store/cache.go\n")
	write("0001-cache.md", "# 1\nThe cache in `internal/store` stays write-through.\n")
	write("0002-other.md", "nothing relevant\n")
	files := []snapshot.ChangedFile{{Path: "internal/store/cache.go"}, {Path: "README.md"}}
	a, err := ReadADRs(dir, "docs/adr", files)
	if err != nil {
		t.Fatal(err)
	}
	if a.Count != 2 {
		t.Errorf("README.md is the index and must not be read as a record; count=%d", a.Count)
	}
	if len(a.Mentions) != 1 || a.Mentions[0].ADR != "0001-cache.md" || a.Mentions[0].Line != 2 {
		t.Fatalf("mentions: %+v", a.Mentions)
	}
	missing, err := ReadADRs(filepath.Join(dir, "nope"), "nope", files)
	if err != nil || missing.Count != 0 {
		t.Fatalf("a missing ADR dir is zero records, not an error: %+v %v", missing, err)
	}
}

func TestReadExternal(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "g.json")
	os.WriteFile(good, []byte(`[{"id":"x","summary":"y","points":5}]`), 0o644)
	if fs, err := ReadExternal(good); err != nil || len(fs) != 1 {
		t.Fatalf("good file: %v %v", fs, err)
	}
	bad := filepath.Join(dir, "b.json")
	os.WriteFile(bad, []byte(`[{"points":5}]`), 0o644)
	if _, err := ReadExternal(bad); err == nil {
		t.Fatal("a finding without id and summary must be rejected")
	}
}

func TestIssueStateAndLabelsAreRead(t *testing.T) {
	o := Options{Task: "acme/app#3", IssueFetcher: func(_ context.Context, repo string, n int) (*Issue, error) {
		if repo != "acme/app" || n != 3 {
			t.Fatalf("fetched %s#%d", repo, n)
		}
		return &Issue{State: "MERGED", Labels: []string{"Needs-Decision"}, Body: "## Scope\n- `a/`\n", Comments: []string{"## Out of Scope\n- `b/`"}}, nil
	}}
	tk, err := readTask(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if tk.State != "merged" || !tk.HasScope || !tk.HasOutOfScope {
		t.Fatalf("state is lower-cased and comments are read too: %+v", tk)
	}
	if len(tk.OpenDecisions) != 1 || !strings.Contains(tk.OpenDecisions[0].Where, "label") {
		t.Fatalf("a needs-decision label is an open decision: %+v", tk.OpenDecisions)
	}
}
