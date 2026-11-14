package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/zhouyg1992/nectar/internal/platform"
)

const (
	defaultAPIBaseURL  = "https://api.telegram.org"
	defaultHTTPTimeout = 65 * time.Second
	pollTimeout        = 50
	oldMessageGrace    = 2 * time.Second
	mediaGroupDelay    = 800 * time.Millisecond
)

type Config struct {
	BotToken     string
	Proxy        string
	AllowedUsers []string
	AllowedChats []string

	// Optional overrides for self-hosted Bot API servers and tests.
	APIBaseURL string
	HTTPClient *http.Client
}

type Adapter struct {
	token        string
	apiBaseURL   string
	httpClient   *http.Client
	allowedUsers map[string]struct{}
	allowedChats map[string]struct{}
	dedup        *messageDedup
	startedAt    time.Time

	botID       int64
	botUsername string

	handler func(platform.Message)

	mediaMu     sync.Mutex
	mediaGroups map[string]*pendingMediaGroup
	stopping    bool

	editMu    sync.Mutex
	lastEdits map[string]time.Time
}

type pendingMediaGroup struct {
	message platform.Message
	timer   *time.Timer
}

func New(cfg Config) (*Adapter, error) {
	token := strings.TrimSpace(cfg.BotToken)
	if token == "" {
		return nil, errors.New("telegram bot_token is required")
	}
	if strings.ContainsAny(token, " \t\r\n") {
		return nil, errors.New("telegram bot_token must not contain whitespace")
	}

	apiBaseURL := strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = defaultAPIBaseURL
	}
	parsedBase, err := url.Parse(apiBaseURL)
	if err != nil || parsedBase.Scheme == "" || parsedBase.Host == "" {
		return nil, fmt.Errorf("telegram api_base_url %q must include scheme and host", apiBaseURL)
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		transport := &http.Transport{}
		if base, ok := http.DefaultTransport.(*http.Transport); ok {
			transport = base.Clone()
		}
		proxy := strings.TrimSpace(cfg.Proxy)
		if proxy == "" {
			transport.Proxy = http.ProxyFromEnvironment
		} else {
			proxyURL, err := url.Parse(proxy)
			if err != nil {
				return nil, errors.New("parse telegram proxy: invalid URL")
			}
			if proxyURL.Scheme == "" || proxyURL.Host == "" {
				return nil, errors.New("telegram proxy must include scheme and host")
			}
			transport.Proxy = http.ProxyURL(proxyURL)
		}
		httpClient = &http.Client{Transport: transport, Timeout: defaultHTTPTimeout}
	}

	return &Adapter{
		token:        token,
		apiBaseURL:   apiBaseURL,
		httpClient:   httpClient,
		allowedUsers: stringSet(cfg.AllowedUsers),
		allowedChats: stringSet(cfg.AllowedChats),
		dedup:        newMessageDedup(2 * time.Minute),
		startedAt:    time.Now(),
		mediaGroups:  make(map[string]*pendingMediaGroup),
		lastEdits:    make(map[string]time.Time),
	}, nil
}

func (a *Adapter) Name() string { return "telegram" }

func (a *Adapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		Streaming:   true,
		Card:        true,
		Media:       true,
		Group:       true,
		MentionOnly: true,
	}
}

// Start blocks until ctx is canceled. Telegram long polling keeps the
// connection open for pollTimeout seconds per request, so no public callback
// URL, TLS certificate, or webhook setup is required.
func (a *Adapter) Start(ctx context.Context, handler func(platform.Message)) error {
	if handler == nil {
		return errors.New("telegram handler is required")
	}
	a.handler = handler
	defer a.stopMediaGroups()

	me, err := a.waitForMe(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("telegram getMe: %w", err)
	}
	a.botID = me.ID
	a.botUsername = me.Username
	slog.Info("telegram: connected", "username", me.Username, "id", me.ID)

	var offset int64
	backoff := time.Second
	for {
		updates, err := a.getUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var apiErr *apiError
			if errors.As(err, &apiErr) {
				switch apiErr.Code {
				case http.StatusUnauthorized:
					return fmt.Errorf("telegram rejected bot token: %s", apiErr.Description)
				case http.StatusConflict:
					return fmt.Errorf(
						"telegram polling conflict: %s; another Nectar instance is polling or a webhook is configured",
						apiErr.Description,
					)
				case http.StatusTooManyRequests:
					if !sleepContext(ctx, retryDelay(apiErr.RetryAfter)) {
						return nil
					}
					continue
				default:
					if apiErr.Code >= 400 && apiErr.Code < 500 {
						return fmt.Errorf("telegram getUpdates rejected: %s", apiErr.Description)
					}
				}
			}
			slog.Warn("telegram: poll failed, retrying", "error", err)
			if !sleepContext(ctx, backoff) {
				return nil
			}
			backoff *= 2
			if backoff > 15*time.Second {
				backoff = 15 * time.Second
			}
			continue
		}

		backoff = time.Second
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			a.dispatchUpdate(update)
		}
	}
}

