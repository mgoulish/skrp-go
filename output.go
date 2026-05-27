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

func runComparison(skupperVersion string, config TestConfig) error {
	if config.TestType == "http-latency" {
		return runLatencyComparison(skupperVersion, config)
	}

	if len(config.Tests) == 0 {
		return fmt.Errorf("comparison needs 'tests' array with full paths to iperf3_client_output.data files")
	}

	fmt.Printf("   → Generating comparison: %s\n", config.ComparisonName)

	dateStr := time.Now().Format("2006_01_02")
	compDir := filepath.Join("skrp_results", skupperVersion, "comparison", dateStr, config.ComparisonName)
	graphicsDir := filepath.Join(compDir, "graphics")
	os.MkdirAll(graphicsDir, 0755)

	var plotLines []string
	colors := []string{"#1f77b4", "#ff7f0e", "#2ca02c", "#d62728", "#9467bd"}

	for i, dataFile := range config.Tests {
		absData, _ := filepath.Abs(dataFile)
		if _, err := os.Stat(absData); err != nil {
			fmt.Printf("   Warning: Could not find data file '%s'\n", dataFile)
			continue
		}

		// Derive a nice label from the directory structure
		// (assumes: .../<testname>/output/data/iperf3_client_output.data)
		testNameDir := filepath.Dir(filepath.Dir(filepath.Dir(absData)))
		name := filepath.Base(testNameDir)
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

print "Comparison plot generated"
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


func processOutput(jsonPath, dataDir, graphicsDir string, config TestConfig) error {
	fp("processing output...\n")
	WhoCalledMe()
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

	// Build yrange directive conditionally
	yrangeLine := "set yrange [0:]"
	if config.YMaxMbps != 0 {
		yrangeLine = "set yrange [0:" + strconv.Itoa(config.YMaxMbps) + "]"
	}

	plotScript := `set terminal pngcairo size 1200,700 enhanced
set output 'throughput.png'
set title '` + cleanTitle + ` (` + strconv.Itoa(config.Parallel) + ` streams, ` + strconv.Itoa(config.Duration) + ` sec) - ` + strconv.Itoa(config.Routers) + ` router(s)'
set xlabel 'Time (seconds)'
set ylabel '` + config.YLabel + `'
` + yrangeLine + `
set grid
set key outside

plot '` + relDataPath + `' using 0:1 with linespoints lw 2 pt 7 lc rgb "#1f77b4" title '` + config.PlotTitle + `'

stats '` + relDataPath + `' nooutput
set label sprintf("Average: %.1f Mbps", STATS_mean) at graph 0.02, 0.95
set label sprintf("Max: %.1f Mbps", STATS_max) at graph 0.02, 0.90
`

	_ = os.WriteFile(filepath.Join(graphicsDir, "throughput_plot.gp"), []byte(plotScript), 0644)

	gnuplotCmd := exec.Command("gnuplot", "throughput_plot.gp")
	gnuplotCmd.Dir = graphicsDir
	//gnuplotCmd.Run()
	output, err := gnuplotCmd.CombinedOutput()

	if err != nil {
		// Output is still populated even if the command returns an error exit code
		fmt.Printf("Command finished with error: %v\n", err)
	}

	// Convert the byte slice to a string and print it
	fmt.Printf("Gnuplot Output:\n%s\n", string(output))

	pngPath := filepath.Join(graphicsDir, "throughput.png")
	if info, _ := os.Stat(pngPath); info != nil && info.Size() > 1000 {
		_ = exec.Command("display", pngPath).Start()
		fmt.Println("   → Graph displayed")
	} else {
		fp("Gnuplot did not produce a graphic.\n")
	}

	return nil
}

// generateHeyCDFData parses the "Latency distribution:" section from hey_output.txt
// and writes a CDF file: latency_ms   cumulative_percentage
func generateHeyCDFData(txtPath, cdfDataPath string) error {
	data, err := os.ReadFile(txtPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	var latencies []struct{ percent, seconds float64 }

	inDistribution := false
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "Latency distribution:" {
			inDistribution = true
			continue
		}
		// No longer rely on a blank line (your files don't have one)
		if inDistribution && strings.Contains(line, " in ") {
			// Handle "10%% in 0.0002 secs" (or "10% in ...")
			parts := strings.Split(line, " in ")
			if len(parts) != 2 {
				continue
			}

			// Parse percent (strip trailing % or %%)
			percentStr := strings.TrimSpace(parts[0])
			percentStr = strings.TrimSuffix(percentStr, "%%")
			percentStr = strings.TrimSuffix(percentStr, "%")
			percent, err1 := strconv.ParseFloat(percentStr, 64)

			// Parse seconds
			secsStr := strings.TrimSpace(parts[1])
			secsStr = strings.TrimSuffix(secsStr, " secs")
			secsStr = strings.TrimSuffix(secsStr, " sec")
			secs, err2 := strconv.ParseFloat(secsStr, 64)

			if err1 == nil && err2 == nil && percent > 0 && percent <= 100 {
				latencies = append(latencies, struct{ percent, seconds float64 }{percent, secs})
			}
		}
	}

	if len(latencies) == 0 {
		return fmt.Errorf("no latency distribution data found in %s", txtPath)
	}

	// Sort by percentile just in case
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i].percent < latencies[j].percent
	})

	f, err := os.Create(cdfDataPath)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, p := range latencies {
		latencyMs := p.seconds * 1000
		fmt.Fprintf(f, "%.3f %.3f\n", latencyMs, p.percent)
	}

	return nil
}

