package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
        "time"
)

// The test configs come from the json files whose paths you put on the command line.
func runTestConfig(skupperVersion, configPath string, showGraphs bool) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var config TestConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	//fp("Config: %+v\n", config)

	if !IsValidTestType(config.TestType) {
		fp("Bad test type: %s\n", config.TestType)
		fp("Valid test types are: %+v\n", ValidTestTypes)
		return errors.New("bad test type")
	}

	cpus := config.getCPUs()
	for i, cpu := range cpus {
		testConfig := config
		testConfig.CPU = cpu
		if len(cpus) > 1 {
			testConfig.TestName = fmt.Sprintf("%s_cpu_%d", config.TestName, cpu)
		}

		fmt.Printf("   → Running with CPU quota = %d%% (%d/%d)\n", cpu, i+1, len(cpus))

		var err error
		switch testConfig.TestType {
		case "throughput":
			err = runThroughputTest(skupperVersion, testConfig, data, showGraphs)
		case "latency":
			err = runHttpLatencyTest(skupperVersion, testConfig, data, showGraphs)
		case "connection-rate":
			err = runConnectionRateTest(skupperVersion, testConfig, data, showGraphs)
		default:
			return fmt.Errorf("unknown test type: %s", testConfig.TestType)
		}

		if err != nil {
			return fmt.Errorf("test failed with CPU=%d: %w", cpu, err)
		}

		if i < len(cpus)-1 {
			time.Sleep(4 * time.Second)
		}
	}

	return nil
}

// getCPUs returns the list of CPU values to test.
// If "cpus" array is present it is used; otherwise falls back to the single "cpu" value.
func (c TestConfig) getCPUs() []int {
	if len(c.CPUs) > 0 {
		return c.CPUs
	}
	if c.CPU != 0 {
		return []int{c.CPU}
	}
	return []int{0} // no CPU limit
}
