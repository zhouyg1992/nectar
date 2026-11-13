package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	dingtalkClient "github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/payload"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/utils"

	"github.com/zhouyg1992/nectar/internal/platform"
)

const (
	defaultAPIBaseURL  = "https://api.dingtalk.com"
	defaultHTTPTimeout = 30 * time.Second
	cardDegradeWindow  = 30 * time.Minute
)

type Config struct {
	ClientID     string
	ClientSecret string
	RobotCode    string
	LogLevel     string
	AllowedUsers []string
	AllowedChats []string
	CardEnabled  *bool

	// Optional overrides for tests and self-hosted gateways.
	APIBaseURL string
	HTTPClient *http.Client
}

type Adapter struct {
	cfg          Config
	clientID     string
	clientSecret string
	robotCode    string
	apiBaseURL   string
	httpClient   *http.Client
	allowedUsers map[string]struct{}
	allowedChats map[string]struct{}
	dedup        *messageDedup

	cardEnabled       atomic.Bool
	cardDegradedUntil atomic.Int64

	tokenMu     sync.Mutex
	accessToken string
	tokenExpiry time.Time

	cardMu sync.Mutex
	cards  map[string]*cardState

	msgCh     chan platform.Message
	handler   func(platform.Message)
	stream    *dingtalkClient.StreamClient
	stopOnce  sync.Once
	streamCtx context.Context
}

func New(cfg Config) (*Adapter, error) {
	clientID := strings.TrimSpace(cfg.ClientID)
	clientSecret := strings.TrimSpace(cfg.ClientSecret)
	if clientID == "" || clientSecret == "" {
		return nil, errors.New("dingtalk client_id and client_secret are required")
	}
	robotCode := strings.TrimSpace(cfg.RobotCode)
	if robotCode == "" {
		robotCode = clientID
	}
	apiBaseURL := strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = defaultAPIBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	cardEnabled := true
	if cfg.CardEnabled != nil {
		cardEnabled = *cfg.CardEnabled
	}

	a := &Adapter{
		cfg:          cfg,
		clientID:     clientID,
		clientSecret: clientSecret,
		robotCode:    robotCode,
		apiBaseURL:   apiBaseURL,
		httpClient:   httpClient,
		allowedUsers: stringSet(cfg.AllowedUsers),
		allowedChats: stringSet(cfg.AllowedChats),
		dedup:        newMessageDedup(60 * time.Second),
		cards:        make(map[string]*cardState),
	}
	a.cardEnabled.Store(cardEnabled)
	return a, nil
}

func (a *Adapter) Name() string { return "dingtalk" }

func (a *Adapter) Capabilities() platform.Capabilities {
	cards := a.cardAvailable()
	return platform.Capabilities{
		Streaming:   cards,
		Card:        cards,
		Media:       true,
		Group:       true,
		MentionOnly: true,
	}
}

// Start blocks until ctx is canceled. The DingTalk SDK runs its own
// reconnect loop after the initial connection succeeds.
func (a *Adapter) Start(ctx context.Context, handler func(platform.Message)) error {
	if handler == nil {
		return errors.New("dingtalk handler is required")
	}
	a.handler = handler
	a.msgCh = make(chan platform.Message, 128)
	a.streamCtx = ctx

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.dispatch(ctx)
	}()

	client := dingtalkClient.NewStreamClient(
		dingtalkClient.WithAppCredential(
			dingtalkClient.NewAppCredentialConfig(a.clientID, a.clientSecret),
		),
	)
	a.stream = client
	client.RegisterRouter(
		utils.SubscriptionTypeKCallback,
		payload.BotMessageCallbackTopic,
		func(_ context.Context, df *payload.DataFrame) (*payload.DataFrameResponse, error) {
			a.enqueueRaw(df.Data)
			return payload.NewSuccessDataFrameResponse(), nil
		},
	)

	for {
		if err := client.Start(ctx); err != nil {
			if ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			slog.Warn("dingtalk: stream connect failed, retrying", "error", err)
			select {
			case <-ctx.Done():
				wg.Wait()
				return nil
			case <-time.After(3 * time.Second):
			}
			continue
		}
		break
	}

	slog.Info("dingtalk: stream connected", "client_id", a.clientID)
	<-ctx.Done()
	a.stopOnce.Do(func() {
		if a.stream != nil {
			a.stream.Close()
		}
	})
	wg.Wait()
	return nil
}

func (a *Adapter) dispatch(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-a.msgCh:
			a.handler(msg)
		}
	}
}

func (a *Adapter) enqueueRaw(raw string) {
	msg, err := a.parseCallback(raw)
	if err != nil {
		slog.Debug("dingtalk: ignoring callback", "error", err)
		return
	}
	if !a.allowed(msg) {
		slog.Debug("dingtalk: message from unauthorized user or chat", "user", msg.UserID, "chat", msg.ChatID)
		return
	}
	if a.dedup.duplicate(msg.MessageID) {
		slog.Debug("dingtalk: duplicate message ignored", "msg_id", msg.MessageID)
		return
	}
	select {
	case <-a.streamCtx.Done():
		return
	case a.msgCh <- msg:
	default:
		slog.Warn("dingtalk: inbound queue full, dropping message", "msg_id", msg.MessageID)
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
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func containsID(set map[string]struct{}, id string) bool {
	if _, ok := set["*"]; ok {
		return true
	}
	_, ok := set[id]
	return ok
}

func (a *Adapter) cardAvailable() bool {
	if !a.cardEnabled.Load() {
		return false
	}
	until := a.cardDegradedUntil.Load()
	return until == 0 || time.Now().UnixNano() >= until
}

func (a *Adapter) markCardDegraded(reason string) {
	until := time.Now().Add(cardDegradeWindow)
	if a.cardDegradedUntil.Swap(until.UnixNano()) == 0 {
		slog.Warn("dingtalk: AI card temporarily degraded", "reason", reason, "until", until.Format(time.RFC3339))
	}
}

func (a *Adapter) apiURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return a.apiBaseURL + path
}

func (a *Adapter) requestJSON(ctx context.Context, method, url string, payload any, token string, out any) ([]byte, int, error) {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("encode request: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("x-acs-dingtalk-access-token", token)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := readLimited(resp.Body, 256<<10)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return respBody, resp.StatusCode, fmt.Errorf("decode response: %w", err)
		}
	}
	return respBody, resp.StatusCode, nil
}

func (a *Adapter) getAccessToken(ctx context.Context) (string, error) {
	a.tokenMu.Lock()
	defer a.tokenMu.Unlock()
	if a.accessToken != "" && time.Now().Before(a.tokenExpiry) {
		return a.accessToken, nil
	}

	var resp struct {
		AccessToken string `json:"accessToken"`
		ExpireIn    int    `json:"expireIn"`
	}
	body, status, err := a.requestJSON(ctx, http.MethodPost, a.apiURL("/v1.0/oauth2/accessToken"), map[string]string{
		"appKey":    a.clientID,
		"appSecret": a.clientSecret,
	}, "", &resp)
	if err != nil {
		return "", fmt.Errorf("request access token: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("access token rejected: status=%d body=%s", status, truncateBytes(body, 512))
	}
	if resp.AccessToken == "" {
		return "", errors.New("access token response is empty")
	}
	expireIn := resp.ExpireIn
	if expireIn <= 0 {
		expireIn = 7200
	}
	if expireIn > 300 {
		expireIn -= 300
	}
	a.accessToken = resp.AccessToken
	a.tokenExpiry = time.Now().Add(time.Duration(expireIn) * time.Second)
	return a.accessToken, nil
}

func truncateBytes(body []byte, max int) string {
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + "…"
}
