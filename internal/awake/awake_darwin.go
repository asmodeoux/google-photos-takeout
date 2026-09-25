package awake

import (
	"os"
	"os/exec"
	"strconv"
)

// caffeinate -i stops idle sleep; -w ends it when takeout exits, even if
// takeout is killed.
func hold() (func(), error) {
	p, err := exec.LookPath("caffeinate")
	if err != nil {
		return nil, nil
	}
	cmd := exec.Command(p, "-i", "-w", strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}, nil
}
