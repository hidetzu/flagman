package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestActionForCommand(t *testing.T) {
	cases := map[string]string{
		"git status":                          "",
		"ls -la && go test ./...":             "",
		"git commit -m 'x'":                   "commit",
		"git add . && git commit -m x":        "commit",
		"git push origin feat":                "push",
		"git push --force origin main":        "force-push",
		"git push origin +main":               "force-push",
		"git push --force-with-lease":         "force-push",
		"git push origin --delete old":        "delete",
		"git -C sub push":                     "push",
		"gh pr create --fill":                 "pr",
		"gh pr merge 3 --squash":              "merge",
		"gh release create v1.0.0":            "release",
		"git reset --hard HEAD~1":             "delete",
		"git clean -fd":                       "delete",
		"rm -rf build":                        "delete",
		"rm build.log":                        "",
		"GIT_TRACE=1 git push":                "push",
		"git commit -m x && git push --force": "force-push",
		"echo 'git push' ":                    "",
	}
	for in, want := range cases {
		if got := ActionForCommand(in); got != want {
			t.Errorf("ActionForCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHookNeverFailsTheAgent(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"hook", "claude-code"}, strings.NewReader("{not json"), &out, &errb)
	if code != 0 || out.Len() != 0 {
		t.Fatalf("bad input must give exit 0 and no opinion; got %d %q", code, out.String())
	}
	out.Reset()
	code = run([]string{"hook", "claude-code"}, strings.NewReader(`{"tool_name":"Read","tool_input":{}}`), &out, &errb)
	if code != 0 || out.Len() != 0 {
		t.Fatalf("a non-Bash tool must get no opinion; got %d %q", code, out.String())
	}
}

func TestUsageExitCodes(t *testing.T) {
	var out, errb bytes.Buffer
	if c := run(nil, nil, &out, &errb); c != exitUsage {
		t.Errorf("no args: %d", c)
	}
	if c := run([]string{"frobnicate"}, nil, &out, &errb); c != exitUsage {
		t.Errorf("unknown command: %d", c)
	}
	if c := run([]string{"check", "--action", "yolo", "--no-log"}, nil, &out, &errb); c != exitUsage {
		t.Errorf("unknown action: %d", c)
	}
	if c := run([]string{"--help"}, nil, &out, &errb); c != 0 {
		t.Errorf("--help: %d", c)
	}
}
