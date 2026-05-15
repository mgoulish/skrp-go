package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type TestConfig struct {
	TestType       string `json:"test_type"`
	TestName       string `json:"test_name"`
	Duration       int    `json:"duration_seconds"`
	Parallel       int    `json:"parallel_streams"`
	Protocol       string `json:"protocol"`
	Port           int    `json:"port"`
	Routers        int    `json:"routers"`
	YMaxMbps       int    `json:"y_max_mbps"`
}

type Iperf3Result struct {
	End struct {
		SumSent struct {
			BitsPerSecond float64 `json:"bits_per_second"`
		} `json:"sum_sent"`
	} `json:"end"`
	Intervals []struct {
		Sum struct {
			BitsPerSecond float64 `json:"bits_per_second"`
		} `json:"sum"`
	} `json:"intervals"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: ./skrp <skupper_version> <config1.json> [config2.json] ...")
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
		return fmt.Errorf("failed to read config: %w", err)
	}

	var config TestConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Defaults
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

	// Save metadata
	type RunInfo struct {
		SkupperVersion string     `json:"skupper_version"`
		TestConfig     TestConfig `json:"test_config"`
		RunTime        time.Time  `json:"run_time"`
	}
	runInfo := RunInfo{SkupperVersion: skupperVersion, TestConfig: config, RunTime: time.Now()}
	infoBytes, _ := json.MarshalIndent(runInfo, "", "  ")
	_ = os.WriteFile(filepath.Join(outputDir, "run_info.json"), infoBytes, 0644)
	_ = os.WriteFile(filepath.Join(outputDir, "config_used.json"), data, 0644)

	// Start routers if needed
	var routerProcs []*os.Process
	if config.Routers > 0 {
		fmt.Printf("   → Starting %d Skupper Router(s)...\n", config.Routers)
		var err error
		routerProcs, err = startSkupperRouters(config.Routers, baseDir, commandsDir)
		if err != nil {
			return err
		}
		defer cleanupRouters(routerProcs)
		time.Sleep(3 * time.Second)
	}

	fmt.Printf("   → Running iperf3 %s test with %d router(s)\n", config.Protocol, config.Routers)

	if err := runIperf3Test(config, outputDir, dataDir, graphicsDir, commandsDir); err != nil {
		return err
	}

	fmt.Printf("✅ Test completed!\n")
	fmt.Printf("   Y-axis max: %d Mbps\n", config.YMaxMbps)
	return nil
}

// ====================== Multi-Router Support ======================
func startSkupperRouters(numRouters int, baseDir, commandsDir string) ([]*os.Process, error) {
	var procs []*os.Process

	// Router A (always needed for 1+ routers)
	routerAConfig := `router {
    mode: interior
    id: skrp-router-A
    workerThreads: 4
}
listener {
    stripAnnotations: no
    idleTimeoutSeconds: 120
    saslMechanisms: ANONYMOUS
    host: 0.0.0.0
    role: inter-router
    authenticatePeer: no
    port: 25000
}
tcpListener {
    host: 0.0.0.0
    port: 5800
    address: router-test
    siteId: skrp-two-router-test
}
`
	_ = os.WriteFile(filepath.Join(baseDir, "router-A.conf"), []byte(routerAConfig), 0644)
	_ = os.WriteFile(filepath.Join(commandsDir, "router-A.conf"), []byte(routerAConfig), 0644)

	cmdA := exec.Command("skrouterd", "-c", filepath.Join(baseDir, "router-A.conf"))
	cmdA.Stdout = os.Stdout
	cmdA.Stderr = os.Stderr
	cmdA.Start()
	procs = append(procs, cmdA.Process)
	fmt.Printf("   → Router A started (PID %d)\n", cmdA.Process.Pid)

	if numRouters >= 2 {
		// Router B
		routerBConfig := `router {
    mode: interior
    id: skrp-router-B
    workerThreads: 4
}
connector {
    stripAnnotations: no
    name: connectorToA
    idleTimeoutSeconds: 120
    saslMechanisms: ANONYMOUS
    host: 127.0.0.1
    role: inter-router
    port: 25000
}
tcpConnector {
    host: 127.0.0.1
    port: 5801
    address: router-test
    siteId: skrp-two-router-test
}
`
		_ = os.WriteFile(filepath.Join(baseDir, "router-B.conf"), []byte(routerBConfig), 0644)
		_ = os.WriteFile(filepath.Join(commandsDir, "router-B.conf"), []byte(routerBConfig), 0644)

		cmdB := exec.Command("skrouterd", "-c", filepath.Join(baseDir, "router-B.conf"))
		cmdB.Stdout = os.Stdout
		cmdB.Stderr = os.Stderr
		cmdB.Start()
		procs = append(procs, cmdB.Process)
		fmt.Printf("   → Router B started (PID %d)\n", cmdB.Process.Pid)
	}

	return procs, nil
}

func cleanupRouters(procs []*os.Process) {
	fmt.Println("   → Shutting down Skupper Routers...")
	for _, p := range procs {
		if p != nil {
			p.Kill()
			p.Wait()
		}
	}
}

// ====================== iperf3 Test ======================
func runIperf3Test(config TestConfig, outputDir, dataDir, graphicsDir, commandsDir string) error {
	serverPort := config.Port
	clientPort := config.Port
	clientTarget := "127.0.0.1"

	if config.Routers == 1 {
		serverPort = 5801
		clientPort = 5800
	} else if config.Routers == 2 {
		serverPort = 5801
		clientPort = 5800
	}

	// Save commands
	serverCmdStr := fmt.Sprintf("#!/bin/bash\niperf3 -s -p %d -1\n", serverPort)
	clientCmdStr := fmt.Sprintf("#!/bin/bash\niperf3 -c %s -p %d -t %d -P %d -f m -J%s\n",
		clientTarget, clientPort, config.Duration, config.Parallel,
		map[bool]string{true: " -u", false: ""}[config.Protocol == "udp"])

	_ = os.WriteFile(filepath.Join(commandsDir, "iperf3_server.sh"), []byte(serverCmdStr), 0755)
	_ = os.WriteFile(filepath.Join(commandsDir, "iperf3_client.sh"), []byte(clientCmdStr), 0755)

	// Start server
	serverCmd := exec.Command("iperf3", "-s", "-p", strconv.Itoa(serverPort), "-1")
	serverCmd.Stderr = os.Stderr
	serverCmd.Start()
	time.Sleep(800 * time.Millisecond)

	// Client
	clientArgs := []string{
		"-c", clientTarget,
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
	output, _ := clientCmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(outputDir, "iperf3_client_output.json"), output, 0644)

	serverCmd.Process.Kill()
	serverCmd.Wait()

	processIperf3Output(filepath.Join(outputDir, "iperf3_client_output.json"), dataDir, graphicsDir, config)
	return nil
}

// ====================== Post-processing (with Y-max) ======================
func processIperf3Output(jsonPath, dataDir, graphicsDir string, config TestConfig) error {
	// (same as previous version - data extraction + plotting)
	raw, _ := os.ReadFile(jsonPath)
	content := string(raw)
	start := strings.Index(content, "{")
	if start == -1 {
		return nil
	}

	var result Iperf3Result
	json.Unmarshal([]byte(content[start:]), &result)

	var throughputs []float64
	for _, interval := range result.Intervals {
		if interval.Sum.BitsPerSecond > 0 {
			throughputs = append(throughputs, interval.Sum.BitsPerSecond/1e6)
		}
	}
	if result.End.SumSent.BitsPerSecond > 0 {
		throughputs = append(throughputs, result.End.SumSent.BitsPerSecond/1e6)
	}

	dataPath := filepath.Join(dataDir, "iperf3_client_output.data")
	f, _ := os.Create(dataPath)
	for _, tp := range throughputs {
		fmt.Fprintf(f, "%.2f\n", tp)
	}
	f.Close()

	cleanTitle := strings.ReplaceAll(config.TestName, "_", "\\_")
	relDataPath := filepath.Join("..", "output", "data", "iperf3_client_output.data")

	plotScript := `set terminal pngcairo size 1200,700 enhanced
set output 'throughput.png'
set title '` + cleanTitle + ` (` + strconv.Itoa(config.Parallel) + ` streams, ` + strconv.Itoa(config.Duration) + ` sec) - ` + strconv.Itoa(config.Routers) + ` router(s)'
set xlabel 'Time (seconds)'
set ylabel 'Throughput (Mbps)'
set yrange [0:` + strconv.Itoa(config.YMaxMbps) + `]
set grid
set key outside

plot '` + relDataPath + `' using 0:1 with linespoints lw 2 pt 7 lc rgb "#1f77b4" title 'Throughput'

stats '` + relDataPath + `' nooutput
set label sprintf("Average: %.1f Mbps", STATS_mean) at graph 0.02, 0.95
set label sprintf("Max: %.1f Mbps", STATS_max) at graph 0.02, 0.90
`

	_ = os.WriteFile(filepath.Join(graphicsDir, "throughput_plot.gp"), []byte(plotScript), 0644)

	gnuplotCmd := exec.Command("gnuplot", "throughput_plot.gp")
	gnuplotCmd.Dir = graphicsDir
	gnuplotCmd.Run()

	pngPath := filepath.Join(graphicsDir, "throughput.png")
	if info, _ := os.Stat(pngPath); info != nil && info.Size() > 1000 {
		_ = exec.Command("display", pngPath).Start()
	}

	return nil
}
