package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/core"
	"github.com/zhouyg1992/nectar/internal/memory"
)

//go:embed assets/*
var assetsFS embed.FS

const (
	sessionCookie = "nectar_dashboard"
	csrfCookie    = "nectar_csrf"
)

// Bot is the dashboard-facing view of one running Nectar bot.
type Bot struct {
	ID            string
	Name          string
	Platform      string
	Engine        string
	Model         string
	Workspace     string
	DataDir       string
	WorkspaceMode string

	AdminUsers   []string
	AllowedChats []string

	MemoryEnabled           bool
	MemoryDefaultScope      string
	MemoryMaxItems          int
	MemoryMaxChars          int
	MemoryAutoLearnFailures bool

	ScheduleEnabled       bool
	ScheduleTimezone      string
	ScheduleHistoryLimit  int
	ScheduleMaxConcurrent int
	ScheduleJitter        string
	ScheduleTimeout       string

	DefaultChannel string
	DefaultAgent   string

	Memory    memory.Store
	Scheduler *core.Scheduler
	Anchors   *core.AnchorStore
}

// Options configures the local dashboard server.
type Options struct {
	Listen      string
	Token       string
	TokenPath   string
	ConfigPath  string
	Version     string
	ReadOnly    bool
	AllowRemote bool
	Logger      *slog.Logger
	StartedAt   time.Time
	Bots        []Bot
}

// Server is the optional local web console. It is intentionally separate from
// the platform adapters and only talks to bot state through the same stores and
// scheduler used by chat and CLI commands.
type Server struct {
	listen      string
	token       string
	tokenPath   string
	configPath  string
	version     string
	readOnly    bool
	allowRemote bool
	logger      *slog.Logger
	startedAt   time.Time

	botOrder []string
	bots     map[string]Bot

	server   *http.Server
	listener net.Listener
}

// New creates a dashboard server. A token is loaded from TokenPath (or
// generated when the file is missing) unless an explicit Token is supplied.
func New(opts Options) (*Server, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	listen := strings.TrimSpace(opts.Listen)
	if listen == "" {
		listen = "127.0.0.1:8787"
	}
	token := strings.TrimSpace(opts.Token)
	if token == "" {
		var err error
		token, err = EnsureToken(opts.TokenPath)
		if err != nil {
			return nil, err
		}
	}
	if len(token) < 16 {
		return nil, errors.New("dashboard token must be at least 16 characters")
	}

	startedAt := opts.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	s := &Server{
		listen:      listen,
		token:       token,
		tokenPath:   opts.TokenPath,
		configPath:  opts.ConfigPath,
		version:     opts.Version,
		readOnly:    opts.ReadOnly,
		allowRemote: opts.AllowRemote,
		logger:      logger,
		startedAt:   startedAt,
		bots:        make(map[string]Bot, len(opts.Bots)),
	}
	for _, bot := range opts.Bots {
		id := strings.TrimSpace(bot.ID)
		if id == "" {
			return nil, errors.New("dashboard bot id is empty")
		}
		if _, exists := s.bots[id]; exists {
			return nil, fmt.Errorf("duplicate dashboard bot id %q", id)
		}
		bot.ID = id
		s.bots[id] = bot
		s.botOrder = append(s.botOrder, id)
	}
	sort.Strings(s.botOrder)
	if len(s.bots) == 0 {
		return nil, errors.New("dashboard requires at least one bot")
	}
	if !s.allowRemote {
		host, _, err := net.SplitHostPort(s.listen)
		if err != nil {
			return nil, fmt.Errorf("invalid dashboard listen address: %w", err)
		}
		if !isLoopbackHost(host) {
			return nil, fmt.Errorf("dashboard listen %q is not loopback; set allow_remote", s.listen)
		}
	}
	return s, nil
}

// Start begins serving the dashboard on its configured TCP listener.
func (s *Server) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.listen)
	if err != nil {
		return fmt.Errorf("listen dashboard on %s: %w", s.listen, err)
	}
	if !s.allowRemote {
		if tcp, ok := listener.Addr().(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
			_ = listener.Close()
			return fmt.Errorf("dashboard listener %s is not loopback", listener.Addr())
		}
	}
	s.listener = listener
	s.server = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("dashboard stopped", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()
	s.logger.Info("dashboard listening", "listen", s.Addr(), "read_only", s.readOnly, "bots", len(s.bots))
	return nil
}

func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.listen
}

