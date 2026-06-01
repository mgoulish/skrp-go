package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ====================== RUN THROUGHPUT TEST ======================
func runThroughputTest(skupperVersion string, config TestConfig, rawData []byte, showGraphs bool) error {
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
	if config.Routers < 0 {
		config.Routers = 0
	}

	dateStr := time.Now().Format("2006_01_02")
	baseDir := filepath.Join("skrp_results", skupperVersion, config.TestType, dateStr, config.TestName)
	outputDir := filepath.Join(baseDir, "output")
	commandsDir := filepath.Join(baseDir, "commands")
	dataDir := filepath.Join(outputDir, "data")
	graphicsDir := filepath.Join(baseDir, "graphics")

	for _, dir := range []string{outputDir, commandsDir, dataDir, graphicsDir} {
		os.MkdirAll(dir, 0755)
	}

	// Write commands FIRST
	writeCommands(config, commandsDir)

	// Save metadata (now using shared RunInfo with Date)
	runInfo := RunInfo{
		SkupperVersion: skupperVersion,
		Date:           dateStr, // <-- NEW
		TestConfig:     config,
		RunTime:        time.Now(),
	}
	infoBytes, _ := json.MarshalIndent(runInfo, "", "  ")
	_ = os.WriteFile(filepath.Join(outputDir, "run_info.json"), infoBytes, 0644)
	_ = os.WriteFile(filepath.Join(outputDir, "config_used.json"), rawData, 0644)

	// Start routers
	var routerProcs []*os.Process
	if config.Routers > 0 {
		fmt.Printf("   → Starting %d router(s)...\n", config.Routers)
		routerProcs, _ = startSkupperRouters(config.Routers, baseDir, commandsDir, outputDir, config.CPU)
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

	if err := runIperf3Test(config, outputDir, dataDir, graphicsDir, commandsDir, showGraphs); err != nil {
		fmt.Printf("   Warning: iperf3 had issues: %v\n", err)
	}

	fmt.Printf("Test completed!\n")
	return nil
}

func runIperf3Test(config TestConfig, outputDir, dataDir, graphicsDir, commandsDir string, showGraphs bool) error {
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
	// Can't use "-f  g" here to print gigabits/sec, because we
	// are using JSON output, in which case iperf3 ignores -f flag.
	clientArgs := []string{
		"-c", "127.0.0.1",
		"-p", strconv.Itoa(clientPort),
		"-t", strconv.Itoa(config.Duration),
		"-P", strconv.Itoa(config.Parallel),
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

	processThroughputOutput(filepath.Join(outputDir, "iperf3_client_output.json"), dataDir, graphicsDir, config, showGraphs)
	return nil
}

// ====================== HTTP LATENCY TEST ======================
func runHttpLatencyTest(skupperVersion string, config TestConfig, rawData []byte, showGraphs bool) error {
	// === Own defaults (duplicated style from runNormalTest) ===
	if config.TestType == "" {
		config.TestType = "latency"
	}
	if config.TestName == "" {
		config.TestName = "latency"
	}
	if config.NumRequests == 0 {
		config.NumRequests = 10000
	}
	if config.Concurrency == 0 {
		config.Concurrency = 50
	}
	if config.Routers < 0 {
		config.Routers = 0
	}

	dateStr := time.Now().Format("2006_01_02")
	baseDir := filepath.Join("skrp_results", skupperVersion, config.TestType, dateStr, config.TestName)
	outputDir := filepath.Join(baseDir, "output")
	commandsDir := filepath.Join(baseDir, "commands")
	dataDir := filepath.Join(outputDir, "data")
	graphicsDir := filepath.Join(baseDir, "graphics")

	for _, dir := range []string{outputDir, commandsDir, dataDir, graphicsDir} {
		os.MkdirAll(dir, 0755)
	}

	// Write commands + metadata
	writeCommands(config, commandsDir)

	// Save metadata (now using shared RunInfo with Date)
	runInfo := RunInfo{
		SkupperVersion: skupperVersion,
		Date:           dateStr, // <-- NEW for comparisons
		TestConfig:     config,
		RunTime:        time.Now(),
	}
	infoBytes, _ := json.MarshalIndent(runInfo, "", "  ")
	_ = os.WriteFile(filepath.Join(outputDir, "run_info.json"), infoBytes, 0644)
	_ = os.WriteFile(filepath.Join(outputDir, "config_used.json"), rawData, 0644)

	// === Start routers if requested ===
	var routerProcs []*os.Process
	if config.Routers > 0 {
		fmt.Printf("   → Starting %d router(s)...\n", config.Routers)
		routerProcs, _ = startSkupperRouters(config.Routers, baseDir, commandsDir, outputDir, config.CPU)
		defer cleanupRouters(routerProcs)

		waitTime := 5 * time.Second
		if config.Routers >= 2 {
			waitTime = 10 * time.Second
		}
		fmt.Printf("   → Waiting %v for routers to sync...\n", waitTime)
		time.Sleep(waitTime)

		waitForRouterReady()
	}

	// === Start minimal HTTP echo server ===
	serverPort := 5801
	if config.Routers == 0 {
		serverPort = 5800
	}
	fmt.Printf("   → Starting minimal HTTP echo server on port %d\n", serverPort)

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("OK"))
	})

	server := &http.Server{Addr: fmt.Sprintf(":%d", serverPort), Handler: mux}
	serverDone := make(chan struct{})

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("HTTP server error: %v\n", err)
		}
		close(serverDone)
	}()
	time.Sleep(800 * time.Millisecond)

	// Target URL
	targetURL := "http://127.0.0.1:5800/ping"
	if config.Routers == 0 {
		targetURL = fmt.Sprintf("http://127.0.0.1:%d/ping", serverPort)
	}

	fmt.Printf("   → Running hey latency test → %s  (concurrency=%d, requests=%d)\n",
		targetURL, config.Concurrency, config.NumRequests)

	// Normal output
	heyArgs := []string{"-n", strconv.Itoa(config.NumRequests), "-c", strconv.Itoa(config.Concurrency), targetURL}
	cmd := exec.Command("hey", heyArgs...)
	output, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(outputDir, "hey_output.txt"), output, 0644)

	if err != nil {
		fmt.Printf("   Warning: hey had issues: %v\n", err)
	} else {
		fmt.Println("   → hey latency test completed successfully")
	}

	// CSV for graphing
	fmt.Printf("   → Running hey again for CSV data \n")
	heyArgsCSV := []string{"-n", strconv.Itoa(config.NumRequests), "-c", strconv.Itoa(config.Concurrency), "-o", "csv", targetURL}
	cmdCSV := exec.Command("hey", heyArgsCSV...)
	csvOutput, _ := cmdCSV.CombinedOutput()
	_ = os.WriteFile(filepath.Join(outputDir, "hey_output.csv"), csvOutput, 0644)

	fmt.Println("   → CSV data for graphing saved")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if shutdownErr := server.Shutdown(ctx); shutdownErr != nil {
		fmt.Printf("   Warning: HTTP server shutdown: %v\n", shutdownErr)
	}
	<-serverDone

	fmt.Printf("HTTP latency test completed!\n")

	// Generate the time-sequence latency graph
	if err := processHttpLatencyOutput(filepath.Join(outputDir, "hey_output.csv"), dataDir, graphicsDir, config, showGraphs); err != nil {
		fmt.Printf("   Warning: Could not create latency time-sequence graph: %v\n", err)
	}
	return nil
}

