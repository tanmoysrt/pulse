package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// sleepInhibitor keeps the operating system awake while Pulse runs. It
// deliberately does not prevent the display from turning off.
type sleepInhibitor struct {
	cmd *exec.Cmd
}

func startSleepInhibitor() (*sleepInhibitor, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("caffeinate", "-i")
	case "linux":
		cmd = exec.Command(
			"systemd-inhibit",
			"--what=sleep",
			"--mode=block",
			"--why=Pulse is running",
			"sleep",
			"infinity",
		)
	default:
		return nil, fmt.Errorf("automatic sleep prevention is not supported on %s", runtime.GOOS)
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &sleepInhibitor{cmd: cmd}, nil
}

func (i *sleepInhibitor) stop() {
	if i == nil || i.cmd == nil || i.cmd.Process == nil {
		return
	}
	_ = i.cmd.Process.Kill()
	_, _ = i.cmd.Process.Wait()
}
