// Coverage tool for Glint.
//
// Runs tests with coverage, checks against a minimum threshold stored in
// coverage_required.txt, and auto-ratchets the threshold upward when
// coverage improves. Fails the build if coverage drops below the threshold.
//
// Usage:
//
//	go run ./scripts/coverage
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func main() {
	scriptDir := findScriptDir()
	coverageRequiredFile := filepath.Join(scriptDir, "coverage_required.txt")
	reportDir := filepath.Join(findProjectRoot(), "target", "reports")

	// Ensure report directory exists
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		log.Fatalf("creating report directory: %v", err)
	}

	coverageRequired, err := readCoverageRequired(coverageRequiredFile)
	if err != nil {
		log.Fatalf("reading coverage required: %v", err)
	}
	fmt.Printf("Coverage threshold: %d%%\n\n", coverageRequired)

	coverprofile := filepath.Join(reportDir, "coverage.out")
	filteredProfile := filepath.Join(reportDir, "coverage-filtered.out")

	// Run tests with coverage (include templates package for helpers.go)
	args := []string{
		"test",
		"./internal/...",
		"./templates/...",
		"-v",
		"-count=1",
		"-race",
		fmt.Sprintf("-coverprofile=%s", coverprofile),
	}

	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = findProjectRoot()

	if err := cmd.Run(); err != nil {
		log.Fatalf("tests failed: %v", err)
	}

	// Filter out generated templ files from coverage profile
	if err := filterCoverageProfile(coverprofile, filteredProfile); err != nil {
		log.Fatalf("filtering coverage profile: %v", err)
	}

	// Generate coverage report from filtered profile
	fmt.Println("\nCoverage report:")
	out, err := exec.Command("go", "tool", "cover", fmt.Sprintf("-func=%s", filteredProfile)).Output()
	if err != nil {
		log.Fatalf("generating coverage report: %v", err)
	}
	allCoverage := string(out)
	fmt.Println(allCoverage)

	totalCoverage, err := extractTotalCoverage(allCoverage)
	if err != nil {
		log.Fatalf("extracting total coverage: %v", err)
	}

	fmt.Printf("Total coverage: %d%%\n", totalCoverage)
	fmt.Printf("Required:       %d%%\n", coverageRequired)

	// Auto-ratchet: if coverage improved, update the threshold
	if totalCoverage > coverageRequired {
		fmt.Printf("\nCoverage improved! Updating threshold from %d%% to %d%%\n", coverageRequired, totalCoverage)
		if err := updateCoverageRequired(coverageRequiredFile, totalCoverage); err != nil {
			log.Fatalf("updating coverage required: %v", err)
		}
	}

	// Fail if coverage dropped
	if totalCoverage < coverageRequired {
		fmt.Printf("\nCoverage %d%% is below threshold %d%%, failing build\n", totalCoverage, coverageRequired)
		os.Exit(1)
	}

	// Generate HTML report
	htmlReport := filepath.Join(reportDir, "coverage.html")
	htmlCmd := exec.Command("go", "tool", "cover", fmt.Sprintf("-html=%s", filteredProfile), "-o", htmlReport)
	if err := htmlCmd.Run(); err != nil {
		fmt.Printf("Warning: could not generate HTML report: %v\n", err)
	} else {
		fmt.Printf("\nHTML coverage report: %s\n", htmlReport)
	}

	fmt.Println("\nCoverage check passed!")
}

func extractTotalCoverage(allCoverage string) (int, error) {
	lines := strings.Split(allCoverage, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "total:") {
			parts := strings.Fields(line)
			if len(parts) < 3 {
				return 0, fmt.Errorf("unexpected total coverage line format: %s", line)
			}
			coverageStr := strings.TrimSuffix(parts[2], "%")
			coverageFloat, err := strconv.ParseFloat(coverageStr, 64)
			if err != nil {
				return 0, fmt.Errorf("parsing coverage percentage %q: %w", coverageStr, err)
			}
			return int(coverageFloat), nil
		}
	}
	return 0, fmt.Errorf("total coverage not found in output")
}

func readCoverageRequired(path string) (int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}
	val, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		return 0, fmt.Errorf("parsing coverage value from %s: %w", path, err)
	}
	return val, nil
}

func updateCoverageRequired(path string, coverage int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(coverage)+"\n"), 0o644)
}

func findScriptDir() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("could not determine script directory")
	}
	return filepath.Dir(filename)
}

// filterCoverageProfile removes generated templ files (_templ.go) from coverage.
func filterCoverageProfile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	}

	lines := strings.Split(string(data), "\n")
	var filtered []string
	for _, line := range lines {
		// Keep the mode line and any line that isn't from a _templ.go file
		if strings.HasPrefix(line, "mode:") || !strings.Contains(line, "_templ.go:") {
			filtered = append(filtered, line)
		}
	}

	return os.WriteFile(dst, []byte(strings.Join(filtered, "\n")), 0o644)
}

func findProjectRoot() string {
	dir := findScriptDir()
	// Walk up from buildscripts/coverage to project root
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			log.Fatal("could not find project root (no go.mod found)")
		}
		dir = parent
	}
}