// ====================== CONNECTION RATE TEST ======================
func runConnectionRateTest(skupperVersion string, config TestConfig, rawData []byte, showGraphs bool) error {
	if config.TestName == "" {
		config.TestName = "connection-rate"
	}
	if config.Duration == 0 {
		config.Duration = 30 // seconds
	}
	if config.Concurrency == 0 {
		config.Concurrency = 200 // number of concurrent clients
	}
	if config.Routers < 0 {
		config.Routers = 0
	}

	dateStr := time.Now().Format("2006_01_02")
	baseDir := filepath.Join("skrp_results", skupperVersion, "connection-rate", dateStr, config.TestName)
	outputDir := filepath.Join(baseDir, "output")
	commandsDir := filepath.Join(baseDir, "commands")
	dataDir := filepath.Join(outputDir, "data")
	graphicsDir := filepath.Join(baseDir, "graphics")

	for _, dir := range []string{outputDir, commandsDir, dataDir, graphicsDir} {
		os.MkdirAll(dir, 0755)
	}

	writeCommands(config, commandsDir)

	type RunInfo struct {
		SkupperVersion string     `json:"skupper_version"`
		TestConfig     TestConfig `json:"test_config"`
		RunTime        time.Time  `json:"run_time"`
	}
	runInfo := RunInfo{SkupperVersion: skupperVersion, TestConfig: config, RunTime: time.Now()}
	infoBytes, _ := json.MarshalIndent(runInfo, "", "  ")
	_ = os.WriteFile(filepath.Join(outputDir, "run_info.json"), infoBytes, 0644)
	_ = os.WriteFile(filepath.Join(outputDir, "config_used.json"), rawData, 0644)

	var routerProcs []*os.Process
	if config.Routers > 0 {
		fmt.Printf("   → Starting %d router(s)...\n", config.Routers)
		routerProcs, _ = startSkupperRouters(config.Routers, baseDir, commandsDir, outputDir, config.CPU)
		defer cleanupRouters(routerProcs)

		waitTime := 5 * time.Second
		if config.Routers >= 2 {
			waitTime = 10 * time.Second
		}
		fmt.Printf("   → Waiting %v for routers to sync...\n", waitTime)
		time.Sleep(waitTime)

		waitForRouterReady()
	}

	// Start minimal HTTP echo server
	serverPort := 5801
	if config.Routers == 0 {
		serverPort = 5800
	}
	fmt.Printf("   → Starting minimal HTTP echo server on port %d\n", serverPort)

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("OK"))
	})

	srv := &http.Server{Addr: fmt.Sprintf(":%d", serverPort), Handler: mux}
	serverDone := make(chan struct{})

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("HTTP server error: %v\n", err)
		}
		close(serverDone)
	}()
	time.Sleep(800 * time.Millisecond)

	// Target URL (through router or direct)
	targetURL := "http://127.0.0.1:5800/ping"
	if config.Routers == 0 {
		targetURL = fmt.Sprintf("http://127.0.0.1:%d/ping", serverPort)
	}

	fmt.Printf("   → Running connection-rate test → %s  (concurrency=%d clients, duration=%ds)\n",
		targetURL, config.Concurrency, config.Duration)

	// Build hey command for connection rate
	// -disable-keepalive is critical: every request = new TCP connection

	fmt.Printf("   → Running connection-rate test → %s  (concurrency=%d clients, duration=%ds)\n",
		targetURL, config.Concurrency, config.Duration)

	// Normal text output (for summary + parseRequestsPerSec)
	heyArgs := []string{
		"-disable-keepalive",
		"-z", fmt.Sprintf("%ds", config.Duration),
		"-c", strconv.Itoa(config.Concurrency),
		"-m", "GET",
		targetURL,
	}
	cmd := exec.Command("hey", heyArgs...)
	output, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(outputDir, "hey_output.txt"), output, 0644)

	rate := parseRequestsPerSec(output)
	fmt.Printf("\n=== Connection Rate ===\n")
	fmt.Printf("Requests/sec (connection rate): %.2f\n\n", rate)

	if err != nil {
		fmt.Printf("   Warning: hey had issues: %v\n", err)
	} else {
		fmt.Println("   → Connection-rate test completed successfully")
	}

	// Extra CSV run for moving-average connection-rate graph (flags BEFORE URL!)
	heyArgsCSV := []string{
		"-disable-keepalive",
		"-z", fmt.Sprintf("%ds", config.Duration),
		"-c", strconv.Itoa(config.Concurrency),
		"-m", "GET",
		"-o", "csv",
		targetURL,
	}
	cmdCSV := exec.Command("hey", heyArgsCSV...)
	csvOutput, _ := cmdCSV.CombinedOutput()
	_ = os.WriteFile(filepath.Join(outputDir, "hey_output.csv"), csvOutput, 0644)

	fmt.Println("   → CSV data for graphing saved")

	// Generate the moving-average connection rate graph
	if err := processConnectionRateOutput(filepath.Join(outputDir, "hey_output.csv"), dataDir, graphicsDir, config, showGraphs); err != nil {
		fmt.Printf("   Warning: Could not create connection-rate moving-average graph: %v\n", err)
	}

	// Graceful shutdown of the echo server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if shutdownErr := srv.Shutdown(ctx); shutdownErr != nil {
		fmt.Printf("   Warning: HTTP server shutdown: %v\n", shutdownErr)
	}
	<-serverDone

	fmt.Printf("Connection-rate test completed!\n")
	return nil
}

