package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"luggage/internal/config"
	"luggage/internal/metrics"
	"luggage/internal/shellutil"
	"luggage/internal/store"
	"luggage/internal/ui"
)

const maxCommandLength = 4096

var Version = "0.0.1"

type App struct {
	Stdout io.Writer
	Stderr io.Writer
}

func New(stdout, stderr io.Writer) *App {
	return &App{Stdout: stdout, Stderr: stderr}
}

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.printUsage()
		return 1
	}

	switch args[0] {
	case "help", "-h", "--help":
		a.printUsage()
		return 0
	case "version", "-v", "--version":
		a.printVersion()
		return 0
	case "init":
		return a.runInit(args[1:])
	case "record":
		return a.runRecord(args[1:])
	case "time":
		return a.runTime(args[1:])
	case "sessions":
		return a.runSessions(args[1:])
	case "config":
		return a.runConfig(args[1:])
	case "completion":
		return a.runCompletion(args[1:])
	case "__complete":
		return a.runInternalComplete(args[1:])
	default:
		fmt.Fprintf(a.Stderr, "unknown command: %s\n\n", args[0])
		a.printUsage()
		return 1
	}
}

func (a *App) printUsage() {
	fmt.Fprintln(a.Stdout, `luggage - command timing trends for zsh

Usage:
  luggage init zsh [--install]
  luggage completion zsh
  luggage version
  luggage record --started-at <epoch-seconds> --ended-at <epoch-seconds> --typed <cmd> --resolved <key> --exit-code <code> --cwd <path>
  luggage time <command> [--days N] [--granularity hourly|daily|weekly] [--view typed|resolved] [--window active|full] [--show-empty] [--pwd PATH|--cwd PATH] [--git-root PATH] [--here] [--this-repo] [--include-sessions] [--success-only]
  luggage sessions [command] [--days N] [--view typed|resolved]
  luggage config get [key]
  luggage config set <key> <value>

Config keys:
  default_days, default_granularity, default_inclusion, default_view,
  session_cutoff_ms, anomaly_window, anomaly_sigma`)
}

func (a *App) printVersion() {
	fmt.Fprintln(a.Stdout, Version)
}

