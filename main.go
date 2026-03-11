package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type CheckResult struct {
	Principle string
	Name      string
	ID        string
	Status    string // "ok", "warn", "fail"
	Detail    string
}

type CheckDefinition struct {
	Principle string
	Name      string
	ID        string
	Run       func() CheckResult
}

type AppConfig struct {
	RequiredEnv  []string                   `json:"required_env" yaml:"required_env"`
	BaselinePath string                     `json:"baseline_path" yaml:"baseline_path"`
	Thresholds   map[string]ThresholdConfig `json:"thresholds" yaml:"thresholds"`
}

type ThresholdConfig struct {
	MaxWarn int `json:"max_warn" yaml:"max_warn"`
	MaxFail int `json:"max_fail" yaml:"max_fail"`
}

type BaselineData struct {
	BrokenWindowsMarkers int    `json:"broken_windows_markers"`
	UpdatedAt            string `json:"updated_at"`
}

type RuntimeState struct {
	BrokenWindowsMarkers int
}

type Summary struct {
	Total int `json:"total"`
	OK    int `json:"ok"`
	Warn  int `json:"warn"`
	Fail  int `json:"fail"`
}

type JSONReport struct {
	StartedAt  string        `json:"started_at"`
	DurationMs int64         `json:"duration_ms"`
	Summary    Summary       `json:"summary"`
	Results    []CheckResult `json:"results"`
}

type CheckInfo struct {
	ID        string `json:"id"`
	Principle string `json:"principle"`
	Name      string `json:"name"`
}

func main() {
	var jsonOutput bool
	var ciMode bool
	var listChecks bool
	var envName string
	var writeBaseline bool
	var configPath string
	var enableChecks string
	var disableChecks string

	flag.BoolVar(&jsonOutput, "json", false, "Output results as JSON")
	flag.BoolVar(&ciMode, "ci", false, "Exit non-zero when any check is warn or fail")
	flag.BoolVar(&listChecks, "list-checks", false, "List all available check IDs and exit")
	flag.StringVar(&envName, "env", "", "Threshold environment profile (for example: local, ci)")
	flag.BoolVar(&writeBaseline, "write-baseline", false, "Write/update baseline snapshot after running checks")
	flag.StringVar(&configPath, "config", "devtool.json", "Path to config file")
	flag.StringVar(&enableChecks, "enable", "", "Comma-separated check IDs to run")
	flag.StringVar(&disableChecks, "disable", "", "Comma-separated check IDs to skip")
	flag.Parse()

	start := time.Now()
	startedAt := start.UTC().Format(time.RFC3339)

	config, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(2)
	}

	if strings.TrimSpace(envName) == "" {
		if ciMode {
			envName = "ci"
		} else {
			envName = "local"
		}
	}

	baseline, err := loadBaseline(config.BaselinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "baseline error: %v\n", err)
		os.Exit(2)
	}

	state := &RuntimeState{BrokenWindowsMarkers: -1}

	checks := []CheckDefinition{
		{Principle: "ETC", Name: "Change Safety Net", ID: "etc-change-safety-net", Run: checkChangeSafetyNet},
		{Principle: "ETC", Name: "Formatting Hygiene", ID: "etc-formatting-hygiene", Run: checkFormattingHygiene},
		{Principle: "Tracer Bullets", Name: "Build Path", ID: "tracer-bullets-build-path", Run: checkTracerBulletsBuildPath},
		{Principle: "DRY", Name: "Large-Line Duplication", ID: "dry-large-line-duplication", Run: checkDryLargeLineDuplication},
		{Principle: "Broken Windows", Name: "Backlog Markers", ID: "broken-windows-backlog-markers", Run: makeCheckBrokenWindowsMarkers(state, baseline)},
		{Principle: "Orthogonality", Name: "Package Boundaries", ID: "orthogonality-package-boundaries", Run: checkOrthogonalityBoundaries},
		{Principle: "Automation", Name: "Environment", ID: "automation-environment", Run: makeCheckEnv(config.RequiredEnv)},
	}

	if listChecks {
		checkInfos := make([]CheckInfo, 0, len(checks))
		for _, check := range checks {
			checkInfos = append(checkInfos, CheckInfo{ID: check.ID, Principle: check.Principle, Name: check.Name})
		}

		if jsonOutput {
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			if encodeErr := encoder.Encode(checkInfos); encodeErr != nil {
				fmt.Fprintf(os.Stderr, "output error: %v\n", encodeErr)
				os.Exit(2)
			}
			return
		}

		fmt.Println("Available checks:")
		for _, check := range checkInfos {
			fmt.Printf("- %s [%s] %s\n", check.ID, check.Principle, check.Name)
		}
		return
	}

	checks, err = filterChecks(checks, splitCSV(enableChecks), splitCSV(disableChecks))
	if err != nil {
		fmt.Fprintf(os.Stderr, "flag error: %v\n", err)
		os.Exit(2)
	}

	if !jsonOutput {
		fmt.Println("🔍 Project Status")
		fmt.Println()
	}

	results := runChecks(checks)
	summary := summarize(results)
	durationMs := time.Since(start).Milliseconds()

	if jsonOutput {
		report := JSONReport{
			StartedAt:  startedAt,
			DurationMs: durationMs,
			Summary:    summary,
			Results:    results,
		}

		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if encodeErr := encoder.Encode(report); encodeErr != nil {
			fmt.Fprintf(os.Stderr, "output error: %v\n", encodeErr)
			os.Exit(2)
		}
	} else {
		printResults(results)
		fmt.Printf("\n⏱️ Completed in %.2fs\n", time.Since(start).Seconds())
	}

	if writeBaseline {
		if state.BrokenWindowsMarkers < 0 {
			count, _, _, scanErr := scanBrokenWindowsMarkers()
			if scanErr != nil {
				fmt.Fprintf(os.Stderr, "baseline error: %v\n", scanErr)
				os.Exit(2)
			}
			state.BrokenWindowsMarkers = count
		}

		if err := writeBaselineSnapshot(config.BaselinePath, state.BrokenWindowsMarkers); err != nil {
			fmt.Fprintf(os.Stderr, "baseline error: %v\n", err)
			os.Exit(2)
		}

		if !jsonOutput {
			fmt.Printf("📸 Baseline updated at %s (broken_windows_markers=%d)\n", config.BaselinePath, state.BrokenWindowsMarkers)
		}
	}

	threshold := resolveThreshold(config, envName)
	if shouldExitNonZero(summary, threshold) {
		os.Exit(1)
	}
}

