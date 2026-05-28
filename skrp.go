package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
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
		"show popup graphs with 'display' after each test (default: off)")

	// Help text when someone runs ./skrp -h or ./skrp --help
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <skupper_version> <config1.json> [config2.json] ...\n\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Flags:")
		flag.PrintDefaults()
	}

	flag.Parse()

	// After flag.Parse(), the remaining non-flag arguments are here
	args := flag.Args()
	fp("args: %v\n", args)
	if len(args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	skupperVersion := args[0]
	configFiles := args[1:]

	fmt.Printf("SKRP starting (skupper version: %s, %d config(s), show-graphs=%v)\n",
		skupperVersion, len(configFiles), showGraphs)

	for i, configPath := range configFiles {
		fmt.Printf("=== Test %d/%d : %s ===\n", i+1, len(configFiles), configPath)
		if err := runTestConfig(skupperVersion, configPath, showGraphs); err != nil {
			fmt.Printf("❌ Failed: %v\n", err)
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
