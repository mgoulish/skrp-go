package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

var fp = fmt.Printf

var showGraphs bool

func main() {
	var showGraphs bool

	requiredExecutables := []string{"skrouterd", "iperf3", "gnuplot", "hey"}
	for _, executable := range requiredExecutables {
		if !commandExists(executable) {
			fp("skrp: %s executable missing\n", executable)
			os.Exit(1)
		}
	}

	flag.BoolVar(&showGraphs, "show-graphs", false,
		"show popup graphs with 'display' after each test/comparison (default: off)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] [skupper_version] <config1.json> [config2.json ...]\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "   If a .json contains \"comparison_type\" it is treated as a comparison specifier.")
		flag.PrintDefaults()
	}

	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	var skupperVersion string
	configFiles := args

	// If first arg is NOT a .json → treat it as skupper_version (for normal tests)
	if !strings.HasSuffix(args[0], ".json") {
		skupperVersion = args[0]
		configFiles = args[1:]
		if len(configFiles) == 0 {
			flag.Usage()
			os.Exit(1)
		}
	}

	fmt.Printf("SKRP starting (skupper version: %s, %d file(s), show-graphs=%v)\n",
		skupperVersion, len(configFiles), showGraphs)

	for i, configPath := range configFiles {
		fmt.Printf("=== Processing %d/%d : %s ===\n", i+1, len(configFiles), configPath)

		data, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Printf("Failed to read %s: %v\n", configPath, err)
			continue
		}

		if IsComparisonFile(data) {
			// NEW: comparison path
			if err := runComparisonSpecifier(configPath, data, showGraphs); err != nil {
				fmt.Printf("Comparison failed: %v\n", err)
			}
		} else {
			// regular test
			if skupperVersion == "" {
				fmt.Println("Error: normal tests require a skupper_version argument")
				continue
			}
			if err := runTestConfig(skupperVersion, configPath, showGraphs); err != nil {
				fmt.Printf("Test failed: %v\n", err)
			}
		}

		if i < len(configFiles)-1 {
			time.Sleep(4 * time.Second)
		}
	}
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
