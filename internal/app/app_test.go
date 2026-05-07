package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"luggage/internal/config"
	"luggage/internal/store"
)

func TestParseEpochSecondsToMS(t *testing.T) {
	ms, err := parseEpochSecondsToMS("1700000000.250")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ms != 1700000000250 {
		t.Fatalf("unexpected ms: %d", ms)
	}
}

func TestFirstToken(t *testing.T) {
	if got := firstToken("gs -sb"); got != "gs" {
		t.Fatalf("expected gs, got %q", got)
	}
}

func TestClassifyCommand(t *testing.T) {
	cfg := config.Default()
	cfg.SessionPatterns = []string{"make ghciwatch"}
	cfg.LongTaskPatterns = []string{"nix-collect-garbage*", "nix build*"}

	cases := []struct {
		name      string
		cmd       string
		duration  int64
		isSession bool
		reason    string
	}{
		{name: "pattern session", cmd: "make ghciwatch", duration: 10, isSession: true, reason: classificationPatternSession},
		{name: "pattern long task", cmd: "nix-collect-garbage -d", duration: 10, isSession: false, reason: classificationPatternLongTask},
		{name: "heuristic session", cmd: "cabal repl", duration: 10, isSession: true, reason: classificationHeuristicSession},
		{name: "heuristic long task", cmd: "stack build", duration: 10, isSession: false, reason: classificationHeuristicLongTask},
		{name: "duration long task", cmd: "custom maintenance", duration: cfg.SessionCutoffMs, isSession: false, reason: classificationDurationLongTask},
		{name: "normal duration", cmd: "gs -sb", duration: 20, isSession: false, reason: classificationNormalDuration},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyCommand(cfg, tc.cmd, tc.duration)
			if got.IsSession != tc.isSession || got.Reason != tc.reason {
				t.Fatalf("classifyCommand(%q) = %+v, want session=%v reason=%s", tc.cmd, got, tc.isSession, tc.reason)
			}
		})
	}
}

func TestClassifyCommandSessionPatternPrecedence(t *testing.T) {
	cfg := config.Default()
	cfg.SessionPatterns = []string{"make *"}
	cfg.LongTaskPatterns = []string{"make ghciwatch"}

	got := classifyCommand(cfg, "make ghciwatch", 10)
	if !got.IsSession || got.Reason != classificationPatternSession {
		t.Fatalf("expected session pattern precedence, got %+v", got)
	}
}

