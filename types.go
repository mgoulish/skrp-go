package main



// types
type TestConfig struct {
	Type           string   `json:"type"`           // "test" or "comparison"
	TestType       string   `json:"test_type"`
	TestName       string   `json:"test_name"`
	Duration       int      `json:"duration_seconds"`
	Parallel       int      `json:"parallel_streams"`
	Protocol       string   `json:"protocol"`
	Port           int      `json:"port"`
	Routers        int      `json:"routers"`
	YMaxMbps       int      `json:"y_max_mbps"`

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

