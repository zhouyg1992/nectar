package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/zhouyg1992/nectar/internal/engine"
)

// SupportsSteer reports whether this OpenCode configuration can reach the
// session's shared server. A private --standalone process has no stable
// address for a second CLI invocation.
func (a *Adapter) SupportsSteer() bool {
	return !a.cfg.Standalone || strings.TrimSpace(a.cfg.ServerURL) != ""
}

func (a *Adapter) Steer(ctx context.Context, sessionID string, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("empty steering prompt")
	}
	if !a.SupportsSteer() {
		return fmt.Errorf("opencode steering requires standalone = false or opencode.server_url")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return engine.ErrNoActiveTurn
	}
	body, err := json.Marshal(map[string]any{
		"text":     text,
		"delivery": "steer",
	})
	if err != nil {
		return fmt.Errorf("encode opencode steer request: %w", err)
	}
	args := []string{"api"}
	if serverURL := strings.TrimSpace(a.cfg.ServerURL); serverURL != "" {
		args = append(args, "--server", serverURL)
	}
	args = append(args, "--data", string(body), "POST", "/api/session/"+sessionID+"/prompt")
	cmd := exec.CommandContext(ctx, a.cfg.Executable, args...)
	cmd.Dir = firstNonEmpty(a.cfg.WorkingDir, ".")
	cmd.Env = engineEnv(cmd.Dir, nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("opencode steer: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

var _ engine.Steerer = (*Adapter)(nil)
var _ engine.SteerCapability = (*Adapter)(nil)
