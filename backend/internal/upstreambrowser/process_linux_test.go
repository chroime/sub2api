//go:build linux

package upstreambrowser

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLinuxProcessHelper(t *testing.T) {
	mode := os.Getenv("UPSTREAM_BROWSER_PROCESS_TEST")
	if mode == "" {
		return
	}
	if mode == "parent" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLinuxProcessHelper$", "-test.timeout=30s")
		cmd.Env = append(os.Environ(), "UPSTREAM_BROWSER_PROCESS_TEST=child")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stdout, cmd.Process.Pid)
	}
	for {
		time.Sleep(time.Second)
	}
}

func linuxFixtureProcess(t *testing.T) (*exec.Cmd, int, func()) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLinuxProcessHelper$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "UPSTREAM_BROWSER_PROCESS_TEST=parent")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	kill, err := startProcess(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 1 {
		t.Fatalf("invalid child pid: %q %v", line, err)
	}
	return cmd, pid, kill
}

func assertLinuxProcessDead(t *testing.T, pid int) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(err) || strings.Contains(string(data), ") Z ") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("detached browser process survived cleanup")
}

func TestLinuxDetachedProcessCleanupSurvivesParentExit(t *testing.T) {
	parent, child, _ := linuxFixtureProcess(t)
	if _, err := browserProcessCleanup(parent.Process, os.Getpid()); err == nil {
		t.Fatal("unrelated parent process accepted")
	}
	cleanup, err := browserProcessCleanup(parent.Process, child)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := linuxProcessIdentity(child)
	if err != nil || identity.group != child {
		t.Fatalf("fixture did not detach: %#v %v", identity, err)
	}
	killLinuxTree(child, identity.started+"0")
	if _, err := linuxProcessIdentity(child); err != nil {
		t.Fatal("mismatched process identity was killed")
	}
	_ = parent.Process.Kill()
	_ = parent.Wait()
	cleanup()
	assertLinuxProcessDead(t, child)
}

func TestLinuxForcedCleanupFindsDetachedDescendants(t *testing.T) {
	_, child, kill := linuxFixtureProcess(t)
	kill()
	assertLinuxProcessDead(t, child)
}
