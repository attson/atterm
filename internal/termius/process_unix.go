//go:build !windows

package termius

import (
	"context"
	"os/exec"
	"strings"
)

func processNames(ctx context.Context) ([]string, error) {
	// comm excludes arguments, so a shell/editor mentioning Termius is not a match.
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "comm=").Output()
	if err != nil {
		return nil, err
	}
	return strings.Split(string(out), "\n"), nil
}