func TestRunRecordKeepsUnknownLongCommandInNormalReports(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	a := New(&stdout, &stderr)
	start := time.Now().Add(-time.Hour)
	code := a.Run([]string{
		"record",
		"--started-at", strconv.FormatInt(start.Unix(), 10),
		"--ended-at", strconv.FormatInt(start.Add(10*time.Minute).Unix(), 10),
		"--typed", "custom maintenance",
		"--resolved", "exec:/usr/local/bin/custom",
		"--exit-code", "0",
		"--cwd", t.TempDir(),
	})
	if code != 0 {
		t.Fatalf("record exited %d, stderr=%q", code, stderr.String())
	}

	dbPath, err := config.DBPath()
	if err != nil {
		t.Fatalf("db path: %v", err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	rows, err := st.QueryRuns(store.QueryOptions{
		Start:           start.Add(-time.Minute),
		Query:           "custom",
		View:            "typed",
		IncludeSessions: false,
	})
	if err != nil {
		t.Fatalf("query runs: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected long task to be visible in normal report queries, got %d rows", len(rows))
	}
	if rows[0].IsSession || rows[0].ClassificationReason != classificationDurationLongTask {
		t.Fatalf("unexpected row classification: %+v", rows[0])
	}
}

func TestDefaultBinaryDir(t *testing.T) {
	t.Setenv("HOME", "/tmp/luggage-home")
	got, err := defaultBinaryDir()
	if err != nil {
		t.Fatalf("defaultBinaryDir error: %v", err)
	}
	want := filepath.Join("/tmp/luggage-home", ".local", "bin")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestInstallBinaryCopies(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src-luggage")
	dst := filepath.Join(dir, "bin", "luggage")

	content := []byte("binary-content")
	if err := os.WriteFile(src, content, 0o755); err != nil {
		t.Fatalf("write src: %v", err)
	}

	installed, err := installBinary(src, dst)
	if err != nil {
		t.Fatalf("installBinary error: %v", err)
	}
	if !installed {
		t.Fatalf("expected installBinary to report installed=true")
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("expected %q, got %q", string(content), string(got))
	}
}

func TestInstallBinaryNoopWhenSamePath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "luggage")
	if err := os.WriteFile(src, []byte("same"), 0o755); err != nil {
		t.Fatalf("write src: %v", err)
	}
	installed, err := installBinary(src, src)
	if err != nil {
		t.Fatalf("installBinary error: %v", err)
	}
	if installed {
		t.Fatalf("expected installed=false when src and dst are same")
	}
}

func TestResolveScopeFilters(t *testing.T) {
	t.Setenv("PWD", "/tmp")
	got, err := resolveScopeFilters("/a", "", "", false, false)
	if err != nil {
		t.Fatalf("resolveScopeFilters err: %v", err)
	}
	if got.Cwd != "/a" {
		t.Fatalf("expected cwd /a, got %q", got.Cwd)
	}
}

func TestAggregateWaitEntries(t *testing.T) {
	now := time.Now()
	rows := []store.RunRow{
		{StartedAt: now.Add(-10 * time.Minute), Duration: 100, TypedCmd: "gs -sb", Resolved: "exec:/usr/bin/git"},
		{StartedAt: now.Add(-9 * time.Minute), Duration: 300, TypedCmd: "gs", Resolved: "exec:/usr/bin/git"},
		{StartedAt: now.Add(-8 * time.Minute), Duration: 200, TypedCmd: "npm test", Resolved: "exec:/usr/bin/npm"},
	}
	start := now.Add(-30 * time.Minute)
	end := now
	entries, totalMs, totalRuns := aggregateWaitEntries(rows, "typed", start, end)
	if totalRuns != 3 {
		t.Fatalf("expected 3 runs, got %d", totalRuns)
	}
	if totalMs != 600 {
		t.Fatalf("expected 600 total ms, got %v", totalMs)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 command entries, got %d", len(entries))
	}
	if entries[0].Command != "gs" || entries[0].TotalMs != 400 {
		t.Fatalf("unexpected top entry: %+v", entries[0])
	}
}

func TestSubcomponentLabel(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: "(empty)"},
		{in: "make", want: "make (default)"},
		{in: "make test", want: "make test"},
		{in: "make -j8 build", want: "make build"},
		{in: "git --no-pager status -sb", want: "git status"},
		{in: "curl -sS -L", want: "curl (flags)"},
	}
	for _, tc := range cases {
		if got := subcomponentLabel(tc.in); got != tc.want {
			t.Fatalf("subcomponentLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAggregateWaitSubcommands(t *testing.T) {
	now := time.Now()
	rows := []store.RunRow{
		{StartedAt: now.Add(-15 * time.Minute), Duration: 12000, TypedCmd: "make build", Resolved: "exec:/usr/bin/make"},
		{StartedAt: now.Add(-14 * time.Minute), Duration: 30000, TypedCmd: "make test", Resolved: "exec:/usr/bin/make"},
		{StartedAt: now.Add(-13 * time.Minute), Duration: 8000, TypedCmd: "make -j8 test", Resolved: "exec:/usr/bin/make"},
	}
	start := now.Add(-30 * time.Minute)
	end := now
	tokens := map[string]bool{"make": true}

	out := aggregateWaitSubcommands(rows, "typed", start, end, tokens, 3)
	subs := out["make"]
	if len(subs) != 2 {
		t.Fatalf("expected 2 subcommand groups, got %d", len(subs))
	}
	if subs[0].Command != "make test" || subs[0].Runs != 2 || subs[0].TotalMs != 38000 {
		t.Fatalf("unexpected first subcommand entry: %+v", subs[0])
	}
	if subs[1].Command != "make build" || subs[1].Runs != 1 || subs[1].TotalMs != 12000 {
		t.Fatalf("unexpected second subcommand entry: %+v", subs[1])
	}
}
