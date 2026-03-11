# devtool

devtool is a lightweight Go CLI that evaluates project health through Pragmatic Programmer principles.

## Overview

The goal is to keep feedback loops short and quality signals visible before merge or release.

devtool is designed for both:

- Local developer workflows (`go run .`, `make check`)
- CI workflows (`go run . --ci`, `make check-ci`)

Current checks (by principle):

- `ETC (Easy To Change)`
  - Change Safety Net: verifies `go test ./...` health
  - Formatting Hygiene: verifies `gofmt -l .` has no drift
- `Tracer Bullets`
  - Build Path: verifies `go build ./...` works as a one-command path check
- `DRY`
  - Large-Line Duplication: scans for repeated large text lines that may signal copy-paste drift
- `Broken Windows`
  - Backlog Markers: scans for `TODO` / `FIXME` / `HACK` / `XXX`
- `Orthogonality`
  - Package Boundaries: validates `go list ./...` and warns when boundaries are not explicit yet
- `Automation`
  - Environment: validates required env vars (currently `DATABASE_URL`)

## Requirements

- Go 1.25+
- GNU Make (optional, for shortcut commands)

## Quickstart

From the project root:

```bash
go run .
```

Or with shortcuts:

```bash
make check
```

Initialize a project config from the example:

```bash
cp devtool.example.yaml devtool.yaml
make check
```

## Common Commands

```bash
make help
make check
make check-json
make check-ci
make check-baseline
make list-checks
```

## CLI Options

- `--json`: output machine-readable JSON report
- `--ci`: use the `ci` threshold profile
- `--list-checks`: list available check IDs and exit
- `--env`: threshold profile name (default: `local`, or `ci` when `--ci` is set)
- `--write-baseline`: write baseline snapshot after checks complete
- `--config`: path to config file (default: `devtool.json`)
- `--enable`: comma-separated check IDs to run
- `--disable`: comma-separated check IDs to skip

Examples:

```bash
# Machine-readable output
go run . --json

# CI mode (fails build on warn/fail)
go run . --ci

# Select threshold profile explicitly
go run . --env ci

# Discover all check IDs
go run . --list-checks

# Discover check IDs in JSON
go run . --list-checks --json

# Run only two checks
go run . --enable etc-change-safety-net,automation-environment

# Skip noisy checks in a local loop
go run . --disable dry-large-line-duplication,broken-windows-backlog-markers

# Create or refresh trend baseline snapshot
go run . --write-baseline
```

## CI Usage

Minimal CI command:

```bash
go run . --ci --json
```

`--ci` enforces a non-zero exit code if any check returns `warn` or `fail`.

Threshold profiles are configurable, so teams can tune CI strictness without code changes.

Threshold rules:

- `max_warn`: fail when warning count is greater than this value
- `max_fail`: fail when failure count is greater than this value
- `-1` disables the limit

### GitHub Actions Example

```yaml
name: quality-signals

on:
  pull_request:
  push:
    branches: [main]

jobs:
  devtool:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.25.x"
      - name: Run pragmatic checks
        run: go run . --ci --json > devtool-report.json
      - name: Upload report
        uses: actions/upload-artifact@v4
        with:
          name: devtool-report
          path: devtool-report.json
```

Config file (`devtool.json`) example:

```json
{
  "required_env": ["DATABASE_URL", "REDIS_URL", "SENTRY_DSN"],
  "baseline_path": ".devtool-baseline.json",
  "thresholds": {
    "local": { "max_warn": -1, "max_fail": -1 },
    "ci": { "max_warn": 0, "max_fail": 0 }
  }
}
```

YAML is also supported via `devtool.yaml` or `devtool.yml`:

```yaml
required_env:
  - DATABASE_URL
  - REDIS_URL
baseline_path: .devtool-baseline.json
thresholds:
  local:
    max_warn: -1
    max_fail: -1
  ci:
    max_warn: 0
    max_fail: 0
```

If `devtool.json` is missing, devtool will also auto-detect `devtool.yaml` and `devtool.yml`.

You can also start directly from the in-repo example file: `devtool.example.yaml`.

Example output:

```text
🔍 Project Status

⚠️ [ETC] Change Safety Net: No test files found; change confidence is low
✅ [ETC] Formatting Hygiene: Formatting is consistent
✅ [Tracer Bullets] Build Path: One-command build path is healthy
✅ [DRY] Large-Line Duplication: No obvious duplicated large lines detected
✅ [Broken Windows] Backlog Markers: No TODO/FIXME-style markers found
⚠️ [Orthogonality] Package Boundaries: Only one package detected; boundaries are not explicit yet
⚠️ [Automation] Environment: Missing environment variables: DATABASE_URL

⏱️ Completed in 0.07s
```

## Status meanings

- `✅ ok`: check passed
- `⚠️ warn`: non-blocking issue that should be reviewed
- `❌ fail`: blocking issue that should be fixed

## Notes

- Test failures include command output to help you diagnose quickly.
- If no test files exist yet, test status returns a warning instead of failure.
- Checks are registered in one list in [main.go](main.go), so adding principles/checks stays low-friction.
- JSON output includes per-check IDs, summary counts, and duration for CI tooling.
- Use `--list-checks` to discover valid IDs for `--enable` and `--disable`.
- Baseline snapshots currently track marker trends for the Broken Windows check.

## Make Targets

- `make fmt`: format source
- `make test`: run tests
- `make build`: compile all packages
- `make check`: human-readable checks
- `make check-json`: machine-readable checks
- `make check-ci`: CI-mode checks
- `make check-baseline`: refresh trend baseline snapshot
- `make list-checks`: list check IDs
- `make list-checks-json`: list check IDs in JSON

## Architecture

devtool uses a simple check registry model:

1. `main` builds a list of checks (`CheckDefinition`) with stable check IDs.
2. `runChecks` executes selected checks and attaches principle metadata.
3. Output rendering is split between human-readable and JSON modes.
4. Exit behavior is determined by threshold profiles (`local`, `ci`, or custom).

To add a new check:

1. Implement a check function that returns `CheckResult`.
2. Register it in the check list with a unique ID and principle.
3. Add documentation and optional examples in this README.

## Release Checklist

- Run `make fmt`
- Run `make test`
- Run `make check`
- Run `make check-ci`
- Validate `make check-json` output shape
- Validate `make list-checks` IDs are documented
- Update README examples if output text changed

## Check IDs

- `etc-change-safety-net`
- `etc-formatting-hygiene`
- `tracer-bullets-build-path`
- `dry-large-line-duplication`
- `broken-windows-backlog-markers`
- `orthogonality-package-boundaries`
- `automation-environment`

## Interpreting New Checks

- `Tracer Bullets` favors a thin, always-runnable path over comprehensive coverage.
- `DRY` uses a heuristic for repeated large lines; it flags possible duplication, not guaranteed bugs.
- `Broken Windows` highlights unresolved markers before they become long-lived debt.
- `Orthogonality` gives a structural signal based on package graph health and boundary granularity.

## Roadmap

Potential next checks:

- Differential duplication tracking against baseline history
- SARIF export mode for static-analysis ingestion
- Plugin interface for external custom checks
