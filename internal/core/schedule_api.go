package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/memory"
)

type ScheduleAPIOptions struct {
	SocketPath     string
	Scheduler      *Scheduler
	Memory         memory.Store
	Logger         *slog.Logger
	DefaultChannel string
	DefaultAgent   string
	AdminUsers     []string
}

// ScheduleAPI exposes the scheduler and memory store to local CLIs over a Unix
// socket. The socket is chmod 0600 and lives under the data directory, so
// filesystem permissions are the authentication boundary.
type ScheduleAPI struct {
	socketPath     string
	scheduler      *Scheduler
	memory         memory.Store
	logger         *slog.Logger
	defaultChannel string
	defaultAgent   string
	adminUsers     []string
	server         *http.Server
	listener       net.Listener
}

func NewScheduleAPI(opts ScheduleAPIOptions) *ScheduleAPI {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &ScheduleAPI{
		socketPath:     opts.SocketPath,
		scheduler:      opts.Scheduler,
		memory:         opts.Memory,
		logger:         logger,
		defaultChannel: opts.DefaultChannel,
		defaultAgent:   opts.DefaultAgent,
		adminUsers:     opts.AdminUsers,
	}
}

func (a *ScheduleAPI) Start(ctx context.Context) error {
	if a.scheduler == nil && a.memory == nil {
		return errors.New("local api: scheduler and memory are nil")
	}
	if strings.TrimSpace(a.socketPath) == "" {
		return errors.New("schedule api: socket path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(a.socketPath), 0o700); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	if _, err := os.Stat(a.socketPath); err == nil {
		conn, dialErr := net.DialTimeout("unix", a.socketPath, 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return fmt.Errorf("schedule api socket %s is already in use", a.socketPath)
		}
		if err := os.Remove(a.socketPath); err != nil {
			return fmt.Errorf("remove stale socket: %w", err)
		}
	}

	listener, err := net.Listen("unix", a.socketPath)
	if err != nil {
		return fmt.Errorf("listen on schedule socket: %w", err)
	}
	if err := os.Chmod(a.socketPath, 0o600); err != nil {
		_ = listener.Close()
		return fmt.Errorf("chmod schedule socket: %w", err)
	}
	a.listener = listener
	a.server = &http.Server{Handler: a.Handler()}
	go func() {
		if err := a.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.logger.Error("schedule api stopped", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		_ = a.Close()
	}()
	return nil
}

func (a *ScheduleAPI) Close() error {
	var err error
	if a.server != nil {
		err = a.server.Close()
	}
	if a.listener != nil {
		_ = a.listener.Close()
	}
	if a.socketPath != "" {
		_ = os.Remove(a.socketPath)
	}
	return err
}

func (a *ScheduleAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/schedule", a.handleCollection)
	mux.HandleFunc("/schedule/preview", a.handlePreview)
	mux.HandleFunc("/schedule/", a.handleItem)
	mux.HandleFunc("/memory", a.handleMemoryCollection)
	mux.HandleFunc("/memory/", a.handleMemoryItem)
	return mux
}

func (a *ScheduleAPI) handleCollection(w http.ResponseWriter, r *http.Request) {
	if a.scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("scheduler is disabled"))
		return
	}
	if !a.authorize(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		includeAll := r.URL.Query().Get("all") == "1" || r.URL.Query().Get("all") == "true"
		writeJSON(w, http.StatusOK, map[string]any{"tasks": a.scheduler.List(includeAll)})
	case http.MethodPost:
		var req ScheduleRequest
		if err := decodeJSON(w, r, &req); err != nil {
			return
		}
		a.fillDefaults(&req)
		createdBy := strings.TrimSpace(r.Header.Get("X-Nectar-User-ID"))
		if createdBy == "" {
			createdBy = "api"
		}
		task, err := a.scheduler.Add(req, createdBy)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"task": task})
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (a *ScheduleAPI) handlePreview(w http.ResponseWriter, r *http.Request) {
	if a.scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("scheduler is disabled"))
		return
	}
	if !a.authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	var req ScheduleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	a.fillDefaults(&req)
	next, err := a.scheduler.Preview(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"next_runs": next})
}

func (a *ScheduleAPI) handleItem(w http.ResponseWriter, r *http.Request) {
	if a.scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("scheduler is disabled"))
		return
	}
	if !a.authorize(w, r) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/schedule/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, errors.New("schedule id is required"))
		return
	}
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = strings.ToLower(parts[1])
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		task, ok := a.scheduler.Get(id)
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Errorf("schedule %s not found", id))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task": task})
	case action == "" && r.Method == http.MethodPatch:
		var patch SchedulePatch
		if err := decodeJSON(w, r, &patch); err != nil {
			return
		}
		task, err := a.scheduler.Update(id, patch)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task": task})
	case action == "" && r.Method == http.MethodDelete:
		if err := a.scheduler.Delete(id); err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
	case action == "pause" && r.Method == http.MethodPost:
		a.handleAction(w, id, a.scheduler.Pause)
	case action == "resume" && r.Method == http.MethodPost:
		a.handleAction(w, id, a.scheduler.Resume)
	case action == "run" && r.Method == http.MethodPost:
		a.handleAction(w, id, a.scheduler.RunNow)
	case (action == "cancel" || action == "stop") && r.Method == http.MethodPost:
		a.handleAction(w, id, a.scheduler.Cancel)
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method or action not allowed"))
	}
}

