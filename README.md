# skrp-go
Skupper Router Performance suite in Go
# skrp — Skupper Router Performance Tester

`skrp` is a Go tool for running reproducible performance tests against the [Skupper Router](https://github.com/skupperproject/skupper-router) .

---

## Features

- Throughput, Latency, and Connection Rate tests
- All test types with same output layout directory structure
- Moving-average graphs for all tests
- Limiting router CPU during tests:  `"cpus": [25, 50, 100, ...]`
- Full absolute paths to raw data files printed on every run, so you can find them
- `commands/` directory made that has everything you need to rerun a test manually.
- Results are written to `skrp_results` dir in the current working directory
- Router logs captured to `output/router*.log` 
- `--show-graphs` flag to optionally pop up the generated graphs
- Example test spec JSON files in the `test_scripts` directory
- Program tests at startup for required executables: skrouterd, iperf3, hey, gnuplot

---

## Quick Start

```bash
# 1. Build
go build -o skrp .

# 2. Run a test (or many)
`./skrp v1.2.3 test_scripts/throughput/r_1_cpu_100.json`

# 3. Run with graphs popping up
`./skrp --show-graphs v1.2.3 tests_scripts/latency/r_1_cpu_200.json`



