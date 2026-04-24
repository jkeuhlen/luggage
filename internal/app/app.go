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
const binaryName = "luggage"

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
	case "install":
		return a.runInstall(args[1:])
	case "record":
		return a.runRecord(args[1:])
	case "time":
		return a.runTime(args[1:])
	case "wait":
		return a.runWait(args[1:])
	case "sessions":
		return a.runSessions(args[1:])
	case "context":
		return a.runContext(args[1:])
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
  luggage install [--bin-dir PATH] [--shell zsh] [--no-shell]
  luggage init zsh [--install]
  luggage completion zsh
  luggage version
  luggage record --started-at <epoch-seconds> --ended-at <epoch-seconds> --typed <cmd> --resolved <key> --exit-code <code> --cwd <path>
  luggage time <command> [--days N] [--granularity hourly|daily|weekly] [--view typed|resolved] [--window active|full] [--show-empty] [--pwd PATH|--cwd PATH] [--git-root PATH] [--here] [--this-repo] [--include-sessions] [--success-only]
  luggage wait [--days N] [--limit N] [--sub-limit N] [--view typed|resolved] [--compare] [--compare-days N] [--pwd PATH|--cwd PATH] [--git-root PATH] [--here] [--this-repo] [--include-sessions] [--success-only]
  luggage sessions [command] [--days N] [--view typed|resolved]
  luggage context [--report summary|timeline] [--days N] [--location repo|cwd] [--top N] [--granularity hourly|daily] [--idle-cutoff-min N] [--block-gap-min N] [--pwd PATH|--cwd PATH] [--git-root PATH] [--here] [--this-repo] [--include-sessions] [--success-only]
  luggage config get [key]
  luggage config set <key> <value>

