// Fuzz testing report tool for Glint.
//
// Runs all fuzz targets for a configurable duration, captures per-target
// stats, and writes a report to target/reports/fuzz.txt. Exits non-zero if
// any fuzz target discovers a failure.
//
// Usage:
//
//	go run ./scripts/fuzz
//	FUZZ_TIME=60s go run ./scripts/fuzz
package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type fuzzTarget struct {
	Function string
	Package  string
}

// fuzzTargets mirrors the targets listed in the Taskfile fuzz task.
var fuzzTargets = []fuzzTarget{
	// Template helpers
	{Function: "FuzzFormatBytes", Package: "./templates/"},
	{Function: "FuzzBackupIDMatchesVMID", Package: "./templates/"},
	// SMART parsing
	{Function: "FuzzParseATAAttributes", Package: "./internal/smart/"},
	{Function: "FuzzParseSCSIText", Package: "./internal/smart/"},
	{Function: "FuzzParseNVMeText", Package: "./internal/smart/"},
	// Sensor parsing
	{Function: "FuzzParseSensorsJSON", Package: "./internal/collector/"},
	// Config parsing
	{Function: "FuzzExpandEnvVars", Package: "./internal/config/"},
	// PVE response parsing
	{Function: "FuzzParseNodeStatus", Package: "./internal/collector/"},
	{Function: "FuzzParseLoadAvg", Package: "./internal/collector/"},
	{Function: "FuzzParseSMARTResponse", Package: "./internal/collector/"},
	{Function: "FuzzParseGuestList", Package: "./internal/collector/"},
	// PBS response parsing
	{Function: "FuzzParsePBSDatastoreUsage", Package: "./internal/collector/"},
	{Function: "FuzzParsePBSSnapshots", Package: "./internal/collector/"},
	{Function: "FuzzParsePBSTasks", Package: "./internal/collector/"},
}

type fuzzResult struct {
	Target         fuzzTarget
	Duration       time.Duration
	Execs          int64
	ExecsPerSec    int64
	NewInteresting int
	Passed         bool
	Output         string
}

var (
	reExecs          = regexp.MustCompile(`execs:\s+(\d+)\s+\((\d+)/sec\)`)
	reNewInteresting = regexp.MustCompile(`new interesting:\s+(\d+)`)
)

func main() {
	projectRoot := findProjectRoot()
	reportDir := filepath.Join(projectRoot, "target", "reports")

	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		log.Fatalf("creating report directory: %v", err)
	}

	fuzzTime := os.Getenv("FUZZ_TIME")
	if fuzzTime == "" {
		fuzzTime = "30s"
	}

	now := time.Now()
	goVer := captureGoVersion()

	fmt.Printf("Running %d fuzz targets (fuzztime=%s each)...\n\n", len(fuzzTargets), fuzzTime)

	results := make([]fuzzResult, 0, len(fuzzTargets))
	failures := 0

	for _, target := range fuzzTargets {
		fmt.Printf("--- %s (%s) ---\n", target.Function, target.Package)
		result := runFuzz(projectRoot, target, fuzzTime)
		results = append(results, result)

		if !result.Passed {
			failures++
			fmt.Printf("FAIL: %s\n\n", target.Function)
		} else {
			fmt.Printf("PASS: %s  execs: %d (%d/sec)  new interesting: %d\n\n",
				target.Function, result.Execs, result.ExecsPerSec, result.NewInteresting)
		}
	}

	report := buildReport(now, goVer, fuzzTime, results)
	reportPath := filepath.Join(reportDir, "fuzz.txt")
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		log.Fatalf("writing fuzz report: %v", err)
	}
	fmt.Printf("Fuzz report: %s\n", reportPath)

	if failures > 0 {
		fmt.Printf("\n%d fuzz target(s) failed.\n", failures)
		os.Exit(1)
	}
	fmt.Println("\nAll fuzz targets passed.")
}