func (a *App) runInit(args []string) int {
	if len(args) == 0 || args[0] != "zsh" {
		fmt.Fprintln(a.Stderr, "usage: luggage init zsh [--install]")
		return 1
	}
	fs := flag.NewFlagSet("init zsh", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	install := fs.Bool("install", false, "install snippet into ~/.zshrc")
	if err := fs.Parse(args[1:]); err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	if *install {
		path, err := shellutil.InstallZshSnippet()
		if err != nil {
			fmt.Fprintln(a.Stderr, err)
			return 1
		}
		compPath, err := shellutil.InstallZshCompletionFile()
		if err != nil {
			fmt.Fprintln(a.Stderr, err)
			return 1
		}
		fmt.Fprintf(a.Stdout, "installed luggage zsh snippet in %s\n", path)
		fmt.Fprintf(a.Stdout, "installed luggage completion in %s\n", compPath)
		return 0
	}
	fmt.Fprint(a.Stdout, shellutil.ZshSnippet())
	return 0
}

func (a *App) runCompletion(args []string) int {
	if len(args) != 1 || args[0] != "zsh" {
		fmt.Fprintln(a.Stderr, "usage: luggage completion zsh")
		return 1
	}
	fmt.Fprint(a.Stdout, shellutil.ZshCompletion())
	return 0
}

func (a *App) runInternalComplete(args []string) int {
	if len(args) == 0 {
		return 1
	}
	switch args[0] {
	case "time":
		return a.runInternalCompleteTime(args[1:])
	default:
		return 1
	}
}

func (a *App) runInternalCompleteTime(args []string) int {
	prefix := ""
	if len(args) > 0 {
		prefix = args[0]
	}
	_, st, err := a.openStore()
	if err != nil {
		return 1
	}
	defer st.Close()

	tokens, err := st.RecentCommandTokens(50, prefix)
	if err != nil {
		return 1
	}
	for _, tok := range tokens {
		fmt.Fprintln(a.Stdout, tok)
	}
	return 0
}

func (a *App) runRecord(args []string) int {
	cfg, st, err := a.openStore()
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	defer st.Close()

	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	startedAt := fs.String("started-at", "", "epoch seconds")
	endedAt := fs.String("ended-at", "", "epoch seconds")
	typed := fs.String("typed", "", "typed command")
	resolved := fs.String("resolved", "", "resolved command key")
	exitCode := fs.Int("exit-code", 0, "exit code")
	cwd := fs.String("cwd", "", "working directory")

	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *typed == "" || *startedAt == "" || *endedAt == "" {
		return 1
	}

	startedMs, err := parseEpochSecondsToMS(*startedAt)
	if err != nil {
		return 1
	}
	endedMs, err := parseEpochSecondsToMS(*endedAt)
	if err != nil {
		return 1
	}
	if endedMs < startedMs {
		endedMs = startedMs
	}
	durationMs := float64(endedMs - startedMs)

	typedCmd := clamp(*typed, maxCommandLength)
	resolvedKey := strings.TrimSpace(*resolved)
	if resolvedKey == "" {
		resolvedKey = "unknown:" + firstToken(typedCmd)
	}
	resolvedKey = clamp(resolvedKey, maxCommandLength)

	cwdPath := strings.TrimSpace(*cwd)
	if cwdPath == "" {
		if wd, err := os.Getwd(); err == nil {
			cwdPath = wd
		}
	}
	gitRoot := detectGitRoot(cwdPath)
	isSession := int64(durationMs) >= cfg.SessionCutoffMs

	run := store.Run{
		StartedAtMs: startedMs,
		EndedAtMs:   endedMs,
		DurationMs:  durationMs,
		TypedCmd:    typedCmd,
		ResolvedCmd: resolvedKey,
		ExitCode:    *exitCode,
		Cwd:         clamp(cwdPath, maxCommandLength),
		GitRoot:     clamp(gitRoot, maxCommandLength),
		IsSession:   isSession,
	}
	if err := st.InsertRun(run); err != nil {
		return 1
	}
	return 0
}

func (a *App) runTime(args []string) int {
	cfg, st, err := a.openStore()
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	defer st.Close()

	fs := flag.NewFlagSet("time", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	days := fs.Int("days", cfg.DefaultDays, "days to look back")
	granularity := fs.String("granularity", cfg.DefaultGranularity, "hourly|daily|weekly")
	view := fs.String("view", cfg.DefaultView, "typed|resolved")
	window := fs.String("window", "active", "active|full")
	showEmpty := fs.Bool("show-empty", false, "include empty buckets in chart window (same as --window full)")
	pwdFilter := fs.String("pwd", "", "cwd prefix filter")
	cwdFilter := fs.String("cwd", "", "cwd prefix filter (alias of --pwd)")
	gitRootFilter := fs.String("git-root", "", "git root filter")
	here := fs.Bool("here", false, "filter to current working directory")
	thisRepo := fs.Bool("this-repo", false, "filter to current git repository root")
	includeSessions := fs.Bool("include-sessions", false, "include long-lived sessions")
	successOnlyDefault := cfg.DefaultInclusion == "success_only"
	successOnly := fs.Bool("success-only", successOnlyDefault, "only include successful runs")
	flagArgs, queryArgs := splitFlagsAndPositionals(
		args,
		map[string]bool{
			"--here":             true,
			"--this-repo":        true,
			"--show-empty":       true,
			"--include-sessions": true,
			"--success-only":     true,
		},
		map[string]bool{
			"--days":        true,
			"--granularity": true,
			"--view":        true,
			"--window":      true,
			"--pwd":         true,
			"--cwd":         true,
			"--git-root":    true,
		},
	)
	if err := fs.Parse(flagArgs); err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	if *days <= 0 {
		fmt.Fprintln(a.Stderr, "--days must be > 0")
		return 1
	}
	if *showEmpty {
		*window = "full"
	}
	switch *window {
	case "active", "full":
	default:
		fmt.Fprintln(a.Stderr, "--window must be active|full")
		return 1
	}
	if len(queryArgs) == 0 {
		fmt.Fprintln(a.Stderr, "usage: luggage time <command> [flags]")
		return 1
	}
	if *pwdFilter != "" && *cwdFilter != "" && *pwdFilter != *cwdFilter {
		fmt.Fprintln(a.Stderr, "--pwd and --cwd provided with different values")
		return 1
	}
	if *cwdFilter == "" {
		*cwdFilter = *pwdFilter
	}
	if *here {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(a.Stderr, err)
			return 1
		}
		*cwdFilter = wd
	}
	if *thisRepo {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(a.Stderr, err)
			return 1
		}
		root := detectGitRoot(wd)
		if root == "" {
			fmt.Fprintln(a.Stderr, "current working directory is not inside a git repository")
			return 1
		}
		*gitRootFilter = root
	}
	if *cwdFilter != "" {
		*cwdFilter = filepath.Clean(*cwdFilter)
	}
	if *gitRootFilter != "" {
		*gitRootFilter = filepath.Clean(*gitRootFilter)
	}
	query := strings.Join(queryArgs, " ")
	exactMatch := strings.Contains(query, " ")

	now := time.Now()
	start := now.AddDate(0, 0, -*days)
	rows, err := st.QueryRuns(store.QueryOptions{
		Start:           start,
		Query:           query,
		ExactMatch:      exactMatch,
		View:            *view,
		IncludeSessions: *includeSessions,
		SuccessOnly:     *successOnly,
		CwdPrefix:       *cwdFilter,
		GitRoot:         *gitRootFilter,
	})
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	g := metrics.NormalizeGranularity(*granularity)
	buckets := metrics.BuildBuckets(rows, start, now, g, now.Location())
	metrics.DetectAnomalies(buckets, cfg.AnomalyWindow, cfg.AnomalySigma)

	report := ui.RenderTimeReport(ui.TimeReportParams{
		Command:         query,
		View:            *view,
		Granularity:     g,
		Days:            *days,
		Window:          *window,
		IncludeSessions: *includeSessions,
		SuccessOnly:     *successOnly,
		CwdFilter:       *cwdFilter,
		GitRootFilter:   *gitRootFilter,
	}, buckets)
	fmt.Fprint(a.Stdout, report)
	return 0
}

