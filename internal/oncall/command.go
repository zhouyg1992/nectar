package oncall

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type commandRunner func(ctx context.Context, dir, name string, args ...string) ([]byte, error)

func execCommand(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			return out, err
		}
		return out, fmt.Errorf("%w: %s", err, message)
	}
	return out, nil
}
