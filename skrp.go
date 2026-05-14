package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

type TestConfig struct {
	SkupperVersion string `json:"skupper_version"` // e.g. "2.5.1" or "none" for baseline
	TestType       string `json:"test_type"`       // NEW: "throughput", "latency", etc.
	TestName       string `json:"test_name"`
	Duration       int    `json:"duration_seconds"`
	Parallel       int    `json:"parallel_streams"`
	Protocol       string `json:"protocol"`
	Port           int    `json:"port"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: ./skrp <config1.json> [config2.json] ...")
		os.Exit(1)
	}

	for i, configPath := range os.Args[1:] {
		fmt.Printf("\n=== Running test %d: %s ===\n", i+1, configPath)
		if err := runTest(configPath); err != nil {
			fmt.Printf("❌ Test failed: %v\n", err)
		}
	}
}

func runTest(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var config TestConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Defaults
	if config.TestType == "" {
		config.TestType = "throughput"
	}
	if config.SkupperVersion == "" {
		config.SkupperVersion = "none"
	}
	if config.TestName == "" {
		config.TestName = "unnamed"
	}
	if config.Duration == 0 {
		config.Duration = 10
	}
	if config.Parallel == 0 {
		config.Parallel = 1
	}
	if config.Protocol == "" {
		config.Protocol = "tcp"
	}
	if config.Port == 0 {
		config.Port = 5201
	}

	// New directory structure:
	// skrp_results / TEST_TYPE / YYYY_MM_DD / TEST_NAME / 0_routers / ...
	dateStr := time.Now().Format("2006_01_02")
	resultsDir := filepath.Join(
		"skrp_results",
		config.TestType,           // ← now "throughput" instead of version
		dateStr,
		config.TestName,
		"0_routers",
		"output",
	)

	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return fmt.Errorf("failed to create results dir: %w", err)
	}

	// Save config for reproducibility
	configCopyPath := filepath.Join(resultsDir, "config_used.json")
	_ = os.WriteFile(configCopyPath, data, 0644)

	fmt.Printf("🚀 0-router iperf3 %s test\n", config.TestType)
	fmt.Printf("  Test type   : %s\n", config.TestType)
	fmt.Printf("  Test name   : %s\n", config.TestName)
	fmt.Printf("  Skupper ver : %s\n", config.SkupperVersion)
	fmt.Printf("  Duration    : %d s\n", config.Duration)
	fmt.Printf("  Parallel    : %d streams\n", config.Parallel)
	fmt.Printf("  Results     : %s\n", resultsDir)

	if err := runIperf3Test(config, resultsDir); err != nil {
		return err
	}

	fmt.Printf("✅ Test completed successfully!\n")
	fmt.Println("   Raw output saved. Next: parsing + graphics...")

	return nil
}

func runIperf3Test(config TestConfig, resultsDir string) error {
	// iperf3 server (one-shot)
	serverCmd := exec.Command("iperf3", "-s", "-p", strconv.Itoa(config.Port), "-1")
	serverCmd.Stderr = os.Stderr
	if err := serverCmd.Start(); err != nil {
		return fmt.Errorf("failed to start iperf3 server: %w", err)
	}
	time.Sleep(800 * time.Millisecond)

	// iperf3 client
	clientArgs := []string{
		"-c", "127.0.0.1",
		"-p", strconv.Itoa(config.Port),
		"-t", strconv.Itoa(config.Duration),
		"-P", strconv.Itoa(config.Parallel),
		"-f", "m",
		"-J", // JSON output
	}
	if config.Protocol == "udp" {
		clientArgs = append(clientArgs, "-u")
	}

	clientCmd := exec.Command("iperf3", clientArgs...)
	output, err := clientCmd.CombinedOutput()

	// Save raw output
	outputPath := filepath.Join(resultsDir, "iperf3_client_output.json")
	if writeErr := os.WriteFile(outputPath, output, 0644); writeErr != nil {
		fmt.Printf("Warning: could not write output file: %v\n", writeErr)
	}

	serverCmd.Process.Kill()
	serverCmd.Wait()

	if err != nil {
		return fmt.Errorf("iperf3 failed: %w\n%s", err, string(output))
	}

	return nil
}
