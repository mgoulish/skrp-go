package main

import (
	"encoding/json"
	"fmt"
	"os"
)

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

