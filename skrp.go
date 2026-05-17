package main

import (
	"fmt"
	"os"
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

