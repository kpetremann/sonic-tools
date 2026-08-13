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

// run executes a command under the deadline of the caller, capped by commandTimeout. Whichever
// expires first kills the command, so a collection cannot outlive the context it was given.
func run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("failed to run '%s %s': %w: %s", name, strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// runDetached runs a command in its own process group, so it is not killed with the caller.
// It avoids a corrupted state when saving a configuration, which is also why it takes no
// context: a save must not be interrupted by the deadline of the command which asked for it.
func runDetached(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("failed to run '%s %s': %w: %s", name, strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

func runJSON(ctx context.Context, out any, name string, args ...string) error {
	res, err := run(ctx, name, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(res), out); err != nil {
		return fmt.Errorf("failed to parse output of '%s': %w", name, err)
	}
	return nil
}

func vtysh(ctx context.Context, command string) (string, error) {
	return run(ctx, "vtysh", "-c", command)
}

func vtyshJSON(ctx context.Context, out any, command string) error {
	return runJSON(ctx, out, "vtysh", "-c", command)
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
