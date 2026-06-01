package main

import (
	"encoding/json"
	"time"
)

// TODO -- add 'comparison'  Maybe...
var ValidTestTypes = map[string]struct{}{
	"throughput":      {},
	"latency":         {},
	"connection-rate": {},
}

func IsValidTestType(testType string) bool {
	_, ok := ValidTestTypes[testType]
	return ok
}

// RunInfo is now shared (was duplicated inside the test functions)
type RunInfo struct {
	SkupperVersion string     `json:"skupper_version"`
	Date           string     `json:"date"` // <-- NEW for comparison logic
	TestConfig     TestConfig `json:"test_config"`
	RunTime        time.Time  `json:"run_time"`
}

// ComparisonSpecifier is the new comparison file format you described
type ComparisonSpecifier struct {
	ComparisonType string `json:"comparison_type"`
	YLabel         string `json:"y_label"`
	// Selectors are everything else; we parse them as interface{} so they can be scalar or []
	Selectors map[string]interface{} `json:"-"` // populated by us after unmarshal
}

// IsComparisonFile returns true if this looks like your new specifier format
func IsComparisonFile(data []byte) bool {
	var m map[string]interface{}
	if json.Unmarshal(data, &m) != nil {
		return false
	}
	_, hasType := m["comparison_type"]
	return hasType
}

// types
type TestConfig struct {
	Type      string `json:"type"` // "throughput", "latency", or "connection-rate"
	TestType  string `json:"test_type"`
	TestName  string `json:"test_name"`
	Duration  int    `json:"duration_seconds"`
	Parallel  int    `json:"parallel_streams"`
	Protocol  string `json:"protocol"`
	Port      int    `json:"port"`
	Routers   int    `json:"routers"`
	YLabel    string `json:"y_label"`
	PlotTitle string `json:"plot_title"`
	CPU       int    `json:"cpu"`
	CPUs      []int  `json:"cpus"`

	// Latency
	TargetURL   string `json:"target_url,omitempty"`   // e.g. "http://127.0.0.1:5800/ping"
	NumRequests int    `json:"num_requests,omitempty"` // default 10_000
	Concurrency int    `json:"concurrency,omitempty"`  // default 50 (like iperf -P)
}

type Iperf3Result struct {
	End struct {
		SumSent struct {
			BitsPerSecond float64 `json:"bits_per_second"`
		} `json:"sum_sent"`
	} `json:"end"`
	Intervals []struct {
		Sum struct {
			BitsPerSecond float64 `json:"bits_per_second"`
		} `json:"sum"`
	} `json:"intervals"`
}