func (s *Server) Close() error {
	var err error
	if s.server != nil {
		err = s.server.Close()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	return err
}

// Handler returns the dashboard HTTP handler. It is exported for tests and for
// callers that want to mount the console behind an existing listener.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(assetsFS, "assets")

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": s.version})
	})
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("GET /logout", s.handleLogout)
	mux.Handle("GET /assets/", noStore(http.StripPrefix("/assets/", http.FileServer(http.FS(assets)))))
	mux.HandleFunc("GET /", s.handleIndex)

	mux.HandleFunc("GET /api/v1/overview", s.authAPI(s.handleOverview))
	mux.HandleFunc("GET /api/v1/bots", s.authAPI(s.handleBots))
	mux.HandleFunc("GET /api/v1/bots/{id}", s.authAPI(s.handleBot))
	mux.HandleFunc("GET /api/v1/bots/{id}/settings", s.authAPI(s.handleSettings))
	mux.HandleFunc("GET /api/v1/bots/{id}/targets", s.authAPI(s.handleTargets))

	mux.HandleFunc("GET /api/v1/bots/{id}/memories", s.authAPI(s.handleMemories))
	mux.HandleFunc("PATCH /api/v1/bots/{id}/memories/{memoryID}", s.mutation(s.handleMemoryPatch))
	mux.HandleFunc("DELETE /api/v1/bots/{id}/memories/{memoryID}", s.mutation(s.handleMemoryDelete))

	mux.HandleFunc("GET /api/v1/bots/{id}/schedules", s.authAPI(s.handleSchedules))
	mux.HandleFunc("POST /api/v1/bots/{id}/schedules", s.mutation(s.handleScheduleCreate))
	mux.HandleFunc("POST /api/v1/bots/{id}/schedules/preview", s.authAPI(s.handleSchedulePreview))
	mux.HandleFunc("PATCH /api/v1/bots/{id}/schedules/{taskID}", s.mutation(s.handleScheduleUpdate))
	mux.HandleFunc("DELETE /api/v1/bots/{id}/schedules/{taskID}", s.mutation(s.handleScheduleDelete))
	mux.HandleFunc("POST /api/v1/bots/{id}/schedules/{taskID}/{action}", s.mutation(s.handleScheduleAction))
	return securityHeaders(mux)
}

type authKind int

const (
	authNone authKind = iota
	authCookie
	authBearer
)

func (s *Server) authenticate(r *http.Request) authKind {
	if header := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(header, "Bearer ") {
		if s.validToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))) {
			return authBearer
		}
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil && s.validToken(cookie.Value) {
		return authCookie
	}
	return authNone
}

func (s *Server) validToken(value string) bool {
	if len(value) != len(s.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(s.token)) == 1
}

func (s *Server) authAPI(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authenticate(r) == authNone {
			writeError(w, http.StatusUnauthorized, errors.New("unauthorized"))
			return
		}
		next(w, r)
	}
}

func (s *Server) mutation(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		kind := s.authenticate(r)
		if kind == authNone {
			writeError(w, http.StatusUnauthorized, errors.New("unauthorized"))
			return
		}
		if s.readOnly {
			writeError(w, http.StatusForbidden, errors.New("dashboard is read-only"))
			return
		}
		if kind == authCookie {
			cookie, err := r.Cookie(csrfCookie)
			if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
				writeError(w, http.StatusForbidden, errors.New("invalid csrf token"))
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" || !s.validToken(token) {
		http.Error(w, "链接无效或已过期。请在终端运行 `nectar dashboard` 获取新的登录链接。", http.StatusUnauthorized)
		return
	}
	s.setSessionCookies(w, r, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	for _, name := range []string{sessionCookie, csrfCookie} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: name == sessionCookie,
			SameSite: http.SameSiteStrictMode,
			Secure:   requestIsHTTPS(r),
		})
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" && s.validToken(token) {
		s.setSessionCookies(w, r, token)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if s.authenticate(r) == authNone {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "dashboard asset missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *Server) setSessionCookies(w http.ResponseWriter, r *http.Request, token string) {
	csrf, err := randomHex(24)
	if err != nil {
		csrf = ""
	}
	maxAge := 30 * 24 * 60 * 60
	secure := requestIsHTTPS(r)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookie,
		Value:    csrf,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
	})
}

func randomHex(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) botFromRequest(r *http.Request) (Bot, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	bot, ok := s.bots[id]
	return bot, ok
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
