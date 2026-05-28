package main

var ValidTestTypes = map[string]struct{}{
	"throughput":      {},
	"latency":         {},
	"connection-rate": {},
}

func IsValidTestType(testType string) bool {
	_, ok := ValidTestTypes[testType]
	return ok
}

// types
type TestConfig struct {
	Type      string `json:"type"` // "test" or "comparison"
	TestType  string `json:"test_type"`
	TestName  string `json:"test_name"`
	Duration  int    `json:"duration_seconds"`
	Parallel  int    `json:"parallel_streams"`
	Protocol  string `json:"protocol"`
	Port      int    `json:"port"`
	Routers   int    `json:"routers"`
	YMaxMbps  int    `json:"y_max_mbps"`
	YLabel    string `json:"y_label"`
	PlotTitle string `json:"plot_title"`
	CPU       int    `json:"cpu"`
	CPUs      []int  `json:"cpus"`

	// Latency
	TargetURL   string `json:"target_url,omitempty"`   // e.g. "http://127.0.0.1:5800/ping"
	NumRequests int    `json:"num_requests,omitempty"` // default 10_000
	Concurrency int    `json:"concurrency,omitempty"`  // default 50 (like iperf -P)

	// Comparison only
	ComparisonName string   `json:"name"`
	Title          string   `json:"title"`
	Tests          []string `json:"tests"`
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
