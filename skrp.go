package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var fp = fmt.Printf

type TestConfig struct {
	Type           string   `json:"type"`           // "test" or "comparison"
	TestType       string   `json:"test_type"`
	TestName       string   `json:"test_name"`
	Duration       int      `json:"duration_seconds"`
	Parallel       int      `json:"parallel_streams"`
	Protocol       string   `json:"protocol"`
	Port           int      `json:"port"`
	Routers        int      `json:"routers"`
	YMaxMbps       int      `json:"y_max_mbps"`

	// Comparison only
	ComparisonName string   `json:"name"`
	Title          string   `json:"title"`
	Tests          []string `json:"tests"`
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
		if err := runConfig(skupperVersion, configPath); err != nil {
			fmt.Printf("❌ Failed: %v\n", err)
		}
		if i < len(configFiles)-1 {
			time.Sleep(4 * time.Second)
		}
	}
}

func runConfig(skupperVersion, configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var config TestConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	if config.Type == "comparison" {
		return runComparison(skupperVersion, config)
	}

	return runNormalTest(skupperVersion, config, data)
}

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
		routerProcs, _ = startSkupperRouters(config.Routers, baseDir, commandsDir)
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

// ====================== Write Commands Early ======================
func writeCommands(config TestConfig, commandsDir string) {
	serverPort := config.Port
	clientPort := config.Port
	if config.Routers >= 1 {
		serverPort = 5801
		clientPort = 5800
	}

	serverCmd := fmt.Sprintf("#!/bin/bash\niperf3 -s -p %d -1\n", serverPort)
	clientCmd := fmt.Sprintf("#!/bin/bash\niperf3 -c 127.0.0.1 -p %d -t %d -P %d -f m -J\n",
		clientPort, config.Duration, config.Parallel)

	_ = os.WriteFile(filepath.Join(commandsDir, "iperf3_server.sh"), []byte(serverCmd), 0755)
	_ = os.WriteFile(filepath.Join(commandsDir, "iperf3_client.sh"), []byte(clientCmd), 0755)

	fmt.Println("   → Commands written to commands/ directory")
}

// ====================== COMPARISON ======================
func runComparison(skupperVersion string, config TestConfig) error {
	if len(config.Tests) == 0 {
		return fmt.Errorf("comparison needs 'tests' array")
	}
	if config.TestType == "" {
		config.TestType = "throughput"
	}

	fmt.Printf("   → Generating comparison: %s\n", config.ComparisonName)

	dateStr := time.Now().Format("2006_01_02")
	compDir := filepath.Join("skrp_results", skupperVersion, "comparison", dateStr, config.ComparisonName)
	graphicsDir := filepath.Join(compDir, "graphics")
	os.MkdirAll(graphicsDir, 0755)

	var plotLines []string
	colors := []string{"#1f77b4", "#ff7f0e", "#2ca02c", "#d62728", "#9467bd"}

	for i, name := range config.Tests {
		fullPath := findLatestTestPath(skupperVersion, config.TestType, dateStr, name)
		dataFile := filepath.Join(fullPath, "output/data/iperf3_client_output.data")
		fp("MDEBUG: runComparison: fullPath: |%s|\n", fullPath)
		absData, _ := filepath.Abs(dataFile)

		fp("MDEBUG: runComparison: absData: |%s|\n", absData)
		if _, err := os.Stat(absData); err != nil {
			fmt.Printf("   Warning: Could not find data for '%s'\n", name)
			continue
		}

		label := strings.TrimSuffix(name, "_routers") + " routers"
		color := colors[i%len(colors)]

		plotLines = append(plotLines, fmt.Sprintf(`'%s' using 0:1 with linespoints lw 2.5 pt 7 lc rgb "%s" title "%s"`,
			absData, color, label))
	}

	if len(plotLines) == 0 {
		return fmt.Errorf("no valid data files found")
	}

	plotScript := `set terminal pngcairo size 1400,800 enhanced
set output 'comparison.png'
set title '` + config.Title + `'
set xlabel 'Time (seconds)'
set ylabel 'Throughput (Mbps)'
set yrange [0:` + strconv.Itoa(config.YMaxMbps) + `]
set grid
set key outside

plot ` + strings.Join(plotLines, ", ") + `

print "✅ Comparison plot generated"
`

	gpPath := filepath.Join(graphicsDir, "comparison_plot.gp")
	_ = os.WriteFile(gpPath, []byte(plotScript), 0644)

	fmt.Println("   → Running gnuplot...")
	gnuplotCmd := exec.Command("gnuplot", "comparison_plot.gp")
	gnuplotCmd.Dir = graphicsDir
	gnuplotCmd.Run()

	pngPath := filepath.Join(graphicsDir, "comparison.png")
	if info, _ := os.Stat(pngPath); info != nil && info.Size() > 1000 {
		fmt.Printf("   → Comparison graph created (%d KB)\n", info.Size()/1024)
		_ = exec.Command("display", pngPath).Start()
	} else {
		fmt.Println("   Warning: comparison.png is empty")
	}

	return nil
}

