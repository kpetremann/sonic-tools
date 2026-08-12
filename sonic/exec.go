package sonic

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const commandTimeout = 30 * time.Second

func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("failed to run '%s %s': %w: %s", name, strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// runDetached runs a command in its own process group, so it is not killed with the caller.
// It avoids a corrupted state when saving a configuration.
func runDetached(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("failed to run '%s %s': %w: %s", name, strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

func runJSON(out any, name string, args ...string) error {
	res, err := run(name, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(res), out); err != nil {
		return fmt.Errorf("failed to parse output of '%s': %w", name, err)
	}
	return nil
}

func vtysh(command string) (string, error) {
	return run("vtysh", "-c", command)
}

func vtyshJSON(out any, command string) error {
	return runJSON(out, "vtysh", "-c", command)
}

// lines splits a command output, dropping empty lines.
func lines(out string) []string {
	res := []string{}
	for line := range strings.SplitSeq(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			res = append(res, line)
		}
	}
	return res
}
