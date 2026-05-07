package config

import "testing"

func TestPatternConfigSetGet(t *testing.T) {
	cfg := Default()
	if err := SetValue(&cfg, "session_patterns", "make ghciwatch, * repl, , * ghci* "); err != nil {
		t.Fatalf("set session_patterns: %v", err)
	}
	if err := SetValue(&cfg, "long_task_patterns", "nix-collect-garbage*, nix build*"); err != nil {
		t.Fatalf("set long_task_patterns: %v", err)
	}

	got, err := GetValue(cfg, "session_patterns")
	if err != nil {
		t.Fatalf("get session_patterns: %v", err)
	}
	if got != "make ghciwatch,* repl,* ghci*" {
		t.Fatalf("unexpected session_patterns: %q", got)
	}

	got, err = GetValue(cfg, "long_task_patterns")
	if err != nil {
		t.Fatalf("get long_task_patterns: %v", err)
	}
	if got != "nix-collect-garbage*,nix build*" {
		t.Fatalf("unexpected long_task_patterns: %q", got)
	}
}

func TestDefaultPatternListsAreEmpty(t *testing.T) {
	cfg := Default()
	if cfg.SessionPatterns == nil {
		t.Fatalf("SessionPatterns should default to an empty slice")
	}
	if cfg.LongTaskPatterns == nil {
		t.Fatalf("LongTaskPatterns should default to an empty slice")
	}
	if len(cfg.SessionPatterns) != 0 || len(cfg.LongTaskPatterns) != 0 {
		t.Fatalf("expected empty pattern defaults, got %#v %#v", cfg.SessionPatterns, cfg.LongTaskPatterns)
	}
}
