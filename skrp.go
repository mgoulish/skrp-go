package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var fp = fmt.Printf

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