// parseRequestsPerSec extracts the "Requests/sec" value from hey's text summary output.
func parseRequestsPerSec(output []byte) float64 {
	re := regexp.MustCompile(`Requests/sec:\s*([0-9.]+)`)
	matches := re.FindSubmatch(output)
	if len(matches) > 1 {
		f, _ := strconv.ParseFloat(string(matches[1]), 64)
		return f
	}
	return 0
}

func WhoCalledMe() {
	// skip = 0: returns information about WhoCalledMe itself
	// skip = 1: returns information about the function that called WhoCalledMe
	// skip = 2: returns the caller of the caller, etc.
	pc, file, line, ok := runtime.Caller(2)

	if !ok {
		fmt.Println("Could not recover caller information")
		return
	}

	// Use the Program Counter (pc) to get the function object
	callerFunction := runtime.FuncForPC(pc)

	fmt.Printf("Called by: %s\n", callerFunction.Name())
	fmt.Printf("Found in:  %s (Line: %d)\n\n", file, line)
}

// ====================== COMPARISON SPECIFIER SUPPORT ======================

// runComparisonSpecifier is the new entry point for your comparison JSON files
func runComparisonSpecifier(configPath string, rawData []byte, showGraphs bool) error {
	var spec ComparisonSpecifier
	if err := json.Unmarshal(rawData, &spec); err != nil {
		return fmt.Errorf("invalid comparison specifier: %w", err)
	}

	// Parse all selector fields
	var raw map[string]interface{}
	json.Unmarshal(rawData, &raw)
	spec.Selectors = make(map[string]interface{})
	for k, v := range raw {
		if k != "comparison_type" && k != "y_label" {
			spec.Selectors[k] = v
		}
	}

	// === NEW: Resolve "latest" values BEFORE searching ===
	testType := spec.ComparisonType

	if ver, ok := spec.Selectors["skupper_version"]; ok {
		if s, ok := ver.(string); ok && s == "latest" {
			resolved := getLatestSkupperVersion()
			spec.Selectors["skupper_version"] = resolved
			fmt.Printf("   → Resolved skupper_version 'latest' → %s\n", resolved)
		}
	}

	if d, ok := spec.Selectors["date"]; ok {
		if s, ok := d.(string); ok && s == "latest" {
			ver := ""
			if v, ok := spec.Selectors["skupper_version"]; ok {
				ver = fmt.Sprintf("%v", v)
			}
			resolved := getLatestDate(ver, testType)
			spec.Selectors["date"] = resolved
			fmt.Printf("   → Resolved date 'latest' → %s\n", resolved)
		}
	}

	// Exactly one field must be a list (the varying axis)
	varyingField, varyingValues, err := identifyVaryingField(spec.Selectors)
	if err != nil {
		return err
	}

	fmt.Printf("   → Comparison: %s | varying on %s = %v\n", spec.ComparisonType, varyingField, varyingValues)
	fmt.Printf("   → Using skupper_version=%v, date=%v\n", spec.Selectors["skupper_version"], spec.Selectors["date"])

	matchingRuns, err := findMatchingTestRuns(spec)
	if err != nil {
		return err
	}
	if len(matchingRuns) == 0 {
		return fmt.Errorf("no matching tests found for this comparison")
	}

	// For now we only support throughput comparisons
	if spec.ComparisonType == "throughput" {
		return generateThroughputComparisonGraph(configPath, spec, varyingField, matchingRuns, showGraphs)
	}

	return fmt.Errorf("comparison_type '%s' not supported yet", spec.ComparisonType)
}