func runChecks(checks []CheckDefinition) []CheckResult {
	results := make([]CheckResult, 0, len(checks))

	for _, check := range checks {
		result := check.Run()
		result.Principle = check.Principle
		result.Name = check.Name
		result.ID = check.ID
		results = append(results, result)
	}

	return results
}

func summarize(results []CheckResult) Summary {
	s := Summary{Total: len(results)}
	for _, r := range results {
		switch r.Status {
		case "ok":
			s.OK++
		case "warn":
			s.Warn++
		case "fail":
			s.Fail++
		}
	}
	return s
}

func checkChangeSafetyNet() CheckResult {
	cmd := exec.Command("go", "test", "./...")
	output, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(output))

	if err != nil {
		detail := "Tests are failing"
		if out != "" {
			detail = out
		}
		return CheckResult{
			Status: "fail",
			Detail: detail,
		}
	}

	if strings.Contains(out, "[no test files]") {
		return CheckResult{
			Status: "warn",
			Detail: "No test files found; change confidence is low",
		}
	}

	return CheckResult{
		Status: "ok",
		Detail: "All tests passing",
	}
}

func checkFormattingHygiene() CheckResult {
	cmd := exec.Command("gofmt", "-l", ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = "Unable to run gofmt"
		}
		return CheckResult{
			Status: "fail",
			Detail: detail,
		}
	}

	out := strings.TrimSpace(string(output))
	if out != "" {
		return CheckResult{
			Status: "warn",
			Detail: "Unformatted files: " + strings.ReplaceAll(out, "\n", ", "),
		}
	}

	return CheckResult{
		Status: "ok",
		Detail: "Formatting is consistent",
	}
}

func checkTracerBulletsBuildPath() CheckResult {
	cmd := exec.Command("go", "build", "./...")
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = "go build failed"
		}
		return CheckResult{Status: "fail", Detail: detail}
	}

	return CheckResult{Status: "ok", Detail: "One-command build path is healthy"}
}

func checkDryLargeLineDuplication() CheckResult {
	counts := map[string]int{}

	err := filepath.WalkDir(".", func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		if !isDryCheckFile(path) {
			return nil
		}

		f, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if len(line) < 24 {
				continue
			}

			if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
				continue
			}

			counts[line]++
		}

		return scanner.Err()
	})

	if err != nil {
		return CheckResult{Status: "fail", Detail: "Unable to scan files for duplication: " + err.Error()}
	}

	duplicates := make([]string, 0)
	for line, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, fmt.Sprintf("%q (%dx)", truncate(line, 72), count))
		}
	}

	if len(duplicates) > 0 {
		sort.Strings(duplicates)
		if len(duplicates) > 3 {
			duplicates = duplicates[:3]
		}
		return CheckResult{Status: "warn", Detail: "Possible duplicated knowledge: " + strings.Join(duplicates, "; ")}
	}

	return CheckResult{Status: "ok", Detail: "No obvious duplicated large lines detected"}
}

