package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultIsValidAndRoundTrips(t *testing.T) {
	d := Default()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "c.json")
	if err := Save(p, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hash() != d.Hash() {
		t.Fatal("a saved and reloaded config must hash the same")
	}
	if got.Rules["tests.failing"].Why == "" {
		t.Fatal("the grounds of a weight must survive a round trip")
	}
}

func TestHashChangesWithAnyParameter(t *testing.T) {
	a, b := Default(), Default()
	b.Thresholds.Review++
	if a.Hash() == b.Hash() {
		t.Fatal("a threshold change must change the hash")
	}
}

func TestValidateRejectsMeaninglessNumbers(t *testing.T) {
	for name, mut := range map[string]func(*Config){
		"review above defer": func(c *Config) { c.Thresholds.Review, c.Thresholds.Defer = 70, 60 },
		"defer above 100":    func(c *Config) { c.Thresholds.Defer = 101 },
		"negative points":    func(c *Config) { c.Rules["x"] = RuleParams{Points: -1} },
		"unnamed group":      func(c *Config) { c.SensitivePaths = append(c.SensitivePaths, PathGroup{Points: 1}) },
		"unknown version":    func(c *Config) { c.Version = 2 },
		"zero window":        func(c *Config) { c.Evidence.HistoryWindowDays = 0 },
	} {
		c := Default()
		mut(c)
		if c.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
