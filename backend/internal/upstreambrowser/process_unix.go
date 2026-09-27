//go:build linux

package upstreambrowser

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func startProcess(cmd *exec.Cmd) (func(), error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	identity, err := linuxProcessIdentity(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	return func() { killLinuxTree(cmd.Process.Pid, identity.started); _ = cmd.Process.Kill() }, nil
}

type linuxIdentity struct {
	parent, group int
	started       string
}

func linuxProcessIdentity(pid int) (linuxIdentity, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil || len(data) > 8192 {
		return linuxIdentity{}, errors.New("browser process identity unavailable")
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return linuxIdentity{}, errors.New("browser process identity invalid")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return linuxIdentity{}, errors.New("browser process identity invalid")
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return linuxIdentity{}, err
	}
	group, err := strconv.Atoi(fields[2])
	if err != nil {
		return linuxIdentity{}, err
	}
	return linuxIdentity{parent: parent, group: group, started: fields[19]}, nil
}

func browserProcessCleanup(parent *os.Process, pid int) (func(), error) {
	identity, err := linuxProcessIdentity(pid)
	if err != nil {
		return nil, err
	}
	ancestor := pid
	for depth := 0; depth < 32; depth++ {
		if ancestor == parent.Pid {
			return func() { killLinuxTree(pid, identity.started) }, nil
		}
		info, err := linuxProcessIdentity(ancestor)
		if err != nil || info.parent <= 1 || info.parent == ancestor {
			break
		}
		ancestor = info.parent
	}
	return nil, errors.New("browser process is not owned by helper")
}

func killLinuxTree(pid int, started string) {
	identity, err := linuxProcessIdentity(pid)
	if err != nil || identity.started != started {
		return
	}
	// Chromium may detach from Node's process group. Discover descendants from
	// every thread before terminating the parent, and guard against PID reuse.
	type member struct {
		pid      int
		identity linuxIdentity
	}
	members := []member{{pid, identity}}
	seen := map[int]bool{pid: true}
	for index := 0; index < len(members) && len(members) < 256; index++ {
		tasks, _ := os.ReadDir(filepath.Join("/proc", strconv.Itoa(members[index].pid), "task"))
		for _, task := range tasks {
			children, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(members[index].pid), "task", task.Name(), "children"))
			if len(children) > 8192 {
				continue
			}
			for _, child := range strings.Fields(string(children)) {
				childPID, err := strconv.Atoi(child)
				if err != nil || childPID <= 1 || seen[childPID] || len(members) >= 256 {
					continue
				}
				childIdentity, err := linuxProcessIdentity(childPID)
				if err != nil || childIdentity.parent != members[index].pid {
					continue
				}
				seen[childPID] = true
				members = append(members, member{childPID, childIdentity})
			}
		}
	}
	for index := len(members) - 1; index >= 0; index-- {
		member := members[index]
		current, err := linuxProcessIdentity(member.pid)
		if err != nil || current.started != member.identity.started {
			continue
		}
		if current.group == member.pid {
			_ = syscall.Kill(-member.pid, syscall.SIGKILL)
		}
		_ = syscall.Kill(member.pid, syscall.SIGKILL)
	}
}
