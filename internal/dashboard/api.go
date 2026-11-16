package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/core"
	"github.com/zhouyg1992/nectar/internal/memory"
)

type botOverview struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Platform         string         `json:"platform"`
	Engine           string         `json:"engine"`
	Model            string         `json:"model"`
	Workspace        string         `json:"workspace"`
	DataDir          string         `json:"data_dir"`
	WorkspaceMode    string         `json:"workspace_mode"`
	MemoryEnabled    bool           `json:"memory_enabled"`
	ScheduleEnabled  bool           `json:"schedule_enabled"`
	MemoryCounts     map[string]int `json:"memory_counts,omitempty"`
	MemoryError      string         `json:"memory_error,omitempty"`
	ScheduleCounts   map[string]int `json:"schedule_counts,omitempty"`
	ScheduleError    string         `json:"schedule_error,omitempty"`
	RunningSchedules int            `json:"running_schedules"`
	NextRun          *time.Time     `json:"next_run,omitempty"`
	LastError        string         `json:"last_error,omitempty"`
}

type overviewResponse struct {
	Version       string        `json:"version"`
	ConfigPath    string        `json:"config_path,omitempty"`
	ReadOnly      bool          `json:"read_only"`
	StartedAt     time.Time     `json:"started_at"`
	UptimeSeconds int64         `json:"uptime_seconds"`
	GeneratedAt   time.Time     `json:"generated_at"`
	Bots          []botOverview `json:"bots"`
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	out := overviewResponse{
		Version:       s.version,
		ConfigPath:    s.configPath,
		ReadOnly:      s.readOnly,
		StartedAt:     s.startedAt.UTC(),
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
		GeneratedAt:   now,
		Bots:          make([]botOverview, 0, len(s.botOrder)),
	}
	for _, id := range s.botOrder {
		out.Bots = append(out.Bots, s.overviewForBot(r, s.bots[id]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) overviewForBot(r *http.Request, bot Bot) botOverview {
	out := botOverview{
		ID:              bot.ID,
		Name:            bot.Name,
		Platform:        bot.Platform,
		Engine:          bot.Engine,
		Model:           bot.Model,
		Workspace:       bot.Workspace,
		DataDir:         bot.DataDir,
		WorkspaceMode:   bot.WorkspaceMode,
		MemoryEnabled:   bot.MemoryEnabled,
		ScheduleEnabled: bot.ScheduleEnabled,
	}
	if bot.Memory != nil {
		counts, err := bot.Memory.Counts(r.Context(), nil)
		if err != nil {
			out.MemoryError = err.Error()
		} else {
			out.MemoryCounts = make(map[string]int, len(counts))
			for status, count := range counts {
				out.MemoryCounts[string(status)] = count
			}
		}
	}
	if bot.Scheduler != nil {
		tasks := bot.Scheduler.List(true)
		out.ScheduleCounts = map[string]int{}
		var next *time.Time
		var lastErrorAt time.Time
		for _, task := range tasks {
			out.ScheduleCounts[string(task.Status)]++
			if task.Running {
				out.RunningSchedules++
			}
			if task.Status == core.ScheduleActive && !task.NextRun.IsZero() {
				if next == nil || task.NextRun.Before(*next) {
					value := task.NextRun
					next = &value
				}
			}
			if task.LastError != "" && task.UpdatedAt.After(lastErrorAt) {
				lastErrorAt = task.UpdatedAt
				out.LastError = task.LastError
			}
		}
		out.NextRun = next
	}
	return out
}

func (s *Server) handleBots(w http.ResponseWriter, r *http.Request) {
	out := make([]botOverview, 0, len(s.botOrder))
	for _, id := range s.botOrder {
		out = append(out, s.overviewForBot(r, s.bots[id]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"bots": out})
}

func (s *Server) handleBot(w http.ResponseWriter, r *http.Request) {
	bot, ok := s.botFromRequest(r)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("bot not found"))
		return
	}
	writeJSON(w, http.StatusOK, s.overviewForBot(r, bot))
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	bot, ok := s.botFromRequest(r)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("bot not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":             bot.ID,
		"name":           bot.Name,
		"platform":       bot.Platform,
		"engine":         bot.Engine,
		"model":          bot.Model,
		"workspace":      bot.Workspace,
		"workspace_mode": bot.WorkspaceMode,
		"data_dir":       bot.DataDir,
		"admin_users":    len(bot.AdminUsers),
		"allowed_chats":  len(bot.AllowedChats),
		"memory": map[string]any{
			"enabled":             bot.MemoryEnabled,
			"default_scope":       bot.MemoryDefaultScope,
			"max_items":           bot.MemoryMaxItems,
			"max_chars":           bot.MemoryMaxChars,
			"auto_learn_failures": bot.MemoryAutoLearnFailures,
		},
		"schedule": map[string]any{
			"enabled":        bot.ScheduleEnabled,
			"timezone":       bot.ScheduleTimezone,
			"history_limit":  bot.ScheduleHistoryLimit,
			"max_concurrent": bot.ScheduleMaxConcurrent,
			"jitter":         bot.ScheduleJitter,
			"timeout":        bot.ScheduleTimeout,
		},
	})
}

type targetView struct {
	Channel  string `json:"channel"`
	ChatID   string `json:"chat_id"`
	ThreadID string `json:"thread_id,omitempty"`
	ChatType string `json:"chat_type,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	Label    string `json:"label"`
}

func (s *Server) handleTargets(w http.ResponseWriter, r *http.Request) {
	bot, ok := s.botFromRequest(r)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("bot not found"))
		return
	}
	if bot.Anchors == nil {
		writeJSON(w, http.StatusOK, map[string]any{"targets": []targetView{}})
		return
	}
	anchors := bot.Anchors.List()
	out := make([]targetView, 0, len(anchors))
	for _, anchor := range anchors {
		if strings.TrimSpace(anchor.Channel) == "" || strings.TrimSpace(anchor.ChatID) == "" {
			continue
		}
		label := anchor.Channel + " / " + anchor.ChatID
		if anchor.ThreadID != "" {
			label += " / " + anchor.ThreadID
		}
		out = append(out, targetView{
			Channel:  anchor.Channel,
			ChatID:   anchor.ChatID,
			ThreadID: anchor.ThreadID,
			ChatType: anchor.ChatType,
			UserID:   anchor.UserID,
			Label:    label,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	writeJSON(w, http.StatusOK, map[string]any{"targets": out})
}

func (s *Server) handleMemories(w http.ResponseWriter, r *http.Request) {
	bot, ok := s.botFromRequest(r)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("bot not found"))
		return
	}
	if bot.Memory == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("memory is disabled for this bot"))
		return
	}
	query := r.URL.Query()
	status := memory.Status(strings.TrimSpace(query.Get("status")))
	if string(status) == "all" {
		status = ""
	}
	scope := memory.Scope(strings.TrimSpace(query.Get("scope")))
	if string(scope) == "all" {
		scope = ""
	}
	kind := memory.Kind(strings.TrimSpace(query.Get("kind")))
	if string(kind) == "all" {
		kind = ""
	}
	tag := strings.TrimSpace(query.Get("tag"))
	limit := 0
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	var (
		items []memory.Memory
		err   error
	)
	if q := strings.TrimSpace(query.Get("q")); q != "" {
		items, err = bot.Memory.Search(r.Context(), memory.SearchOptions{
			Query: q, Scope: scope, Limit: limit,
		})
	} else {
		items, err = bot.Memory.List(r.Context(), memory.ListOptions{
			Status: status, Scope: scope,
		})
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	filtered := items[:0]
	for _, item := range items {
		if kind != "" && item.Kind != kind {
			continue
		}
		if tag != "" && !hasTag(item.Tags, tag) {
			continue
		}
		filtered = append(filtered, item)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	counts, _ := bot.Memory.Counts(r.Context(), nil)
	outCounts := make(map[string]int, len(counts))
	for k, v := range counts {
		outCounts[string(k)] = v
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"memories": filtered,
		"counts":   outCounts,
	})
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if strings.EqualFold(strings.TrimSpace(tag), want) {
			return true
		}
	}
	return false
}

func (s *Server) handleMemoryPatch(w http.ResponseWriter, r *http.Request) {
	bot, ok := s.botFromRequest(r)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("bot not found"))
		return
	}
	if bot.Memory == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("memory is disabled for this bot"))
		return
	}
	id := strings.TrimSpace(r.PathValue("memoryID"))
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("memory id is required"))
		return
	}
	if _, exists, err := bot.Memory.Get(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	} else if !exists {
		writeError(w, http.StatusNotFound, fmt.Errorf("memory %s not found", id))
		return
	}
	var patch struct {
		Status  *string `json:"status,omitempty"`
		Pinned  *bool   `json:"pinned,omitempty"`
		Content *string `json:"content,omitempty"`
	}
	if !readJSON(w, r, &patch) {
		return
	}
	if patch.Content != nil {
		if _, err := bot.Memory.UpdateContent(r.Context(), id, *patch.Content); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	if patch.Status != nil {
		if err := bot.Memory.SetStatus(r.Context(), id, memory.Status(strings.TrimSpace(*patch.Status))); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	if patch.Pinned != nil {
		if err := bot.Memory.SetPinned(r.Context(), id, *patch.Pinned); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	updated, exists, err := bot.Memory.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, fmt.Errorf("memory %s not found", id))
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	bot, ok := s.botFromRequest(r)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("bot not found"))
		return
	}
	if bot.Memory == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("memory is disabled for this bot"))
		return
	}
	if r.URL.Query().Get("confirm") != "1" && r.URL.Query().Get("confirm") != "true" {
		writeError(w, http.StatusBadRequest, errors.New("memory delete requires confirm=1"))
		return
	}
	id := strings.TrimSpace(r.PathValue("memoryID"))
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("memory id is required"))
		return
	}
	if err := bot.Memory.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	bot, ok := s.botFromRequest(r)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("bot not found"))
		return
	}
	if bot.Scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("scheduler is disabled for this bot"))
		return
	}
	includeAll := r.URL.Query().Get("all") == "1" || r.URL.Query().Get("all") == "true"
	writeJSON(w, http.StatusOK, map[string]any{"tasks": bot.Scheduler.List(includeAll)})
}

func (s *Server) handleScheduleCreate(w http.ResponseWriter, r *http.Request) {
	bot, ok := s.botFromRequest(r)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("bot not found"))
		return
	}
	if bot.Scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("scheduler is disabled for this bot"))
		return
	}
	var req core.ScheduleRequest
	if !readJSON(w, r, &req) {
		return
	}
	s.fillScheduleDefaults(&req, bot)
	task, err := bot.Scheduler.Add(req, "dashboard")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"task": task})
}

func (s *Server) handleSchedulePreview(w http.ResponseWriter, r *http.Request) {
	bot, ok := s.botFromRequest(r)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("bot not found"))
		return
	}
	if bot.Scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("scheduler is disabled for this bot"))
		return
	}
	var req core.ScheduleRequest
	if !readJSON(w, r, &req) {
		return
	}
	s.fillScheduleDefaults(&req, bot)
	runs, err := bot.Scheduler.Preview(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"next_runs": runs})
}

func (s *Server) handleScheduleUpdate(w http.ResponseWriter, r *http.Request) {
	bot, ok := s.botFromRequest(r)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("bot not found"))
		return
	}
	if bot.Scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("scheduler is disabled for this bot"))
		return
	}
	var patch core.SchedulePatch
	if !readJSON(w, r, &patch) {
		return
	}
	task, err := bot.Scheduler.Update(strings.TrimSpace(r.PathValue("taskID")), patch)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

func (s *Server) handleScheduleDelete(w http.ResponseWriter, r *http.Request) {
	bot, ok := s.botFromRequest(r)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("bot not found"))
		return
	}
	if bot.Scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("scheduler is disabled for this bot"))
		return
	}
	id := strings.TrimSpace(r.PathValue("taskID"))
	if err := bot.Scheduler.Delete(id); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (s *Server) handleScheduleAction(w http.ResponseWriter, r *http.Request) {
	bot, ok := s.botFromRequest(r)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("bot not found"))
		return
	}
	if bot.Scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("scheduler is disabled for this bot"))
		return
	}
	id := strings.TrimSpace(r.PathValue("taskID"))
	action := strings.ToLower(strings.TrimSpace(r.PathValue("action")))
	var err error
	switch action {
	case "pause":
		err = bot.Scheduler.Pause(id)
	case "resume":
		err = bot.Scheduler.Resume(id)
	case "run":
		err = bot.Scheduler.RunNow(id)
	case "cancel", "stop":
		err = bot.Scheduler.Cancel(id)
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported schedule action %q", action))
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "ok"})
}

func (s *Server) fillScheduleDefaults(req *core.ScheduleRequest, bot Bot) {
	if strings.TrimSpace(req.Channel) == "" {
		req.Channel = bot.DefaultChannel
	}
	if strings.TrimSpace(req.AgentID) == "" {
		req.AgentID = bot.DefaultAgent
	}
	if strings.TrimSpace(req.BotID) == "" {
		req.BotID = bot.ID
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, errors.New("request body is required"))
		} else {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid json: %w", err))
		}
		return false
	}
	return true
}
