package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

type CheckResult struct {
	Name   string
	Status string // "ok", "warn", "fail"
	Detail string
}

func main() {
	start := time.Now()

	fmt.Println("🔍 Project Status\n")

	results := []CheckResult{
		checkTests(),
		checkEnv(),
	}

	printResults(results)

	fmt.Printf("\n⏱️ Completed in %.2fs\n", time.Since(start).Seconds())
}

func checkTests() CheckResult {
	cmd := exec.Command("go", "test", "./...")
	err := cmd.Run()

	if err != nil {
		return CheckResult{
			Name:   "Tests",
			Status: "fail",
			Detail: "Tests are failing",
		}
	}

	return CheckResult{
		Name:   "Tests",
		Status: "ok",
		Detail: "All tests passing",
	}
}

func checkEnv() CheckResult {
	if os.Getenv("DATABASE_URL") == "" {
		return CheckResult{
			Name:   "ENV",
			Status: "warn",
			Detail: "DATABASE_URL not set",
		}
	}

	return CheckResult{
		Name:   "ENV",
		Status: "ok",
		Detail: "Environment looks good",
	}
}

func printResults(results []CheckResult) {
	for _, r := range results {
		icon := map[string]string{
			"ok":   "✅",
			"warn": "⚠️",
			"fail": "❌",
		}[r.Status]

		fmt.Printf("%s %s: %s\n", icon, r.Name, r.Detail)
	}
}
