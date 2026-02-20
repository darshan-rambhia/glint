// Benchmark report tool for Glint.
//
// Runs all benchmarks, captures output, and writes a timestamped report to
// target/reports/bench.txt. Exits non-zero if any benchmarks fail.
//
// Usage:
//
//	go run ./scripts/bench
//	BENCH_TIME=10s go run ./scripts/bench
package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func main() {
	projectRoot := findProjectRoot()
	reportDir := filepath.Join(projectRoot, "target", "reports")

	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		log.Fatalf("creating report directory: %v", err)
	}

	benchTime := os.Getenv("BENCH_TIME")
	if benchTime == "" {
		benchTime = "3s"
	}

	now := time.Now()
	goVer := captureGoVersion()

	fmt.Printf("Running benchmarks (benchtime=%s)...\n\n", benchTime)

	cmd := exec.Command("go", "test",
		"-bench=.",
		"-benchmem",
		fmt.Sprintf("-benchtime=%s", benchTime),
		"-run=^$",
		"./...",
	)
	cmd.Dir = projectRoot

	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)

	runErr := cmd.Run()

	var report strings.Builder
	sep := strings.Repeat("=", 72)
	report.WriteString("Glint Benchmark Report\n")
	report.WriteString(sep + "\n")
	fmt.Fprintf(&report, "Generated:      %s\n", now.Format(time.RFC1123))
	fmt.Fprintf(&report, "Go Version:     %s\n", goVer)
	fmt.Fprintf(&report, "OS/Arch:        %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&report, "Benchmark Time: %s per benchmark\n", benchTime)
	report.WriteString(sep + "\n\n")
	report.WriteString(buf.String())
	if runErr != nil {
		fmt.Fprintf(&report, "\n[ERROR] %v\n", runErr)
	}

	reportPath := filepath.Join(reportDir, "bench.txt")
	if err := os.WriteFile(reportPath, []byte(report.String()), 0o644); err != nil {
		log.Fatalf("writing bench report: %v", err)
	}
	fmt.Printf("\nBenchmark report: %s\n", reportPath)

	if runErr != nil {
		os.Exit(1)
	}
	fmt.Println("Benchmark run complete.")
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
