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
	"strings"
	"time"
)

type ScheduleAPIOptions struct {
	SocketPath     string
	Scheduler      *Scheduler
	Logger         *slog.Logger
	DefaultChannel string
	DefaultAgent   string
	AdminUsers     []string
}

// ScheduleAPI exposes the scheduler to the local CLI over a Unix socket. The
// socket is chmod 0600 and lives under the data directory, so filesystem
// permissions are the authentication boundary.
type ScheduleAPI struct {
	socketPath     string
	scheduler      *Scheduler
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
		logger:         logger,
		defaultChannel: opts.DefaultChannel,
		defaultAgent:   opts.DefaultAgent,
		adminUsers:     opts.AdminUsers,
	}
}

func (a *ScheduleAPI) Start(ctx context.Context) error {
	if a.scheduler == nil {
		return errors.New("schedule api: scheduler is nil")
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
	return mux
}

func (a *ScheduleAPI) handleCollection(w http.ResponseWriter, r *http.Request) {
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
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method or action not allowed"))
	}
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
