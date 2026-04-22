package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestQueryRunsFilters(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "filters.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	now := time.Now().Add(-time.Hour).UnixMilli()
	runs := []Run{
		{
			StartedAtMs: now,
			EndedAtMs:   now + 20,
			DurationMs:  20,
			TypedCmd:    "gs",
			ResolvedCmd: "exec:/usr/bin/git",
			ExitCode:    0,
			Cwd:         "/repo-a",
			GitRoot:     "/repo-a",
		},
		{
			StartedAtMs: now + 1000,
			EndedAtMs:   now + 1030,
			DurationMs:  30,
			TypedCmd:    "gs",
			ResolvedCmd: "exec:/usr/bin/git",
			ExitCode:    0,
			Cwd:         "/repo-b/subdir",
			GitRoot:     "/repo-b",
		},
	}
	for _, r := range runs {
		if err := st.InsertRun(r); err != nil {
			t.Fatalf("insert run: %v", err)
		}
	}

	start := time.UnixMilli(now - 1000)
	byCwd, err := st.QueryRuns(QueryOptions{
		Start:           start,
		Query:           "gs",
		View:            "typed",
		IncludeSessions: true,
		CwdPrefix:       "/repo-b",
	})
	if err != nil {
		t.Fatalf("query by cwd: %v", err)
	}
	if len(byCwd) != 1 || byCwd[0].TypedCmd != "gs" {
		t.Fatalf("expected 1 gs row for cwd filter, got %d", len(byCwd))
	}
	if byCwd[0].Duration != 30 {
		t.Fatalf("expected duration 30 for cwd filter, got %v", byCwd[0].Duration)
	}

	byRepo, err := st.QueryRuns(QueryOptions{
		Start:           start,
		Query:           "gs",
		View:            "typed",
		IncludeSessions: true,
		GitRoot:         "/repo-a",
	})
	if err != nil {
		t.Fatalf("query by repo: %v", err)
	}
	if len(byRepo) != 1 {
		t.Fatalf("expected 1 gs row for repo filter, got %d", len(byRepo))
	}
	if byRepo[0].Duration != 20 {
		t.Fatalf("expected duration 20 for repo filter, got %v", byRepo[0].Duration)
	}
}

func BenchmarkInsertRun(b *testing.B) {
	dir := b.TempDir()
	dbPath := filepath.Join(dir, "bench.db")
	st, err := Open(dbPath)
	if err != nil {
		b.Fatalf("open store: %v", err)
	}
	defer st.Close()

	now := time.Now().UnixMilli()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := Run{
			StartedAtMs: now,
			EndedAtMs:   now + 123,
			DurationMs:  123,
			TypedCmd:    "gs",
			ResolvedCmd: "exec:/usr/bin/git",
			ExitCode:    0,
			Cwd:         "/tmp",
			GitRoot:     "",
			IsSession:   false,
		}
		if err := st.InsertRun(r); err != nil {
			b.Fatalf("insert run: %v", err)
		}
	}
}
