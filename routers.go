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

func startSkupperRouters(numRouters int, baseDir, commandsDir string, cpu int) ([]*os.Process, error) {
	var procs []*os.Process

	// We will test cpu for zero just before command execution.
	cpu_quota_str := fmt.Sprintf("--property=CPUQuota=%d%%", cpu)
	var cmd, cmdA, cmdB *exec.Cmd

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

		if cpu == 0 {
			cmd = exec.Command("skrouterd", "-c", filepath.Join(baseDir, "router.conf"))
		} else {
			cmd = exec.Command("systemd-run",
				"--user",
				"--scope",
				cpu_quota_str,
				"--",
				"skrouterd",
				"-c", filepath.Join(baseDir, "router.conf"),
			)
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Start()
		procs = append(procs, cmd.Process)
		fmt.Printf("   → Single Router started (PID %d)\n", cmd.Process.Pid)

	} else if numRouters == 2 {
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

		if cpu == 0 {
			cmdA = exec.Command("skrouterd", "-c", filepath.Join(baseDir, "router-A.conf"))
		} else {
			cmdA = exec.Command("systemd-run",
				"--user",
				"--scope",
				cpu_quota_str,
				"--",
				"skrouterd",
				"-c",
				filepath.Join(baseDir, "router-A.conf"),
			)
		}

		cmdA.Stdout = os.Stdout
		cmdA.Stderr = os.Stderr
		cmdA.Start()
		procs = append(procs, cmdA.Process)
		fmt.Printf("   → Router A started (PID %d)\n", cmdA.Process.Pid)

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

		if cpu == 0 {
			cmdB = exec.Command("skrouterd", "-c", filepath.Join(baseDir, "router-B.conf"))
		} else {
			cmdB = exec.Command("systemd-run",
				"--user",
				"--scope",
				cpu_quota_str,
				"--",
				"skrouterd",
				"-c",
				filepath.Join(baseDir, "router-B.conf"),
			)
		}
		cmdB.Stdout = os.Stdout
		cmdB.Stderr = os.Stderr
		cmdB.Start()
		procs = append(procs, cmdB.Process)
		fmt.Printf("   → Router B started (PID %d)\n", cmdB.Process.Pid)
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