func (a *ScheduleAPI) handleMemoryCollection(w http.ResponseWriter, r *http.Request) {
	if a.memory == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("memory is disabled"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		query := r.URL.Query()
		status := memory.Status(strings.TrimSpace(query.Get("status")))
		if status == "all" {
			status = ""
		}
		scope := memory.Scope(strings.TrimSpace(query.Get("scope")))
		if scope == "all" {
			scope = ""
		}
		scopeIDs := splitCSV(query.Get("scope_ids"))
		limit := 0
		if value := strings.TrimSpace(query.Get("limit")); value != "" {
			if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		var (
			items []memory.Memory
			err   error
		)
		if q := strings.TrimSpace(query.Get("q")); q != "" {
			items, err = a.memory.Search(r.Context(), memory.SearchOptions{
				Query: q, Scope: scope, ScopeIDs: scopeIDs, Limit: limit,
			})
		} else {
			items, err = a.memory.List(r.Context(), memory.ListOptions{
				Status: status, Scope: scope, ScopeIDs: scopeIDs,
			})
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"memories": items})
	case http.MethodPost:
		var item memory.Memory
		if err := decodeJSON(w, r, &item); err != nil {
			return
		}
		if err := a.prepareMemoryWrite(r, &item, true); err != nil {
			writeError(w, http.StatusForbidden, err)
			return
		}
		created, err := a.memory.Put(r.Context(), item)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (a *ScheduleAPI) handleMemoryItem(w http.ResponseWriter, r *http.Request) {
	if a.memory == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("memory is disabled"))
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/memory/"), "/")
	if id == "" {
		writeError(w, http.StatusNotFound, errors.New("memory id is required"))
		return
	}
	item, ok, err := a.memory.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("memory %s not found", id))
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, item)
	case http.MethodPatch:
		if err := a.prepareMemoryWrite(r, &item, false); err != nil {
			writeError(w, http.StatusForbidden, err)
			return
		}
		var patch struct {
			Status  *string `json:"status,omitempty"`
			Pinned  *bool   `json:"pinned,omitempty"`
			Content *string `json:"content,omitempty"`
		}
		if err := decodeJSON(w, r, &patch); err != nil {
			return
		}
		if patch.Content != nil {
			if _, err := a.memory.UpdateContent(r.Context(), id, *patch.Content); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		if patch.Status != nil {
			if err := a.memory.SetStatus(r.Context(), id, memory.Status(strings.TrimSpace(*patch.Status))); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		if patch.Pinned != nil {
			if err := a.memory.SetPinned(r.Context(), id, *patch.Pinned); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		updated, ok, err := a.memory.Get(r.Context(), id)
		if err != nil || !ok {
			writeError(w, http.StatusBadRequest, fmt.Errorf("reload memory: %w", err))
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if r.URL.Query().Get("confirm") != "1" && r.URL.Query().Get("confirm") != "true" {
			writeError(w, http.StatusBadRequest, errors.New("memory delete requires confirm=1"))
			return
		}
		if err := a.prepareMemoryWrite(r, &item, false); err != nil {
			writeError(w, http.StatusForbidden, err)
			return
		}
		if err := a.memory.Delete(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
	default:
		w.Header().Set("Allow", "GET, PATCH, DELETE")
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (a *ScheduleAPI) prepareMemoryWrite(r *http.Request, item *memory.Memory, creating bool) error {
	userID := strings.TrimSpace(r.Header.Get("X-Nectar-User-ID"))
	admin := a.memoryAdmin(userID)
	if item.Scope == memory.ScopeGlobal && !admin {
		return errors.New("global memory requires an admin user")
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Nectar-Chat-Type")), "group") && !admin {
		if creating {
			if item.Status == memory.StatusActive {
				item.Status = memory.StatusCandidate
			}
			return nil
		}
		return errors.New("group memory changes require an admin user")
	}
	return nil
}

func (a *ScheduleAPI) memoryAdmin(userID string) bool {
	if strings.TrimSpace(userID) == "" || len(a.adminUsers) == 0 {
		return true
	}
	for _, admin := range a.adminUsers {
		if strings.TrimSpace(admin) == userID {
			return true
		}
	}
	return false
}

func splitCSV(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (a *ScheduleAPI) authorize(w http.ResponseWriter, r *http.Request) bool {
	if len(a.adminUsers) == 0 {
		return true
	}
	userID := strings.TrimSpace(r.Header.Get("X-Nectar-User-ID"))
	if userID == "" {
		// Local operator invoked the CLI directly. The socket is 0600, so this
		// remains a same-user trust boundary.
		return true
	}
	for _, admin := range a.adminUsers {
		if strings.TrimSpace(admin) == userID {
			return true
		}
	}
	writeError(w, http.StatusForbidden, fmt.Errorf("user %s is not a schedule admin", userID))
	return false
}

func (a *ScheduleAPI) handleAction(w http.ResponseWriter, id string, action func(string) error) {
	if err := action(id); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "ok"})
}

func (a *ScheduleAPI) fillDefaults(req *ScheduleRequest) {
	if strings.TrimSpace(req.Channel) == "" {
		req.Channel = a.defaultChannel
	}
	if strings.TrimSpace(req.AgentID) == "" {
		req.AgentID = a.defaultAgent
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid json: %w", err))
		return err
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