Config keys:
  default_days, default_granularity, default_inclusion, default_view,
  session_cutoff_ms, anomaly_window, anomaly_sigma`)
}

func (a *App) printVersion() {
	fmt.Fprintln(a.Stdout, Version)
}

func (a *App) runInstall(args []string) int {
	defaultBinDir, err := defaultBinaryDir()
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}

	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	binDir := fs.String("bin-dir", defaultBinDir, "installation directory for luggage binary")
	shell := fs.String("shell", "zsh", "shell integration to install")
	noShell := fs.Bool("no-shell", false, "skip shell hook and completion setup")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(a.Stderr, "usage: luggage install [--bin-dir PATH] [--shell zsh] [--no-shell]")
		return 1
	}
	if !*noShell && *shell != "zsh" {
		fmt.Fprintf(a.Stderr, "unsupported shell: %s (only zsh is currently supported)\n", *shell)
		return 1
	}

	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	targetPath := filepath.Join(*binDir, binaryName)
	installed, err := installBinary(exePath, targetPath)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	if installed {
		fmt.Fprintf(a.Stdout, "installed binary to %s\n", targetPath)
	} else {
		fmt.Fprintf(a.Stdout, "binary already installed at %s\n", targetPath)
	}

	if *noShell {
		fmt.Fprintln(a.Stdout, "shell setup skipped (--no-shell)")
		return 0
	}

	snippetPath, err := shellutil.InstallZshSnippet()
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	compPath, err := shellutil.InstallZshCompletionFile()
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	fmt.Fprintf(a.Stdout, "installed zsh integration in %s\n", snippetPath)
	fmt.Fprintf(a.Stdout, "installed zsh completion in %s\n", compPath)
	fmt.Fprintln(a.Stdout, "restart your shell or run: exec zsh")
	return 0
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
	filters, err := resolveScopeFilters(*pwdFilter, *cwdFilter, *gitRootFilter, *here, *thisRepo)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
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
		CwdPrefix:       filters.Cwd,
		GitRoot:         filters.GitRoot,
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
		CwdFilter:       filters.Cwd,
		GitRootFilter:   filters.GitRoot,
	}, buckets)
	fmt.Fprint(a.Stdout, report)
	return 0
}

func (a *App) runWait(args []string) int {
	cfg, st, err := a.openStore()
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	defer st.Close()

	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	days := fs.Int("days", 30, "days to look back")
	limit := fs.Int("limit", 10, "max commands to display")
	subLimit := fs.Int("sub-limit", 3, "subcommands to show per top command (0 to disable)")
	view := fs.String("view", cfg.DefaultView, "typed|resolved")
	compare := fs.Bool("compare", false, "compare to previous period")
	compareDays := fs.Int("compare-days", 0, "length of comparison period in days (defaults to --days)")
	pwdFilter := fs.String("pwd", "", "cwd prefix filter")
	cwdFilter := fs.String("cwd", "", "cwd prefix filter (alias of --pwd)")
	gitRootFilter := fs.String("git-root", "", "git root filter")
	here := fs.Bool("here", false, "filter to current working directory")
	thisRepo := fs.Bool("this-repo", false, "filter to current git repository root")
	includeSessions := fs.Bool("include-sessions", false, "include long-lived sessions")
	successOnly := fs.Bool("success-only", false, "only include successful runs")

	flagArgs, positionals := splitFlagsAndPositionals(
		args,
		map[string]bool{
			"--compare":          true,
			"--here":             true,
			"--this-repo":        true,
			"--include-sessions": true,
			"--success-only":     true,
		},
		map[string]bool{
			"--days":         true,
			"--limit":        true,
			"--sub-limit":    true,
			"--view":         true,
			"--compare-days": true,
			"--pwd":          true,
			"--cwd":          true,
			"--git-root":     true,
		},
	)
	if err := fs.Parse(flagArgs); err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}
	if len(positionals) > 0 {
		fmt.Fprintln(a.Stderr, "usage: luggage wait [flags]")
		return 1
	}
	if *days <= 0 {
		fmt.Fprintln(a.Stderr, "--days must be > 0")
		return 1
	}
	if *limit <= 0 {
		fmt.Fprintln(a.Stderr, "--limit must be > 0")
		return 1
	}
	if *subLimit < 0 {
		fmt.Fprintln(a.Stderr, "--sub-limit must be >= 0")
		return 1
	}
	if *compareDays == 0 {
		*compareDays = *days
	}
	if *compareDays < 0 {
		fmt.Fprintln(a.Stderr, "--compare-days must be >= 0")
		return 1
	}
	switch *view {
	case "typed", "resolved":
	default:
		fmt.Fprintln(a.Stderr, "--view must be typed|resolved")
		return 1
	}

	filters, err := resolveScopeFilters(*pwdFilter, *cwdFilter, *gitRootFilter, *here, *thisRepo)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}

	now := time.Now()
	currentStart := now.AddDate(0, 0, -*days)
	queryStart := currentStart
	prevStart := time.Time{}
	prevEnd := time.Time{}
	if *compare {
		prevEnd = currentStart
		prevStart = prevEnd.AddDate(0, 0, -*compareDays)
		queryStart = prevStart
	}

	rows, err := st.QueryRuns(store.QueryOptions{
		Start:           queryStart,
		View:            *view,
		IncludeSessions: *includeSessions,
		SuccessOnly:     *successOnly,
		CwdPrefix:       filters.Cwd,
		GitRoot:         filters.GitRoot,
	})
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return 1
	}

	currentEntries, currentTotalMs, currentRuns := aggregateWaitEntries(rows, *view, currentStart, now)
	prevEntriesMap := map[string]waitEntry{}
	prevTotalMs := 0.0
	prevRuns := 0
	if *compare {
		prevEntries, totalMs, runs := aggregateWaitEntries(rows, *view, prevStart, prevEnd)
		prevTotalMs = totalMs
		prevRuns = runs
		for _, e := range prevEntries {
			prevEntriesMap[e.Command] = e
		}
	}

	limitN := *limit
	if len(currentEntries) < limitN {
		limitN = len(currentEntries)
	}
	reportEntries := make([]ui.WaitEntry, 0, limitN)
	topTokenSet := make(map[string]bool, limitN)
	for i := 0; i < limitN; i++ {
		cur := currentEntries[i]
		topTokenSet[cur.Command] = true
		prev := prevEntriesMap[cur.Command]
		reportEntries = append(reportEntries, ui.WaitEntry{
			Command:        cur.Command,
			CurrentTotalMs: cur.TotalMs,
			CurrentRuns:    cur.Runs,
			CurrentMeanMs:  cur.MeanMs,
			PrevTotalMs:    prev.TotalMs,
			PrevRuns:       prev.Runs,
			PrevMeanMs:     prev.MeanMs,
		})
	}
	subBreakdowns := aggregateWaitSubcommands(rows, *view, currentStart, now, topTokenSet, *subLimit)
	for i := range reportEntries {
		if subs := subBreakdowns[reportEntries[i].Command]; len(subs) > 0 {
			reportEntries[i].Subcommands = make([]ui.WaitSubEntry, 0, len(subs))
			for _, sub := range subs {
				reportEntries[i].Subcommands = append(reportEntries[i].Subcommands, ui.WaitSubEntry{
					Command: sub.Command,
					TotalMs: sub.TotalMs,
					Runs:    sub.Runs,
					MeanMs:  sub.MeanMs,
				})
			}
		}
	}

	report := ui.RenderWaitReport(ui.WaitReportParams{
		Days:            *days,
		Limit:           *limit,
		View:            *view,
		Compare:         *compare,
		CompareDays:     *compareDays,
		IncludeSessions: *includeSessions,
		SuccessOnly:     *successOnly,
		CwdFilter:       filters.Cwd,
		GitRootFilter:   filters.GitRoot,
		CurrentStart:    currentStart,
		CurrentEnd:      now,
		PrevStart:       prevStart,
		PrevEnd:         prevEnd,
	}, reportEntries, ui.WaitTotals{
		CurrentTotalMs: currentTotalMs,
		CurrentRuns:    currentRuns,
		PrevTotalMs:    prevTotalMs,
		PrevRuns:       prevRuns,
	})
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

func subcomponentLabel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(empty)"
	}
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return "(empty)"
	}
	root := parts[0]
	if len(parts) == 1 {
		return root + " (default)"
	}
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		if p != "" && !strings.HasPrefix(p, "-") {
			return root + " " + p
		}
	}
	return root + " (flags)"
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

type scopeFilters struct {
	Cwd     string
	GitRoot string
}

func resolveScopeFilters(pwdFilter, cwdFilter, gitRootFilter string, here, thisRepo bool) (scopeFilters, error) {
	if pwdFilter != "" && cwdFilter != "" && pwdFilter != cwdFilter {
		return scopeFilters{}, errors.New("--pwd and --cwd provided with different values")
	}
	if cwdFilter == "" {
		cwdFilter = pwdFilter
	}
	if here {
		wd, err := os.Getwd()
		if err != nil {
			return scopeFilters{}, err
		}
		cwdFilter = wd
	}
	if thisRepo {
		wd, err := os.Getwd()
		if err != nil {
			return scopeFilters{}, err
		}
		root := detectGitRoot(wd)
		if root == "" {
			return scopeFilters{}, errors.New("current working directory is not inside a git repository")
		}
		gitRootFilter = root
	}
	if cwdFilter != "" {
		cwdFilter = filepath.Clean(cwdFilter)
	}
	if gitRootFilter != "" {
		gitRootFilter = filepath.Clean(gitRootFilter)
	}
	return scopeFilters{Cwd: cwdFilter, GitRoot: gitRootFilter}, nil
}

type waitEntry struct {
	Command string
	TotalMs float64
	Runs    int
	MeanMs  float64
}

type waitSubEntry struct {
	Command string
	TotalMs float64
	Runs    int
	MeanMs  float64
}

func aggregateWaitEntries(rows []store.RunRow, view string, start, end time.Time) ([]waitEntry, float64, int) {
	type agg struct {
		total float64
		runs  int
	}
	groups := map[string]*agg{}
	totalMs := 0.0
	totalRuns := 0

	for _, row := range rows {
		if row.StartedAt.Before(start) || !row.StartedAt.Before(end) {
			continue
		}
		token := row.TypedCmd
		if view == "resolved" {
			token = row.Resolved
		}
		token = firstToken(token)
		if token == "" {
			token = "(empty)"
		}
		entry := groups[token]
		if entry == nil {
			entry = &agg{}
			groups[token] = entry
		}
		entry.total += row.Duration
		entry.runs++
		totalMs += row.Duration
		totalRuns++
	}

	out := make([]waitEntry, 0, len(groups))
	for cmd, ag := range groups {
		out = append(out, waitEntry{
			Command: cmd,
			TotalMs: ag.total,
			Runs:    ag.runs,
			MeanMs:  ag.total / float64(ag.runs),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalMs == out[j].TotalMs {
			if out[i].Runs == out[j].Runs {
				return out[i].Command < out[j].Command
			}
			return out[i].Runs > out[j].Runs
		}
		return out[i].TotalMs > out[j].TotalMs
	})
	return out, totalMs, totalRuns
}

func aggregateWaitSubcommands(rows []store.RunRow, view string, start, end time.Time, tokens map[string]bool, limit int) map[string][]waitSubEntry {
	type agg struct {
		total float64
		runs  int
	}
	perToken := map[string]map[string]*agg{}

	for _, row := range rows {
		if row.StartedAt.Before(start) || !row.StartedAt.Before(end) {
			continue
		}
		full := strings.TrimSpace(row.TypedCmd)
		if view == "resolved" {
			full = strings.TrimSpace(row.Resolved)
		}
		if full == "" {
			full = "(empty)"
		}
		token := firstToken(full)
		if token == "" {
			token = "(empty)"
		}
		if !tokens[token] {
			continue
		}
		tokenMap := perToken[token]
		if tokenMap == nil {
			tokenMap = map[string]*agg{}
			perToken[token] = tokenMap
		}
		subKey := subcomponentLabel(full)
		entry := tokenMap[subKey]
		if entry == nil {
			entry = &agg{}
			tokenMap[subKey] = entry
		}
		entry.total += row.Duration
		entry.runs++
	}

	out := make(map[string][]waitSubEntry, len(perToken))
	for token, subMap := range perToken {
		subs := make([]waitSubEntry, 0, len(subMap))
		for cmd, ag := range subMap {
			subs = append(subs, waitSubEntry{
				Command: cmd,
				TotalMs: ag.total,
				Runs:    ag.runs,
				MeanMs:  ag.total / float64(ag.runs),
			})
		}
		sort.Slice(subs, func(i, j int) bool {
			if subs[i].TotalMs == subs[j].TotalMs {
				if subs[i].Runs == subs[j].Runs {
					return subs[i].Command < subs[j].Command
				}
				return subs[i].Runs > subs[j].Runs
			}
			return subs[i].TotalMs > subs[j].TotalMs
		})
		if limit > 0 && len(subs) > limit {
			subs = subs[:limit]
		}
		out[token] = subs
	}
	return out
}

func defaultBinaryDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin"), nil
}

func installBinary(srcPath, dstPath string) (bool, error) {
	srcAbs, err := filepath.Abs(srcPath)
	if err != nil {
		return false, err
	}
	dstAbs, err := filepath.Abs(dstPath)
	if err != nil {
		return false, err
	}
	if sameFilePath(srcAbs, dstAbs) {
		return false, nil
	}

	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		return false, err
	}

	srcFile, err := os.Open(srcAbs)
	if err != nil {
		return false, err
	}
	defer srcFile.Close()

	tmpPath := dstAbs + ".tmp"
	dstFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = dstFile.Close()
		_ = os.Remove(tmpPath)
	}()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return false, err
	}
	if err := dstFile.Sync(); err != nil {
		return false, err
	}
	if err := dstFile.Close(); err != nil {
		return false, err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return false, err
	}
	if err := os.Rename(tmpPath, dstAbs); err != nil {
		return false, err
	}
	return true, nil
}

func sameFilePath(a, b string) bool {
	if a == b {
		return true
	}
	aEval, aErr := filepath.EvalSymlinks(a)
	bEval, bErr := filepath.EvalSymlinks(b)
	if aErr == nil && bErr == nil && aEval == bEval {
		return true
	}
	return false
}
