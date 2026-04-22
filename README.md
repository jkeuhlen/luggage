# luggage

`luggage` tracks zsh command timings with low overhead and lets you inspect trends quickly.

## Build

```bash
go build -o luggage ./cmd/luggage
```

This repo pins `toolchain go1.24.12` for consistent local/CI results.

## Install zsh hooks + completion

Print snippet:

```bash
./luggage init zsh
```

One-step install (binary + zsh integration + completion):

```bash
./luggage install
```

Install binary only:

```bash
./luggage install --no-shell
```

Custom binary directory:

```bash
./luggage install --bin-dir ~/.local/bin
```

Install into `~/.zshrc` and `~/.zfunc/_luggage` (idempotent update):

```bash
./luggage init zsh --install
```

Print completion script only:

```bash
./luggage completion zsh
```

Then reload shell:

```bash
source ~/.zshrc
```

## Storage

- SQLite DB: `~/.luggage/luggage.db`
- Config: `~/.luggage/config.json`

## Usage

Time trends (default: last 90 days, daily):

```bash
luggage time gs
```

The `time` view renders a compact horizontal chart:
- active-window by default (empty leading/trailing buckets hidden)
- sparse series auto-hide chart and show table-only
- Unicode median sparkline (`▁▂▃▄▅▆▇█`) plus p95 gap markers (`·`, `▴`, `▲`, `◆`)

Useful flags:

```bash
luggage time gs --days 30 --granularity daily
luggage time gs --view resolved
luggage time gs --window full
luggage time gs --show-empty
luggage time gs --here
luggage time gs --this-repo
luggage time gs --pwd ~/workspace
luggage time gs --git-root ~/workspace/luggage
luggage time gs --include-sessions
luggage time gs --success-only
```

Session-focused metrics (long-lived commands like REPL/watch):

```bash
luggage sessions
luggage sessions ghciwatch
```

Waiting-time breakdown (where CLI wait time is going):

```bash
luggage wait --days 30 --limit 10
luggage wait --days 30 --limit 3 --compare
luggage wait --days 30 --compare --compare-days 30 --this-repo
luggage wait --days 30 --sub-limit 5
```

Config:

```bash
luggage config get
luggage config set default_days 120
luggage config set default_granularity weekly
luggage config set session_cutoff_ms 300000
```

Version:

```bash
luggage -v
luggage --version
luggage version
```

Sample chart outputs from tests:

```bash
go test ./internal/ui -run TestSampleCharts -v
```

## Quality Gates

Run the full local guard-rail suite:

```bash
make ci
```

Useful targets:

```bash
make fmt
make fmt-check
make lint
make test
make test-race
make vuln
make coverage-check
```

CI runs the same checks on every push/PR via `.github/workflows/ci.yml`.

## Notes

- Commands longer than 5 minutes are classified as session commands by default.
- `luggage time` excludes session commands unless `--include-sessions` is set.
- Both typed and resolved command identities are stored.
