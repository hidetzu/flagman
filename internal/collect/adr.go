package collect

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hidetzu/flagman/internal/snapshot"
)

// ReadADRs scans every markdown file in dir for lines that name a changed path, either the
// full path or its directory written as "dir/" or `dir`. A missing directory is an
// observation (the project keeps no decision records), reported as zero records read.
func ReadADRs(dir, display string, files []snapshot.ChangedFile) (*snapshot.ADRs, error) {
	a := &snapshot.ADRs{Dir: display}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return a, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		// README.md is the directory's index, not a decision: it names every file it lists.
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".md") && !strings.EqualFold(e.Name(), "README.md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		a.Count++
		seen := map[string]bool{}
		for i, line := range strings.Split(string(b), "\n") {
			for _, f := range files {
				if seen[f.Path] || !mentions(line, f.Path) {
					continue
				}
				// A record naming itself (an ADR changed in this diff) is not a boundary.
				if path.Base(f.Path) == name {
					continue
				}
				seen[f.Path] = true
				a.Mentions = append(a.Mentions, snapshot.ADRMention{ADR: name, Line: i + 1, Path: f.Path, Text: clip(strings.TrimSpace(line))})
			}
		}
	}
	return a, nil
}

func mentions(line, p string) bool {
	if strings.Contains(line, p) {
		return true
	}
	dir := path.Dir(p)
	if dir == "." || len(dir) < 3 {
		return false
	}
	return strings.Contains(line, dir+"/") || strings.Contains(line, "`"+dir+"`")
}