func (a *App) runSessions(args []string) int {
	cfg, st, err := a.openStore()
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	defer st.Close()

	fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	days := fs.Int("days", cfg.DefaultDays, "days to look back")
	view := fs.String("view", cfg.DefaultView, "typed|resolved")
	flagArgs, queryArgs := splitFlagsAndPositionals(
		args,
		map[string]bool{},
		map[string]bool{
			"--days": true,
			"--view": true,
		},
	)
	if err := fs.Parse(flagArgs); err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	if *days <= 0 {
		fmt.Fprintln(a.Stderr, "--days must be > 0")
		return 1
	}
	query := ""
	exactMatch := false
	if len(queryArgs) > 0 {
		query = strings.Join(queryArgs, " ")
		exactMatch = strings.Contains(query, " ")
	}

	now := time.Now()
	start := now.AddDate(0, 0, -*days)
	rows, err := st.QueryRuns(store.QueryOptions{
		Start:           start,
		Query:           query,
		ExactMatch:      exactMatch,
		View:            *view,
		IncludeSessions: true,
		SuccessOnly:     false,
	})
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}

	sessionRows := make([]store.RunRow, 0, len(rows))
	for _, row := range rows {
		if row.IsSession {
			sessionRows = append(sessionRows, row)
		}
	}
	durations := make([]float64, 0, len(sessionRows))
	for _, row := range sessionRows {
		durations = append(durations, row.Duration)
	}

	summary := ui.SessionSummary{Days: *days, Runs: len(sessionRows)}
	if len(durations) > 0 {
		summary.TotalMs = sum(durations)
		summary.MedianMs = pct(durations, 50)
		summary.P95Ms = pct(durations, 95)
	}

	type agg struct {
		runs      int
		total     float64
		durations []float64
	}
	groups := map[string]*agg{}
	for _, row := range sessionRows {
		tok := firstToken(row.TypedCmd)
		if tok == "" {
			tok = "(empty)"
		}
		entry := groups[tok]
		if entry == nil {
			entry = &agg{}
			groups[tok] = entry
		}
		entry.runs++
		entry.total += row.Duration
		entry.durations = append(entry.durations, row.Duration)
	}

	entries := make([]ui.SessionEntry, 0, len(groups))
	for cmd, ag := range groups {
		entries = append(entries, ui.SessionEntry{
			Command: cmd,
			Runs:    ag.runs,
			TotalMs: ag.total,
			MeanMs:  ag.total / float64(ag.runs),
			P95Ms:   pct(ag.durations, 95),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].TotalMs > entries[j].TotalMs
	})
	if len(entries) > 10 {
		entries = entries[:10]
	}
	summary.TopEntries = entries
	fmt.Fprint(a.Stdout, ui.RenderSessionSummary(summary))
	return 0
}

