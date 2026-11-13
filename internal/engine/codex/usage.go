package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/engine"
)

type rawTokenUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
	TotalTokens           int `json:"total_tokens"`
}

func (u rawTokenUsage) empty() bool {
	return u.InputTokens == 0 &&
		u.CachedInputTokens == 0 &&
		u.OutputTokens == 0 &&
		u.TotalTokens == 0
}

func (u rawTokenUsage) contextTokens() int {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.InputTokens + u.OutputTokens
}

func (u rawTokenUsage) uncachedInputTokens() int {
	value := u.InputTokens - u.CachedInputTokens
	if value < 0 {
		return 0
	}
	return value
}

type codexSessionUsage struct {
	Last          rawTokenUsage
	Turn          rawTokenUsage
	ContextWindow int
	HasLast       bool
	HasTurn       bool
}

func codexUsageMeta(last, turn *rawTokenUsage) map[string]any {
	if last == nil && turn == nil {
		return nil
	}

	data := map[string]int{}
	// last_token_usage is the final model call and describes current context;
	// turn_token_usage accumulates every model call in the user turn.
	if last != nil && !last.empty() {
		data["context_tokens"] = last.contextTokens()
	}
	usage := turn
	if usage == nil || usage.empty() {
		usage = last
	}
	if usage != nil && !usage.empty() {
		data["input_tokens"] = usage.uncachedInputTokens()
		data["cached_input_tokens"] = usage.CachedInputTokens
		data["output_tokens"] = usage.OutputTokens
	}
	if len(data) == 0 {
		return nil
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	return map[string]any{"usage": string(encoded)}
}

func applyCodexSessionUsage(ev *engine.Event, sessionID string) {
	snapshot := readCodexSessionUsage(sessionID)
	if snapshot == nil {
		return
	}

	var last, turn *rawTokenUsage
	if snapshot.HasLast && !snapshot.Last.empty() {
		last = &snapshot.Last
	}
	if snapshot.HasTurn && !snapshot.Turn.empty() {
		turn = &snapshot.Turn
	}
	if meta := codexUsageMeta(last, turn); meta != nil {
		if ev.Meta == nil {
			ev.Meta = map[string]any{}
		}
		ev.Meta["usage"] = meta["usage"]
	}
	if snapshot.ContextWindow > 0 {
		if ev.Meta == nil {
			ev.Meta = map[string]any{}
		}
		ev.Meta["context_window"] = snapshot.ContextWindow
	}
}

func readCodexSessionUsage(sessionID string) *codexSessionUsage {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	path := findCodexSessionFile(sessionID)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var snapshot codexSessionUsage
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.Contains(line, `"token_count"`):
			var record struct {
				Type    string `json:"type"`
				Payload struct {
					Type string `json:"type"`
					Info *struct {
						LastTokenUsage     *rawTokenUsage `json:"last_token_usage"`
						ModelContextWindow int            `json:"model_context_window"`
					} `json:"info"`
				} `json:"payload"`
			}
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				continue
			}
			if record.Type != "event_msg" || record.Payload.Type != "token_count" || record.Payload.Info == nil {
				continue
			}
			if record.Payload.Info.LastTokenUsage != nil {
				snapshot.Last = *record.Payload.Info.LastTokenUsage
				snapshot.HasLast = true
			}
			if record.Payload.Info.ModelContextWindow > 0 {
				snapshot.ContextWindow = record.Payload.Info.ModelContextWindow
			}
		case strings.Contains(line, `"token_usage_record"`):
			var record struct {
				Type    string `json:"type"`
				Payload struct {
					TurnTokenUsage *rawTokenUsage `json:"turn_token_usage"`
				} `json:"payload"`
			}
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				continue
			}
			if record.Type != "token_usage_record" || record.Payload.TurnTokenUsage == nil {
				continue
			}
			snapshot.Turn = *record.Payload.TurnTokenUsage
			snapshot.HasTurn = true
		}
	}

	if !snapshot.HasLast && !snapshot.HasTurn && snapshot.ContextWindow == 0 {
		return nil
	}
	return &snapshot
}

func findCodexSessionFile(sessionID string) string {
	home := codexHome()
	if home == "" {
		return ""
	}
	root := filepath.Join(home, "sessions")

	patterns := []string{
		filepath.Join(root, "*", "*", "*", "rollout-*-"+sessionID+".jsonl"),
		filepath.Join(root, "*", "*", "*", "*"+sessionID+"*.jsonl"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			continue
		}
		if path := newestFile(matches); path != "" {
			return path
		}
	}

	var bestPath string
	var bestMod time.Time
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if !strings.HasSuffix(name, ".jsonl") || !strings.Contains(name, sessionID) {
			return nil
		}
		if bestPath == "" || info.ModTime().After(bestMod) {
			bestPath = path
			bestMod = info.ModTime()
		}
		return nil
	})
	return bestPath
}

func newestFile(paths []string) string {
	var bestPath string
	var bestMod time.Time
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if bestPath == "" || info.ModTime().After(bestMod) {
			bestPath = path
			bestMod = info.ModTime()
		}
	}
	return bestPath
}