// identifyVaryingField returns the key that has a []value and the list itself
func identifyVaryingField(selectors map[string]interface{}) (string, []interface{}, error) {
	var varying string
	var values []interface{}
	count := 0
	for k, v := range selectors {
		if arr, ok := v.([]interface{}); ok {
			count++
			varying = k
			values = arr
		}
	}
	if count != 1 {
		return "", nil, fmt.Errorf("exactly one selector field must be a list; found %d", count)
	}
	return varying, values, nil
}

// findMatchingTestRuns walks skrp_results and returns the base dirs of matching tests
func findMatchingTestRuns(spec ComparisonSpecifier) ([]string, error) {
	var matches []string
	err := filepath.Walk("skrp_results", func(path string, info os.FileInfo, err error) error {
		if err != nil || !strings.HasSuffix(path, "/output/run_info.json") {
			return nil
		}
		data, _ := os.ReadFile(path)
		var ri RunInfo
		if json.Unmarshal(data, &ri) != nil {
			return nil
		}

		if matchesSpec(ri, spec) {
			baseDir := filepath.Dir(filepath.Dir(path))
			matches = append(matches, baseDir)
		}
		return nil
	})
	return matches, err
}

func matchesSpec(ri RunInfo, spec ComparisonSpecifier) bool {
	for field, wantRaw := range spec.Selectors {
		// skip the varying field (the one that is a list)
		if _, isSlice := wantRaw.([]interface{}); isSlice {
			continue
		}

		// treat "latest" as wildcard
		if s, ok := wantRaw.(string); ok && s == "latest" {
			continue
		}

		var got interface{}
		switch field {
		case "skupper_version":
			got = ri.SkupperVersion
		case "date":
			got = ri.Date
		case "cpu":
			got = ri.TestConfig.CPU
		case "routers":
			got = ri.TestConfig.Routers
		default:
			continue
		}

		if !valuesEqual(got, wantRaw) {
			return false
		}
	}
	return true
}