func (a *App) runConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(a.Stderr, "usage: luggage config get [key] | luggage config set <key> <value>")
		return 1
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}

	switch args[0] {
	case "get":
		if len(args) == 1 {
			out, _ := json.MarshalIndent(cfg, "", "  ")
			fmt.Fprintln(a.Stdout, string(out))
			return 0
		}
		value, err := config.GetValue(cfg, args[1])
		if err != nil {
			fmt.Fprintln(a.Stderr, err)
			return 1
		}
		fmt.Fprintln(a.Stdout, value)
		return 0
	case "set":
		if len(args) != 3 {
			fmt.Fprintln(a.Stderr, "usage: luggage config set <key> <value>")
			return 1
		}
		if err := config.SetValue(&cfg, args[1], args[2]); err != nil {
			fmt.Fprintln(a.Stderr, err)
			return 1
		}
		if err := config.Save(cfg); err != nil {
			fmt.Fprintln(a.Stderr, err)
			return 1
		}
		fmt.Fprintf(a.Stdout, "set %s=%s\n", args[1], args[2])
		return 0
	default:
		fmt.Fprintln(a.Stderr, "usage: luggage config get [key] | luggage config set <key> <value>")
		return 1
	}
}

func (a *App) openStore() (config.Config, *store.Store, error) {
	if _, err := config.EnsureDataDir(); err != nil {
		return config.Config{}, nil, err
	}
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, nil, err
	}
	dbPath, err := config.DBPath()
	if err != nil {
		return config.Config{}, nil, err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return config.Config{}, nil, err
	}
	return cfg, st, nil
}

func parseEpochSecondsToMS(v string) (int64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, errors.New("empty epoch value")
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errors.New("invalid epoch value")
	}
	return int64(math.Round(f * 1000)), nil
}

func detectGitRoot(cwd string) string {
	if cwd == "" {
		return ""
	}
	cur := cwd
	for {
		if cur == "." {
			break
		}
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return ""
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

func clamp(in string, n int) string {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func sum(in []float64) float64 {
	t := 0.0
	for _, v := range in {
		t += v
	}
	return t
}

func pct(in []float64, p int) float64 {
	if len(in) == 0 {
		return 0
	}
	clone := append([]float64(nil), in...)
	sort.Float64s(clone)
	if p <= 0 {
		return clone[0]
	}
	if p >= 100 {
		return clone[len(clone)-1]
	}
	rank := (float64(p) / 100.0) * float64(len(clone)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return clone[lo]
	}
	w := rank - float64(lo)
	return clone[lo]*(1-w) + clone[hi]*w
}

func splitFlagsAndPositionals(args []string, boolFlags, valueFlags map[string]bool) (flagArgs, positionals []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if boolFlags[arg] {
			flagArgs = append(flagArgs, arg)
			continue
		}
		if valueFlags[arg] {
			flagArgs = append(flagArgs, arg)
			if i+1 < len(args) {
				i++
				flagArgs = append(flagArgs, args[i])
			}
			continue
		}
		if strings.HasPrefix(arg, "--") {
			name := arg
			if idx := strings.IndexByte(arg, '='); idx > 0 {
				name = arg[:idx]
			}
			if boolFlags[name] || valueFlags[name] {
				flagArgs = append(flagArgs, arg)
				continue
			}
		}
		positionals = append(positionals, arg)
	}
	return flagArgs, positionals
}