func findLatestTestPath(version, testType, dateStr, name string) string {
	base := filepath.Join("skrp_results", version, testType, dateStr)
	entries, _ := os.ReadDir(base)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() > entries[j].Name()
	})

	for _, e := range entries {
		fp("MDEBUG: findLatestTestPath: looking for: |%s| in |%s|\n", e.Name(), name)
		//if strings.Contains(strings.ToLower(e.Name()), strings.ToLower(name)) {
                if strings.Contains(e.Name(), name) || strings.Contains(e.Name(), strings.TrimSuffix(name, "_routers")) {

			candidate := filepath.Join(base, e.Name(), name)
			if _, err := os.Stat(filepath.Join(candidate, "output/data/iperf3_client_output.data")); err == nil {
				return candidate
			}
			// Try without trailing _routers
			if _, err := os.Stat(filepath.Join(base, e.Name(), "output/data/iperf3_client_output.data")); err == nil {
				return filepath.Join(base, e.Name())
			}
		}
	}
	return name
}

// ====================== Router Helpers ======================
func waitForRouterReady() {
	fmt.Println("   → Waiting for router listener on port 5800...")
	for i := 0; i < 35; i++ {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:5800", 800*time.Millisecond)
		if err == nil {
			conn.Close()
			fmt.Println("   → Router listener is READY!")
			return
		}
		time.Sleep(700 * time.Millisecond)
	}
	fmt.Println("   Warning: Router listener not responding after ~24s")
}

func startSkupperRouters(numRouters int, baseDir, commandsDir string) ([]*os.Process, error) {
	var procs []*os.Process

	if numRouters == 1 {
		routerConfig := `router {
    mode: interior
    id: skrp-router-A
    workerThreads: 4
}
tcpListener {
    host: 0.0.0.0
    port: 5800
    address: router-test
    siteId: skrp-test
}
tcpConnector {
    host: 127.0.0.1
    port: 5801
    address: router-test
    siteId: skrp-test
}`
		writeRouterFiles(baseDir, commandsDir, "router.conf", routerConfig)

		cmd := exec.Command("skrouterd", "-c", filepath.Join(baseDir, "router.conf"))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Start()
		procs = append(procs, cmd.Process)
		fmt.Printf("   → Single Router started (PID %d)\n", cmd.Process.Pid)

	} else if numRouters == 2 {
		routerA := `router {
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
    siteId: skrp-multi-test
}`
		writeRouterFiles(baseDir, commandsDir, "router-A.conf", routerA)

		cmdA := exec.Command("skrouterd", "-c", filepath.Join(baseDir, "router-A.conf"))
		cmdA.Stdout = os.Stdout
		cmdA.Stderr = os.Stderr
		cmdA.Start()
		procs = append(procs, cmdA.Process)
		fmt.Printf("   → Router A started (PID %d)\n", cmdA.Process.Pid)

		routerB := `router {
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
    siteId: skrp-multi-test
}`
		writeRouterFiles(baseDir, commandsDir, "router-B.conf", routerB)

		cmdB := exec.Command("skrouterd", "-c", filepath.Join(baseDir, "router-B.conf"))
		cmdB.Stdout = os.Stdout
		cmdB.Stderr = os.Stderr
		cmdB.Start()
		procs = append(procs, cmdB.Process)
		fmt.Printf("   → Router B started (PID %d)\n", cmdB.Process.Pid)
	}

	return procs, nil
}

func writeRouterFiles(baseDir, commandsDir, filename, content string) {
	_ = os.WriteFile(filepath.Join(baseDir, filename), []byte(content), 0644)
	_ = os.WriteFile(filepath.Join(commandsDir, filename), []byte(content), 0644)
}

func cleanupRouters(procs []*os.Process) {
	fmt.Println("   → Shutting down routers...")
	for _, p := range procs {
		if p != nil {
			p.Kill()
			p.Wait()
		}
	}
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

func processIperf3Output(jsonPath, dataDir, graphicsDir string, config TestConfig) error {
	raw, _ := os.ReadFile(jsonPath)
	content := string(raw)
	start := strings.Index(content, "{")
	if start == -1 {
		fmt.Println("   Warning: No JSON from iperf3")
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
		fmt.Println("   → Graph displayed")
	}

	return nil
}