func makeCheckBrokenWindowsMarkers(state *RuntimeState, baseline *BaselineData) func() CheckResult {
	return func() CheckResult {
		totalMarkers, markerFiles, markerPattern, err := scanBrokenWindowsMarkers()
		if err != nil {
			return CheckResult{Status: "fail", Detail: "Unable to scan marker comments: " + err.Error()}
		}

		state.BrokenWindowsMarkers = totalMarkers

		if totalMarkers > 0 {
			sort.Strings(markerFiles)
			if len(markerFiles) > 3 {
				markerFiles = markerFiles[:3]
			}

			detail := fmt.Sprintf("Found %d %s-style markers: %s", totalMarkers, markerPattern, strings.Join(markerFiles, ", "))
			if baseline != nil {
				delta := totalMarkers - baseline.BrokenWindowsMarkers
				if delta > 0 {
					detail += fmt.Sprintf(" (increased by +%d vs baseline)", delta)
				} else if delta < 0 {
					detail += fmt.Sprintf(" (decreased by %d vs baseline)", delta)
				} else {
					detail += " (unchanged vs baseline)"
				}
			}

			return CheckResult{Status: "warn", Detail: detail}
		}

		if baseline != nil && baseline.BrokenWindowsMarkers > 0 {
			return CheckResult{Status: "ok", Detail: fmt.Sprintf("No TODO/FIXME-style markers found (improved from baseline=%d)", baseline.BrokenWindowsMarkers)}
		}

		return CheckResult{Status: "ok", Detail: "No TODO/FIXME-style markers found"}
	}
}

func scanBrokenWindowsMarkers() (int, []string, string, error) {
	markerPattern := regexp.MustCompile(`(?i)\b(TODO|FIXME|HACK|XXX)\b`)
	markerFiles := make([]string, 0)
	totalMarkers := 0

	err := filepath.WalkDir(".", func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		if !isGoSourceFile(path) {
			return nil
		}

		f, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer f.Close()

		fileCount := 0
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			commentIndex := strings.Index(line, "//")
			if commentIndex == -1 {
				continue
			}
			commentText := line[commentIndex+2:]
			if markerPattern.MatchString(commentText) {
				fileCount++
			}
		}

		if scanErr := scanner.Err(); scanErr != nil {
			return scanErr
		}

		if fileCount > 0 {
			totalMarkers += fileCount
			markerFiles = append(markerFiles, fmt.Sprintf("%s (%d)", path, fileCount))
		}

		return nil
	})

	if err != nil {
		return 0, nil, "TODO/FIXME", err
	}

	return totalMarkers, markerFiles, "TODO/FIXME", nil
}

func checkOrthogonalityBoundaries() CheckResult {
	cmd := exec.Command("go", "list", "./...")
	output, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(output))
	if err != nil {
		detail := out
		if detail == "" {
			detail = "go list failed"
		}
		return CheckResult{Status: "fail", Detail: detail}
	}

	packages := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			packages++
		}
	}

	if packages <= 1 {
		return CheckResult{Status: "warn", Detail: "Only one package detected; boundaries are not explicit yet"}
	}

	return CheckResult{Status: "ok", Detail: fmt.Sprintf("Detected %d packages with valid import graph", packages)}
}

func makeCheckEnv(required []string) func() CheckResult {
	requiredVars := required
	if len(requiredVars) == 0 {
		requiredVars = []string{"DATABASE_URL"}
	}

	return func() CheckResult {
		missing := make([]string, 0)
		for _, key := range requiredVars {
			if strings.TrimSpace(key) == "" {
				continue
			}
			if os.Getenv(key) == "" {
				missing = append(missing, key)
			}
		}

		if len(missing) > 0 {
			return CheckResult{
				Status: "warn",
				Detail: "Missing environment variables: " + strings.Join(missing, ", "),
			}
		}

		return CheckResult{Status: "ok", Detail: "Environment looks good"}
	}
}

func loadConfig(path string) (AppConfig, error) {
	defaultConfig := defaultAppConfig()

	if strings.TrimSpace(path) == "" {
		return defaultConfig, nil
	}

	data, configPath, err := readConfigData(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultConfig, nil
		}
		return AppConfig{}, err
	}

	config := defaultConfig
	if err := unmarshalConfigData(configPath, data, &config); err != nil {
		return AppConfig{}, err
	}

	if len(config.RequiredEnv) == 0 {
		config.RequiredEnv = defaultConfig.RequiredEnv
	}
	if strings.TrimSpace(config.BaselinePath) == "" {
		config.BaselinePath = defaultConfig.BaselinePath
	}
	if config.Thresholds == nil {
		config.Thresholds = defaultConfig.Thresholds
	}
	if _, ok := config.Thresholds["local"]; !ok {
		config.Thresholds["local"] = defaultConfig.Thresholds["local"]
	}
	if _, ok := config.Thresholds["ci"]; !ok {
		config.Thresholds["ci"] = defaultConfig.Thresholds["ci"]
	}

	return config, nil
}

