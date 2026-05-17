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


// ====================== NORMAL TEST ======================
func runNormalTest(skupperVersion string, config TestConfig, rawData []byte) error {
	if config.TestType == "" { config.TestType = "throughput" }
	if config.TestName == "" { config.TestName = "unnamed_test" }
	if config.Duration == 0 { config.Duration = 10 }
	if config.Parallel == 0 { config.Parallel = 1 }
	if config.Protocol == "" { config.Protocol = "tcp" }
	if config.Port == 0 { config.Port = 5201 }
	if config.Routers < 0 { config.Routers = 0 }
	if config.YMaxMbps <= 0 { config.YMaxMbps = 600000 }

	dateStr := time.Now().Format("2006_01_02")
	routerStr := fmt.Sprintf("%d_routers", config.Routers)

	baseDir := filepath.Join("skrp_results", skupperVersion, config.TestType, dateStr, config.TestName, routerStr)
	outputDir := filepath.Join(baseDir, "output")
	commandsDir := filepath.Join(baseDir, "commands")
	dataDir := filepath.Join(outputDir, "data")
	graphicsDir := filepath.Join(baseDir, "graphics")

	for _, dir := range []string{outputDir, commandsDir, dataDir, graphicsDir} {
		os.MkdirAll(dir, 0755)
	}

	// Write commands FIRST
	writeCommands(config, commandsDir)

	// Save metadata
	type RunInfo struct {
		SkupperVersion string     `json:"skupper_version"`
		TestConfig     TestConfig `json:"test_config"`
		RunTime        time.Time  `json:"run_time"`
	}
	runInfo := RunInfo{SkupperVersion: skupperVersion, TestConfig: config, RunTime: time.Now()}
	infoBytes, _ := json.MarshalIndent(runInfo, "", "  ")
	_ = os.WriteFile(filepath.Join(outputDir, "run_info.json"), infoBytes, 0644)
	_ = os.WriteFile(filepath.Join(outputDir, "config_used.json"), rawData, 0644)

	// Start routers
	var routerProcs []*os.Process
	if config.Routers > 0 {
		fmt.Printf("   → Starting %d router(s)...\n", config.Routers)
		routerProcs, _ = startSkupperRouters(config.Routers, baseDir, commandsDir, config.CPU)
		defer cleanupRouters(routerProcs)

		waitTime := 5 * time.Second
		if config.Routers >= 2 {
			waitTime = 10 * time.Second
		}
		fmt.Printf("   → Waiting %v for routers to sync...\n", waitTime)
		time.Sleep(waitTime)

		waitForRouterReady()
	}

	fmt.Printf("   → Running iperf3 test with %d router(s)\n", config.Routers)

	if err := runIperf3Test(config, outputDir, dataDir, graphicsDir, commandsDir); err != nil {
		fmt.Printf("   Warning: iperf3 had issues: %v\n", err)
	}

	fmt.Printf("✅ Test completed! Y-max = %d Mbps\n", config.YMaxMbps)
	return nil
}

func runIperf3Test(config TestConfig, outputDir, dataDir, graphicsDir, commandsDir string) error {
	serverPort := config.Port
	clientPort := config.Port
	if config.Routers >= 1 {
		serverPort = 5801
		clientPort = 5800
	}

	fmt.Printf("   → Starting iperf3 server on port %d\n", serverPort)
	serverCmd := exec.Command("iperf3", "-s", "-p", strconv.Itoa(serverPort), "-1")
	serverCmd.Stderr = os.Stderr
	serverCmd.Start()
	time.Sleep(2 * time.Second)

	fmt.Printf("   → Starting iperf3 client to port %d\n", clientPort)
	clientArgs := []string{
		"-c", "127.0.0.1",
		"-p", strconv.Itoa(clientPort),
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
	_ = os.WriteFile(filepath.Join(outputDir, "iperf3_client_output.json"), output, 0644)

	if err != nil {
		fmt.Printf("   Warning: iperf3 client error: %v\n", err)
	} else {
		fmt.Println("   → iperf3 client completed successfully")
	}

	time.Sleep(1 * time.Second)
	serverCmd.Process.Kill()
	serverCmd.Wait()

	processIperf3Output(filepath.Join(outputDir, "iperf3_client_output.json"), dataDir, graphicsDir, config)
	return nil
}

