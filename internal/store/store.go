package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

type Run struct {
	StartedAtMs int64
	EndedAtMs   int64
	DurationMs  float64
	TypedCmd    string
	ResolvedCmd string
	ExitCode    int
	Cwd         string
	GitRoot     string
	IsSession   bool
}

type RunRow struct {
	StartedAt time.Time
	Duration  float64
	ExitCode  int
	IsSession bool
	TypedCmd  string
	Resolved  string
}

func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_busy_timeout=1000&_journal_mode=WAL&_synchronous=NORMAL", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) init() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS runs (
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
CREATE INDEX IF NOT EXISTS idx_runs_typed_time ON runs(typed_cmd, started_at_ms);
CREATE INDEX IF NOT EXISTS idx_runs_resolved_time ON runs(resolved_cmd_key, started_at_ms);
CREATE INDEX IF NOT EXISTS idx_runs_time ON runs(started_at_ms);
CREATE INDEX IF NOT EXISTS idx_runs_git_time ON runs(git_root, started_at_ms);
CREATE INDEX IF NOT EXISTS idx_runs_session_time ON runs(is_session, started_at_ms);
`)
	return err
}

func (s *Store) InsertRun(run Run) error {
	isSession := 0
	if run.IsSession {
		isSession = 1
	}
	_, err := s.db.Exec(`
INSERT INTO runs (
	started_at_ms, ended_at_ms, duration_ms,
	typed_cmd, resolved_cmd_key, exit_code,
	cwd, git_root, is_session, created_at_ms
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		run.StartedAtMs,
		run.EndedAtMs,
		run.DurationMs,
		run.TypedCmd,
		run.ResolvedCmd,
		run.ExitCode,
		run.Cwd,
		run.GitRoot,
		isSession,
		time.Now().UnixMilli(),
	)
	return err
}

func (s *Store) QueryRuns(opts QueryOptions) ([]RunRow, error) {
	col := "typed_cmd"
	if opts.View == "resolved" {
		col = "resolved_cmd_key"
	}

	where := "started_at_ms >= ?"
	args := []any{opts.Start.UnixMilli()}

	if opts.Query != "" {
		if opts.ExactMatch {
			where += fmt.Sprintf(" AND %s = ?", col)
			args = append(args, opts.Query)
		} else {
			where += fmt.Sprintf(" AND (CASE WHEN instr(%s,' ') = 0 THEN %s ELSE substr(%s,1,instr(%s,' ')-1) END) = ?", col, col, col, col)
			args = append(args, opts.Query)
		}
	}
	if !opts.IncludeSessions {
		where += " AND is_session = 0"
	}
	if opts.SuccessOnly {
		where += " AND exit_code = 0"
	}
	if opts.CwdPrefix != "" {
		where += " AND cwd LIKE ?"
		args = append(args, opts.CwdPrefix+"%")
	}
	if opts.GitRoot != "" {
		where += " AND git_root = ?"
		args = append(args, opts.GitRoot)
	}

	query := fmt.Sprintf(`
SELECT started_at_ms, duration_ms, exit_code, is_session, typed_cmd, resolved_cmd_key
FROM runs
WHERE %s
ORDER BY started_at_ms ASC
`, where)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RunRow
	for rows.Next() {
		var startedMs int64
		var duration float64
		var exitCode int
		var isSession int
		var typed string
		var resolved string
		if err := rows.Scan(&startedMs, &duration, &exitCode, &isSession, &typed, &resolved); err != nil {
			return nil, err
		}
		out = append(out, RunRow{
			StartedAt: time.UnixMilli(startedMs),
			Duration:  duration,
			ExitCode:  exitCode,
			IsSession: isSession == 1,
			TypedCmd:  typed,
			Resolved:  resolved,
		})
	}
	return out, rows.Err()
}

type SessionSummaryRow struct {
	CommandToken string
	Runs         int
	TotalMs      float64
	MedianMs     float64
	P95Ms        float64
}

type QueryOptions struct {
	Start           time.Time
	Query           string
	ExactMatch      bool
	View            string
	IncludeSessions bool
	SuccessOnly     bool
	CwdPrefix       string
	GitRoot         string
}

func (s *Store) RecentCommandTokens(limit int, prefix string) ([]string, error) {
	if limit <= 0 {
		limit = 50
	}
	scanLimit := limit * 20
	if scanLimit < 200 {
		scanLimit = 200
	}
	rows, err := s.db.Query(`
SELECT typed_cmd
FROM runs
ORDER BY started_at_ms DESC
LIMIT ?
`, scanLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prefix = strings.TrimSpace(prefix)
	seen := map[string]bool{}
	out := make([]string, 0, limit)

	for rows.Next() {
		if len(out) >= limit {
			break
		}
		var typed string
		if err := rows.Scan(&typed); err != nil {
			return nil, err
		}
		token := firstToken(typed)
		if token == "" || seen[token] {
			continue
		}
		if prefix != "" && !strings.HasPrefix(token, prefix) {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out, rows.Err()
}

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