func (a *Adapter) waitForMe(ctx context.Context) (tgUser, error) {
	backoff := 2 * time.Second
	for {
		me, err := a.getMe(ctx)
		if err == nil {
			return me, nil
		}
		if ctx.Err() != nil {
			return tgUser{}, ctx.Err()
		}
		var apiErr *apiError
		if errors.As(err, &apiErr) && apiErr.Code < 500 && apiErr.Code != http.StatusTooManyRequests {
			return tgUser{}, err
		}
		slog.Warn("telegram: getMe failed, retrying", "error", err)
		if !sleepContext(ctx, backoff) {
			return tgUser{}, ctx.Err()
		}
		backoff *= 2
		if backoff > 15*time.Second {
			backoff = 15 * time.Second
		}
	}
}

func (a *Adapter) dispatchUpdate(update update) {
	if update.Message == nil {
		return
	}
	msg, mediaGroupID, err := a.parseMessage(update.Message)
	if err != nil {
		slog.Debug("telegram: ignoring update", "error", err)
		return
	}
	if !a.allowed(msg) {
		slog.Debug("telegram: message blocked by allowlist", "chat", msg.ChatID, "user", msg.UserID)
		return
	}
	if a.dedup.duplicate(msg.Channel + ":" + msg.ChatID + ":" + msg.MessageID) {
		return
	}
	a.queueMessage(msg, mediaGroupID)
}

func (a *Adapter) queueMessage(msg platform.Message, mediaGroupID string) {
	if mediaGroupID == "" {
		a.handler(msg)
		return
	}

	a.mediaMu.Lock()
	defer a.mediaMu.Unlock()
	if a.stopping {
		return
	}
	key := msg.ChatID + ":" + mediaGroupID
	pending := a.mediaGroups[key]
	if pending == nil {
		pending = &pendingMediaGroup{message: msg}
		a.mediaGroups[key] = pending
		pending.timer = time.AfterFunc(mediaGroupDelay, func() {
			a.flushMediaGroup(key)
		})
		return
	}

	if msg.Text != "" {
		if pending.message.Text == "" {
			pending.message.Text = msg.Text
		} else if !strings.Contains(pending.message.Text, msg.Text) {
			pending.message.Text += "\n" + msg.Text
		}
	}
	pending.message.Attachments = append(pending.message.Attachments, msg.Attachments...)
}

func (a *Adapter) flushMediaGroup(id string) {
	a.mediaMu.Lock()
	pending := a.mediaGroups[id]
	delete(a.mediaGroups, id)
	stopping := a.stopping
	a.mediaMu.Unlock()
	if pending == nil || stopping {
		return
	}
	a.handler(pending.message)
}

func (a *Adapter) stopMediaGroups() {
	a.mediaMu.Lock()
	defer a.mediaMu.Unlock()
	a.stopping = true
	for id, pending := range a.mediaGroups {
		if pending.timer != nil {
			pending.timer.Stop()
		}
		delete(a.mediaGroups, id)
	}
}

func (a *Adapter) allowed(msg platform.Message) bool {
	if len(a.allowedUsers) == 0 && len(a.allowedChats) == 0 {
		return true
	}
	if len(a.allowedUsers) > 0 {
		if msg.UserID == "" || !containsID(a.allowedUsers, msg.UserID) {
			return false
		}
	}
	if msg.ChatType == "group" && len(a.allowedChats) > 0 && !containsID(a.allowedChats, msg.ChatID) {
		return false
	}
	return true
}

func stringSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

func containsID(set map[string]struct{}, value string) bool {
	_, ok := set[value]
	return ok
}

func retryDelay(seconds int) time.Duration {
	if seconds <= 0 {
		return time.Second
	}
	if seconds > 60 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}

func sleepContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type messageDedup struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

func newMessageDedup(ttl time.Duration) *messageDedup {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &messageDedup{seen: make(map[string]time.Time), ttl: ttl}
}

func (d *messageDedup) duplicate(id string) bool {
	if id == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	for key, at := range d.seen {
		if now.Sub(at) > d.ttl {
			delete(d.seen, key)
		}
	}
	if _, ok := d.seen[id]; ok {
		return true
	}
	d.seen[id] = now
	return false
}
