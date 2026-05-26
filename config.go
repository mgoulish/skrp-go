package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

func runTestConfig(skupperVersion, configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var config TestConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}
	fp("Config: %+v\n", config)

	if !IsValidTestType(config.TestType) {
		fp("Bad test type: %s\n", config.TestType)
		fp("Valid test types are: %+v\n", ValidTestTypes)
		return errors.New("Bad test type")
	}

	if config.TestType == "throughput" {
		// This field should have a better name
		// how about TestSubType ?
		if config.Type == "comparison" {
			return runComparison(skupperVersion, config)
		} else {
			return runThroughputTest(skupperVersion, config, data)
		}
	} else if config.TestType == "latency" {
		return runHttpLatencyTest(skupperVersion, config, data)
	} else if config.TestType == "connection-rate" {
		return runConnectionRateTest(skupperVersion, config, data)
	} else {
		fp("runTestConfig: this can't happen.\n")
	}

	return nil
}