// ====================== LATENCY COMPARISON ======================
func runLatencyComparison(skupperVersion string, config TestConfig) error {
	if len(config.Tests) == 0 {
		return fmt.Errorf("comparison needs 'tests' array with full paths to hey_output.txt files")
	}

	fmt.Printf("   → Generating latency CDF comparison: %s\n", config.ComparisonName)

	dateStr := time.Now().Format("2006_01_02")
	compDir := filepath.Join("skrp_results", skupperVersion, "comparison", dateStr, config.ComparisonName)
	graphicsDir := filepath.Join(compDir, "graphics")
	dataDir := filepath.Join(compDir, "data")
	os.MkdirAll(graphicsDir, 0755)
	os.MkdirAll(dataDir, 0755)

	var plotLines []string
	colors := []string{"#1f77b4", "#ff7f0e", "#2ca02c", "#d62728", "#9467bd"}

	for i, txtFile := range config.Tests {
		if _, err := os.Stat(txtFile); err != nil {
			fmt.Printf("   Warning: Could not find %s\n", txtFile)
			continue
		}

		cdfData := filepath.Join(dataDir, fmt.Sprintf("test%d.cdf.data", i))
		if err := generateHeyCDFData(txtFile, cdfData); err != nil {
			fmt.Printf("   Warning: Failed to generate CDF for %s: %v\n", txtFile, err)
			continue
		}

		label := filepath.Base(filepath.Dir(filepath.Dir(txtFile)))
		color := colors[i%len(colors)]
		absData, _ := filepath.Abs(cdfData)

		plotLines = append(plotLines, fmt.Sprintf(`'%s' using 1:2 with lines lw 2.5 lc rgb "%s" title "%s"`,
			absData, color, label))
	}

	if len(plotLines) == 0 {
		return fmt.Errorf("no valid latency data files found")
	}

	plotScript := `set terminal pngcairo size 1200,500 enhanced
set output 'latency_cdf_comparison.png'
set title '` + config.Title + `'
set xlabel 'Latency (milliseconds)'
set ylabel 'Percentage of requests (%)'
set logscale x
set xrange [0.1:200]
set xtics (0.1, 0.2, 0.5, 1, 2, 5, 10, 20, 50, 100, 200)
set yrange [0:100]
set grid
set key outside

plot ` + strings.Join(plotLines, ", ") + `

print "Latency CDF comparison generated"
`

	gpPath := filepath.Join(graphicsDir, "latency_cdf_comparison.gp")
	_ = os.WriteFile(gpPath, []byte(plotScript), 0644)

	fmt.Println("   → Running gnuplot...")
	gnuplotCmd := exec.Command("gnuplot", "latency_cdf_comparison.gp")
	gnuplotCmd.Dir = graphicsDir
	gnuplotCmd.Run()

	pngPath := filepath.Join(graphicsDir, "latency_cdf_comparison.png")
	if info, _ := os.Stat(pngPath); info != nil && info.Size() > 1000 {
		fmt.Printf("   → Latency CDF comparison graph created (%d KB)\n", info.Size()/1024)
		_ = exec.Command("display", pngPath).Start()
	} else {
		fmt.Println("   Warning: latency_cdf_comparison.png is empty")
	}

	return nil
}
