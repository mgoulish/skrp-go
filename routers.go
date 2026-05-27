package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func waitForRouterReady() {
	fmt.Println("   → Waiting for router listener on port 5800...")
	for i := 0; i < 35; i++ {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:5800", 800*time.Millisecond)
		if err == nil {
			conn.Close()
			fmt.Println("   → Router listener is READY!")
			time.Sleep(500 * time.Millisecond)
			return
		}
		time.Sleep(700 * time.Millisecond)
	}
	fmt.Println("   Warning: Router listener not responding after ~24s")
}

// startSkupperRouters starts the router(s) and redirects ALL output (stdout + stderr)
// exclusively to log files in the test's output/ directory.
// No router logs appear on the console anymore.
func startSkupperRouters(numRouters int, baseDir, commandsDir, outputDir string, cpu int) ([]*os.Process, error) {
	var procs []*os.Process

	if cpu != 0 {
		fp("Warning: limiting router CPU to %d%% per router.\n", cpu)
	}

	cpu_quota_str := fmt.Sprintf("--property=CPUQuota=%d%%", cpu)

	if numRouters == 1 {
		routerConfig := `router {
    mode: interior
    id: skrp-router-A
    workerThreads: 4
}
tcpListener {
    host: 0.0.0.0
    port: 5800
    address: router-test
    siteId: skrp-test
}
tcpConnector {
    host: 127.0.0.1
    port: 5801
    address: router-test
    siteId: skrp-test
}`
		writeRouterFiles(baseDir, commandsDir, "router.conf", routerConfig)

		logPath := filepath.Join(outputDir, "router.log")
		logFile, err := os.Create(logPath)
		if err != nil {
			fmt.Printf("   Warning: could not create router.log: %v\n", err)
		} else {
			cmd := createRouterCmd(cpu, cpu_quota_str, filepath.Join(baseDir, "router.conf"))
			cmd.Stdout = logFile
			cmd.Stderr = logFile
			if err := cmd.Start(); err != nil {
				logFile.Close()
				return procs, err
			}
			procs = append(procs, cmd.Process)
			fmt.Printf("   → Router started (PID %d) → logs saved to: output/router.log\n", cmd.Process.Pid)
			// logFile stays open until the test ends (router is killed in cleanup)
		}

	} else if numRouters == 2 {
		// Router A
		routerA := `router {
    mode: interior
    id: skrp-router-A
    workerThreads: 4
}
listener {
    stripAnnotations: no
    idleTimeoutSeconds: 120
    saslMechanisms: ANONYMOUS
    host: 0.0.0.0
    role: inter-router
    authenticatePeer: no
    port: 25000
}
tcpListener {
    host: 0.0.0.0
    port: 5800
    address: router-test
    siteId: skrp-multi-test
}`
		writeRouterFiles(baseDir, commandsDir, "router-A.conf", routerA)

		logAPath := filepath.Join(outputDir, "router-A.log")
		logAFile, _ := os.Create(logAPath)
		cmdA := createRouterCmd(cpu, cpu_quota_str, filepath.Join(baseDir, "router-A.conf"))
		cmdA.Stdout = logAFile
		cmdA.Stderr = logAFile
		if err := cmdA.Start(); err != nil {
			logAFile.Close()
		} else {
			procs = append(procs, cmdA.Process)
			fmt.Printf("   → Router A started (PID %d) → logs saved to: output/router-A.log\n", cmdA.Process.Pid)
		}

		// Router B
		routerB := `router {
    mode: interior
    id: skrp-router-B
    workerThreads: 4
}
connector {
    stripAnnotations: no
    name: connectorToA
    idleTimeoutSeconds: 120
    saslMechanisms: ANONYMOUS
    host: 127.0.0.1
    role: inter-router
    port: 25000
}
tcpConnector {
    host: 127.0.0.1
    port: 5801
    address: router-test
    siteId: skrp-multi-test
}`
		writeRouterFiles(baseDir, commandsDir, "router-B.conf", routerB)

		logBPath := filepath.Join(outputDir, "router-B.log")
		logBFile, _ := os.Create(logBPath)
		cmdB := createRouterCmd(cpu, cpu_quota_str, filepath.Join(baseDir, "router-B.conf"))
		cmdB.Stdout = logBFile
		cmdB.Stderr = logBFile
		if err := cmdB.Start(); err != nil {
			logBFile.Close()
		} else {
			procs = append(procs, cmdB.Process)
			fmt.Printf("   → Router B started (PID %d) → logs saved to: output/router-B.log\n", cmdB.Process.Pid)
		}
	}

	return procs, nil
}

func writeRouterFiles(baseDir, commandsDir, filename, content string) {
	_ = os.WriteFile(filepath.Join(baseDir, filename), []byte(content), 0644)
	_ = os.WriteFile(filepath.Join(commandsDir, filename), []byte(content), 0644)
}

func cleanupRouters(procs []*os.Process) {
	fmt.Println("   → Shutting down routers...")
	for _, p := range procs {
		if p != nil {
			p.Kill()
			p.Wait()
		}
	}
}

// Small helper so we don't duplicate the systemd-run / skrouterd logic
func createRouterCmd(cpu int, cpu_quota_str, configPath string) *exec.Cmd {
	if cpu == 0 {
		return exec.Command("skrouterd", "-c", configPath)
	}
	return exec.Command("systemd-run",
		"--user",
		"--scope",
		cpu_quota_str,
		"--",
		"skrouterd",
		"-c", configPath,
	)
}
