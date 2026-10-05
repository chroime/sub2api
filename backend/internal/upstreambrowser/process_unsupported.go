//go:build !windows && !linux

package upstreambrowser

import (
	"os"
	"os/exec"
)

func startProcess(_ *exec.Cmd) (func(), error)                   { return nil, ErrUnavailable }
func browserProcessCleanup(_ *os.Process, _ int) (func(), error) { return nil, ErrUnavailable }