func defaultAppConfig() AppConfig {
	return AppConfig{
		RequiredEnv:  []string{"DATABASE_URL"},
		BaselinePath: ".devtool-baseline.json",
		Thresholds: map[string]ThresholdConfig{
			"local": {MaxWarn: -1, MaxFail: -1},
			"ci":    {MaxWarn: 0, MaxFail: 0},
		},
	}
}

func readConfigData(path string) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, path, nil
	}

	if !(errors.Is(err, os.ErrNotExist) && path == "devtool.json") {
		return nil, "", err
	}

	for _, candidate := range []string{"devtool.yaml", "devtool.yml"} {
		candidateData, candidateErr := os.ReadFile(candidate)
		if candidateErr == nil {
			return candidateData, candidate, nil
		}
		if !errors.Is(candidateErr, os.ErrNotExist) {
			return nil, "", candidateErr
		}
	}

	return nil, "", os.ErrNotExist
}

func unmarshalConfigData(path string, data []byte, out *AppConfig) error {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, out); err != nil {
			return fmt.Errorf("invalid YAML in %s: %w", path, err)
		}
	default:
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("invalid JSON in %s: %w", path, err)
		}
	}

	return nil
}

func loadBaseline(path string) (*BaselineData, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	baseline := &BaselineData{}
	if err := json.Unmarshal(data, baseline); err != nil {
		return nil, fmt.Errorf("invalid baseline JSON in %s: %w", path, err)
	}

	return baseline, nil
}

func writeBaselineSnapshot(path string, markerCount int) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("baseline path is empty")
	}

	baseline := BaselineData{BrokenWindowsMarkers: markerCount, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func resolveThreshold(config AppConfig, envName string) ThresholdConfig {
	if threshold, ok := config.Thresholds[envName]; ok {
		return threshold
	}

	if envName != "local" {
		if threshold, ok := config.Thresholds["local"]; ok {
			return threshold
		}
	}

	return ThresholdConfig{MaxWarn: -1, MaxFail: -1}
}

func shouldExitNonZero(summary Summary, threshold ThresholdConfig) bool {
	if threshold.MaxFail >= 0 && summary.Fail > threshold.MaxFail {
		return true
	}
	if threshold.MaxWarn >= 0 && summary.Warn > threshold.MaxWarn {
		return true
	}
	return false
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func filterChecks(checks []CheckDefinition, enabled []string, disabled []string) ([]CheckDefinition, error) {
	available := map[string]bool{}
	for _, check := range checks {
		available[check.ID] = true
	}

	for _, id := range enabled {
		if !available[id] {
			return nil, fmt.Errorf("unknown check in --enable: %s", id)
		}
	}
	for _, id := range disabled {
		if !available[id] {
			return nil, fmt.Errorf("unknown check in --disable: %s", id)
		}
	}

	enabledSet := map[string]bool{}
	for _, id := range enabled {
		enabledSet[id] = true
	}
	disabledSet := map[string]bool{}
	for _, id := range disabled {
		disabledSet[id] = true
	}

	filtered := make([]CheckDefinition, 0, len(checks))
	for _, check := range checks {
		if len(enabledSet) > 0 && !enabledSet[check.ID] {
			continue
		}
		if disabledSet[check.ID] {
			continue
		}
		filtered = append(filtered, check)
	}

	if len(filtered) == 0 {
		return nil, errors.New("no checks selected")
	}

	return filtered, nil
}

func isDryCheckFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".env") {
		return true
	}

	ext := filepath.Ext(path)
	switch ext {
	case ".md", ".txt", ".yaml", ".yml", ".toml", ".json":
		return true
	default:
		return false
	}
}

func isGoSourceFile(path string) bool {
	return filepath.Ext(path) == ".go"
}

func truncate(input string, max int) string {
	if len(input) <= max {
		return input
	}
	return input[:max-3] + "..."
}

func printResults(results []CheckResult) {
	for _, r := range results {
		icon := map[string]string{
			"ok":   "✅",
			"warn": "⚠️",
			"fail": "❌",
		}[r.Status]

		fmt.Printf("%s [%s] %s: %s\n", icon, r.Principle, r.Name, r.Detail)
	}
}