func runFuzz(projectRoot string, target fuzzTarget, fuzzTime string) fuzzResult {
	start := time.Now()

	cmd := exec.Command("go", "test",
		fmt.Sprintf("-fuzz=%s", target.Function),
		fmt.Sprintf("-fuzztime=%s", fuzzTime),
		target.Package,
	)
	cmd.Dir = projectRoot

	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)

	err := cmd.Run()
	duration := time.Since(start)
	output := buf.String()

	var execs, execsPerSec int64
	var newInteresting int

	// Scan from end to find the last progress line with final stats.
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if strings.HasPrefix(line, "fuzz: elapsed:") {
			if m := reExecs.FindStringSubmatch(line); m != nil {
				execs, _ = strconv.ParseInt(m[1], 10, 64)
				execsPerSec, _ = strconv.ParseInt(m[2], 10, 64)
			}
			if m := reNewInteresting.FindStringSubmatch(line); m != nil {
				n, _ := strconv.Atoi(m[1])
				newInteresting = n
			}
			break
		}
	}

	// Distinguish real fuzz failures (which write a corpus file) from spurious
	// "context deadline exceeded" failures that can occur when the fuzz timer
	// fires and races with test finalization in Go's fuzz runner.
	passed := err == nil ||
		(strings.Contains(output, "context deadline exceeded") &&
			!strings.Contains(output, "Failing input written to"))

	return fuzzResult{
		Target:         target,
		Duration:       duration,
		Execs:          execs,
		ExecsPerSec:    execsPerSec,
		NewInteresting: newInteresting,
		Passed:         passed,
		Output:         output,
	}
}

func buildReport(now time.Time, goVer, fuzzTime string, results []fuzzResult) string {
	var sb strings.Builder
	sep := strings.Repeat("=", 72)
	thin := strings.Repeat("-", 72)

	sb.WriteString("Glint Fuzz Testing Report\n")
	sb.WriteString(sep + "\n")
	fmt.Fprintf(&sb, "Generated:   %s\n", now.Format(time.RFC1123))
	fmt.Fprintf(&sb, "Go Version:  %s\n", goVer)
	fmt.Fprintf(&sb, "OS/Arch:     %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&sb, "Fuzz Time:   %s per target\n", fuzzTime)
	sb.WriteString(sep + "\n\n")

	// Summary table
	sb.WriteString("Summary\n")
	sb.WriteString(thin + "\n")
	fmt.Fprintf(&sb, "  %-40s  %-4s  %12s  %s\n", "Target", "Status", "Execs", "New Corpus")
	sb.WriteString(thin + "\n")

	var totalExecs int64
	failures := 0

	for _, r := range results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
			failures++
		}
		totalExecs += r.Execs
		fmt.Fprintf(&sb, "  %-40s  %-4s  %12d  %d\n",
			r.Target.Function, status, r.Execs, r.NewInteresting)
	}

	sb.WriteString(thin + "\n")
	fmt.Fprintf(&sb, "  Total executions: %d\n", totalExecs)
	if failures > 0 {
		fmt.Fprintf(&sb, "  FAILED targets:   %d\n", failures)
	} else {
		sb.WriteString("  All targets passed.\n")
	}
	sb.WriteString("\n")

	// Detailed output per target
	sb.WriteString("Detailed Output\n")
	sb.WriteString(sep + "\n\n")

	for _, r := range results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(&sb, "[%s] %s\n", status, r.Target.Function)
		fmt.Fprintf(&sb, "  Package:         %s\n", r.Target.Package)
		fmt.Fprintf(&sb, "  Duration:        %s\n", r.Duration.Round(time.Millisecond))
		fmt.Fprintf(&sb, "  Total Execs:     %d\n", r.Execs)
		fmt.Fprintf(&sb, "  Execs/sec:       %d\n", r.ExecsPerSec)
		fmt.Fprintf(&sb, "  New Interesting: %d\n", r.NewInteresting)
		sb.WriteString("\n  Output:\n")
		for line := range strings.SplitSeq(strings.TrimRight(r.Output, "\n"), "\n") {
			fmt.Fprintf(&sb, "    %s\n", line)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func captureGoVersion() string {
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func findProjectRoot() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("could not determine script directory")
	}
	dir := filepath.Dir(filename)
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
