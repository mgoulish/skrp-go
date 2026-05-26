package main

import (
	"encoding/json"
        "errors"
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
        fp ( "Config: %+v\n", config)

        // What is 'Type' in config?
        if ! IsValidTestType ( config.TestType ) {
                fp ( "Bad test type: %s\n", config.TestType )
                fp ( "Valid test types are: %+v\n", ValidTestTypes)
                return errors.New("Bad test type")
        }


	if config.Type == "comparison" {
		return runComparison(skupperVersion, config)
	}

	return runTest(skupperVersion, config, data)
}