// Helper: handles int/float64 comparison (the most common mismatch)
func valuesEqual(a, b interface{}) bool {
	if a == b {
		return true
	}
	// int vs float64
	if ia, ok := a.(int); ok {
		if fb, ok := b.(float64); ok {
			return float64(ia) == fb
		}
	}
	if fa, ok := a.(float64); ok {
		if ib, ok := b.(int); ok {
			return fa == float64(ib)
		}
	}
	return false
}

// generateThroughputComparisonGraph creates the multi-plot graph
func generateThroughputComparisonGraph(specPath string, spec ComparisonSpecifier, varyingField string, runs []string, showGraphs bool) error {
	compName := strings.TrimSuffix(filepath.Base(specPath), ".json")
	compDir := filepath.Join("skrp_results", "comparisons", compName)
	graphicsDir := filepath.Join(compDir, "graphics")
	dataDir := filepath.Join(compDir, "data")
	os.MkdirAll(graphicsDir, 0755)
	os.MkdirAll(dataDir, 0755)

	var plotLines []string
	for i, runDir := range runs {
		srcData := filepath.Join(runDir, "output", "data", "iperf3_client_output.data")
		if _, err := os.Stat(srcData); os.IsNotExist(err) {
			continue
		}
		dest := filepath.Join(dataDir, fmt.Sprintf("series_%d.data", i))
		_ = copyFile(srcData, dest)

		label := fmt.Sprintf("%s %v", varyingField, getVaryingValueFromRun(runDir, varyingField))
		plotLines = append(plotLines, fmt.Sprintf("'%s' using 0:1 with linespoints lw 2 pt %d title '%s'",
			filepath.Join("..", "data", filepath.Base(dest)), i+1, label))
	}

	title := fmt.Sprintf("%s Comparison Across %s Values", strings.Title(spec.ComparisonType), strings.Title(varyingField))

	plotScript := fmt.Sprintf(`set terminal pngcairo size 1400,800 enhanced
set output 'comparison.png'
set title '%s'
set xlabel 'Time (seconds)'
set ylabel '%s'
set yrange [0:]
set grid
set key outside

plot %s

`, title, spec.YLabel, strings.Join(plotLines, ", "))

	_ = os.WriteFile(filepath.Join(graphicsDir, "comparison_plot.gp"), []byte(plotScript), 0644)

	cmd := exec.Command("gnuplot", "comparison_plot.gp")
	cmd.Dir = graphicsDir
	output, _ := cmd.CombinedOutput()
	fmt.Printf("Gnuplot output:\n%s\n", string(output))

	png := filepath.Join(graphicsDir, "comparison.png")
	fmt.Printf("   → Comparison graph written to %s\n", png)
	if showGraphs {
		exec.Command("display", png).Start()
	}
	return nil
}

func copyFile(src, dst string) error {
	data, _ := os.ReadFile(src)
	return os.WriteFile(dst, data, 0644)
}

func getVaryingValueFromRun(runDir string, field string) interface{} {
	data, _ := os.ReadFile(filepath.Join(runDir, "output", "run_info.json"))
	var ri RunInfo
	json.Unmarshal(data, &ri)
	switch field {
	case "cpu":
		return ri.TestConfig.CPU
	case "routers":
		return ri.TestConfig.Routers
	default:
		return "?"
	}
}

// getLatestSkupperVersion returns the newest skupper version directory (lexicographically newest)
func getLatestSkupperVersion() string {
	entries, _ := os.ReadDir("skrp_results")
	var latest string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "comparisons" {
			if latest == "" || e.Name() > latest {
				latest = e.Name()
			}
		}
	}
	return latest
}

// getLatestDate returns the newest date directory for a given skupper version + test type
func getLatestDate(version, testType string) string {
	if version == "" {
		return ""
	}
	path := filepath.Join("skrp_results", version, testType)
	entries, _ := os.ReadDir(path)
	var latest string
	for _, e := range entries {
		if e.IsDir() {
			if latest == "" || e.Name() > latest {
				latest = e.Name()
			}
		}
	}
	return latest
}
