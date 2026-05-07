package store

import (
	"database/sql"
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

func TestQueryRunsReturnsClassificationReason(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "classification.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	now := time.Now().Add(-time.Minute).UnixMilli()
	if err := st.InsertRun(Run{
		StartedAtMs:          now,
		EndedAtMs:            now + 20,
		DurationMs:           20,
		TypedCmd:             "gs",
		ResolvedCmd:          "exec:/usr/bin/git",
		ExitCode:             0,
		Cwd:                  "/repo",
		GitRoot:              "/repo",
		ClassificationReason: "normal_duration",
	}); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	rows, err := st.QueryRuns(QueryOptions{Start: time.UnixMilli(now - 1000), Query: "gs", View: "typed"})
	if err != nil {
		t.Fatalf("query runs: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].ClassificationReason != "normal_duration" {
		t.Fatalf("expected classification reason, got %q", rows[0].ClassificationReason)
	}
}

func TestOpenMigratesClassificationReasonColumn(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	now := time.Now().Add(-time.Minute).UnixMilli()
	_, err = db.Exec(`
CREATE TABLE runs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	started_at_ms INTEGER NOT NULL,
	ended_at_ms INTEGER NOT NULL,
	duration_ms REAL NOT NULL,
	typed_cmd TEXT NOT NULL,
	resolved_cmd_key TEXT NOT NULL,
	exit_code INTEGER NOT NULL,
	cwd TEXT NOT NULL,
	git_root TEXT NOT NULL,
	is_session INTEGER NOT NULL,
	created_at_ms INTEGER NOT NULL
);
INSERT INTO runs (
	started_at_ms, ended_at_ms, duration_ms,
	typed_cmd, resolved_cmd_key, exit_code,
	cwd, git_root, is_session, created_at_ms
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
`, now, now+20, 20, "ghci", "exec:/usr/bin/ghci", 130, "/repo", "/repo", 1, now+30)
	if err != nil {
		_ = db.Close()
		t.Fatalf("create legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer st.Close()

	rows, err := st.QueryRuns(QueryOptions{Start: time.UnixMilli(now - 1000), Query: "ghci", View: "typed", IncludeSessions: true})
	if err != nil {
		t.Fatalf("query migrated rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 migrated row, got %d", len(rows))
	}
	if !rows[0].IsSession || rows[0].ClassificationReason != "legacy" {
		t.Fatalf("unexpected migrated row: %+v", rows[0])
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
