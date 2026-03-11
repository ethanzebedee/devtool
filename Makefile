APP := devtool

.PHONY: help fmt test build check check-json check-ci check-baseline list-checks list-checks-json

help:
	@echo "Available targets:"
	@echo "  make fmt               - Format Go source"
	@echo "  make test              - Run go test ./..."
	@echo "  make build             - Build project"
	@echo "  make check             - Run human-readable checks"
	@echo "  make check-json        - Run checks with JSON output"
	@echo "  make check-ci          - Run checks in CI mode (non-zero on warn/fail)"
	@echo "  make check-baseline    - Run checks and update baseline snapshot"
	@echo "  make list-checks       - List available check IDs"
	@echo "  make list-checks-json  - List available check IDs as JSON"

fmt:
	gofmt -w main.go

test:
	go test ./...

build:
	go build ./...

check:
	go run .

check-json:
	go run . --json

check-ci:
	go run . --ci

check-baseline:
	go run . --write-baseline

list-checks:
	go run . --list-checks

list-checks-json:
	go run . --list-checks --json
