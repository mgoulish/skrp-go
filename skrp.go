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
	TestType       string `json:"test_type"`       // "throughput", "latency", ...
	TestName       string `json:"test_name"`
	Duration       int    `json:"duration_seconds"`
	Parallel       int    `json:"parallel_streams"`
	Protocol       string `json:"protocol"`
	Port           int    `json:"port"`
	// skupper_version is now taken from command line instead
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: ./skrp <skupper_version> <config1.json> [config2.json] ...")
		fmt.Println("Example: ./skrp 2.2.0 throughput_test.json")
		os.Exit(1)
	}

	skupperVersion := os.Args[1]
	configFiles := os.Args[2:]

	fmt.Printf("🚀 SKRP - Skupper Router Performance Tester\n")
	fmt.Printf("Skupper Version: %s\n\n", skupperVersion)

	for i, configPath := range configFiles {
		fmt.Printf("=== Test %d/%d : %s ===\n", i+1, len(configFiles), configPath)
		if err := runTest(skupperVersion, configPath); err != nil {
			fmt.Printf("❌ Test failed: %v\n", err)
		}
	}
}

func runTest(skupperVersion, configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config %s: %w", configPath, err)
	}

	var config TestConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config %s: %w", configPath, err)
	}

	// Defaults
	if config.TestType == "" {
		config.TestType = "throughput"
	}
	if config.TestName == "" {
		config.TestName = "unnamed_test"
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

	// === NEW DIRECTORY STRUCTURE ===
	// skrp_results / <skupper_version> / <test_type> / YYYY_MM_DD / <test_name> / 0_routers / ...
	dateStr := time.Now().Format("2006_01_02")

	resultsDir := filepath.Join(
		"skrp_results",
		skupperVersion,      // ← New top-level version directory
		config.TestType,
		dateStr,
		config.TestName,
		"0_routers",
		"output",
	)

	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return fmt.Errorf("failed to create results directory: %w", err)
	}

	// Save config + version info
	type RunInfo struct {
		SkupperVersion string    `json:"skupper_version"`
		TestConfig     TestConfig `json:"test_config"`
		RunTime        time.Time `json:"run_time"`
	}
	runInfo := RunInfo{
		SkupperVersion: skupperVersion,
		TestConfig:     config,
		RunTime:        time.Now(),
	}

	infoBytes, _ := json.MarshalIndent(runInfo, "", "  ")
	_ = os.WriteFile(filepath.Join(resultsDir, "run_info.json"), infoBytes, 0644)
	_ = os.WriteFile(filepath.Join(resultsDir, "config_used.json"), data, 0644)

	fmt.Printf("Test Type   : %s\n", config.TestType)
	fmt.Printf("Test Name   : %s\n", config.TestName)
	fmt.Printf("Duration    : %d seconds\n", config.Duration)
	fmt.Printf("Parallel    : %d streams\n", config.Parallel)
	fmt.Printf("Results     : %s\n\n", resultsDir)

	if err := runIperf3Test(config, resultsDir); err != nil {
		return err
	}

	fmt.Printf("✅ Test completed successfully!\n")
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
		"-J",
	}
	if config.Protocol == "udp" {
		clientArgs = append(clientArgs, "-u")
	}

	clientCmd := exec.Command("iperf3", clientArgs...)
	output, err := clientCmd.CombinedOutput()

	outputPath := filepath.Join(resultsDir, "iperf3_client_output.json")
	if writeErr := os.WriteFile(outputPath, output, 0644); writeErr != nil {
		fmt.Printf("Warning: failed to save output: %v\n", writeErr)
	}

	serverCmd.Process.Kill()
	serverCmd.Wait()

	if err != nil {
		return fmt.Errorf("iperf3 client error: %w\n%s", err, string(output))
	}

	return nil
}
