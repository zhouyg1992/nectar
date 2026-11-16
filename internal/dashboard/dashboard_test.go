package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/core"
	"github.com/zhouyg1992/nectar/internal/memory"
)

var testToken = strings.Repeat("t", 32)

func TestDashboardAuthOverviewAndMemory(t *testing.T) {
	server, store := newDashboardTestServer(t, false)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/api/v1/overview")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated overview status = %s", resp.Status)
	}

	resp, err = doBearer(t, http.MethodGet, httpServer.URL+"/api/v1/overview", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview status = %s", resp.Status)
	}
	var overview overviewResponse
	if err := json.NewDecoder(resp.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	if len(overview.Bots) != 1 {
		t.Fatalf("bots = %#v", overview.Bots)
	}
	if got := overview.Bots[0].MemoryCounts["active"]; got != 1 {
		t.Fatalf("active memory count = %d", got)
	}
	if got := overview.Bots[0].MemoryCounts["candidate"]; got != 1 {
		t.Fatalf("candidate memory count = %d", got)
	}
	if got := overview.Bots[0].ScheduleCounts["active"]; got != 1 {
		t.Fatalf("active schedule count = %d", got)
	}

	resp, err = doBearer(t, http.MethodGet, httpServer.URL+"/api/v1/bots/default/memories?status=all", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("memory list status = %s", resp.Status)
	}
	var listed struct {
		Memories []memory.Memory `json:"memories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Memories) != 2 {
		t.Fatalf("memories = %#v", listed.Memories)
	}
	if _, _, err := store.Get(context.Background(), listed.Memories[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardCookieCSRFAndReadOnly(t *testing.T) {
	server, store := newDashboardTestServer(t, false)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(httpServer.URL + "/login?token=" + testToken)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login status = %s", resp.Status)
	}
	cookies := jar.Cookies(mustURL(t, httpServer.URL))
	if len(cookies) == 0 {
		t.Fatal("login did not set cookies")
	}

	item := memory.Memory{
		Kind: memory.KindFact, Scope: memory.ScopeBot,
		Content: "dashboard csrf test", Status: memory.StatusCandidate,
	}
	created, err := store.Put(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"content": "changed"})
	resp, err = client.Do(mustRequest(t, http.MethodPatch, httpServer.URL+"/api/v1/bots/default/memories/"+created.ID, body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cookie mutation without csrf status = %s", resp.Status)
	}

	csrf := cookieValue(jar, mustURL(t, httpServer.URL), csrfCookie)
	req := mustRequest(t, http.MethodPatch, httpServer.URL+"/api/v1/bots/default/memories/"+created.ID, body)
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cookie mutation with csrf status = %s", resp.Status)
	}

	readOnly, _ := newDashboardTestServer(t, true)
	readOnlyServer := httptest.NewServer(readOnly.Handler())
	defer readOnlyServer.Close()
	resp, err = doBearer(t, http.MethodPatch, readOnlyServer.URL+"/api/v1/bots/default/memories/"+created.ID, body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only mutation status = %s", resp.Status)
	}
}

func TestDashboardScheduleCRUDAndTargets(t *testing.T) {
	server, _ := newDashboardTestServer(t, false)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := doBearer(t, http.MethodGet, httpServer.URL+"/api/v1/bots/default/targets", nil)
	if err != nil {
		t.Fatal(err)
	}
	var targets struct {
		Targets []targetView `json:"targets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(targets.Targets) != 1 || targets.Targets[0].ChatID != "chat-1" {
		t.Fatalf("targets = %#v", targets.Targets)
	}

	create := map[string]any{
		"mode":     "cron",
		"cron":     "0 9 * * *",
		"prompt":   "new dashboard task",
		"channel":  "feishu",
		"chat_id":  "chat-1",
		"agent_id": "codex",
		"timezone": "UTC",
	}
	body, _ := json.Marshal(create)
	resp, err = doBearer(t, http.MethodPost, httpServer.URL+"/api/v1/bots/default/schedules", body)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Task core.ScheduledTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || created.Task.ID == "" {
		t.Fatalf("create status = %s task = %#v", resp.Status, created.Task)
	}

	resp, err = doBearer(t, http.MethodPost, httpServer.URL+"/api/v1/bots/default/schedules/"+created.Task.ID+"/pause", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pause status = %s", resp.Status)
	}

	resp, err = doBearer(t, http.MethodGet, httpServer.URL+"/api/v1/bots/default/schedules?all=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Tasks []core.ScheduledTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(listed.Tasks) != 2 {
		t.Fatalf("tasks = %#v", listed.Tasks)
	}

	resp, err = doBearer(t, http.MethodDelete, httpServer.URL+"/api/v1/bots/default/schedules/"+created.Task.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %s", resp.Status)
	}
}

func newDashboardTestServer(t *testing.T, readOnly bool) (*Server, memory.Store) {
	t.Helper()
	dataDir := t.TempDir()
	store, err := memory.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []memory.Memory{
		{Kind: memory.KindFact, Scope: memory.ScopeBot, Content: "active fact", Status: memory.StatusActive},
		{Kind: memory.KindLesson, Scope: memory.ScopeBot, Content: "pending lesson", Status: memory.StatusCandidate},
	} {
		if _, err := store.Put(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}

	anchors, err := core.NewAnchorStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	key := core.SessionKey{BotID: "default", Channel: "feishu", ChatID: "chat-1", AgentID: "codex"}
	if err := anchors.Set(key, core.SessionAnchor{Channel: "feishu", ChatID: "chat-1", ChatType: "group"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	scheduler, err := core.NewScheduler(core.SchedulerOptions{
		StorePath:       filepath.Join(dataDir, "schedules.json"),
		Anchors:         anchors,
		DefaultTimezone: "UTC",
		Now:             func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.Add(core.ScheduleRequest{
		Mode: "cron", Cron: "0 8 * * *", Prompt: "existing task",
		Channel: "feishu", ChatID: "chat-1", BotID: "default", AgentID: "codex", Timezone: "UTC",
	}, "test"); err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{
		Listen:   "127.0.0.1:0",
		Token:    testToken,
		Version:  "test",
		ReadOnly: readOnly,
		Bots: []Bot{{
			ID: "default", Name: "Nectar", Platform: "feishu", Engine: "codex", Model: "gpt-test",
			Workspace: dataDir, DataDir: dataDir, WorkspaceMode: "shared",
			MemoryEnabled: true, MemoryDefaultScope: "bot", MemoryMaxItems: 6, MemoryMaxChars: 1500,
			ScheduleEnabled: true, ScheduleTimezone: "UTC", ScheduleHistoryLimit: 100,
			ScheduleMaxConcurrent: 4, ScheduleJitter: "0s", ScheduleTimeout: "30m",
			DefaultChannel: "feishu", DefaultAgent: "codex",
			Memory: store, Scheduler: scheduler, Anchors: anchors,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return server, store
}

func doBearer(t *testing.T, method, url string, body []byte) (*http.Response, error) {
	t.Helper()
	req := mustRequest(t, method, url, body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	return http.DefaultClient.Do(req)
}

func mustRequest(t *testing.T, method, url string, body []byte) *http.Request {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func cookieValue(jar http.CookieJar, page *url.URL, name string) string {
	for _, cookie := range jar.Cookies(page) {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

func TestDashboardLoginPageDoesNotLeakToken(t *testing.T) {
	server, _ := newDashboardTestServer(t, false)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login status = %s", resp.Status)
	}
	if strings.Contains(resp.Header.Get("Set-Cookie"), testToken) {
		t.Fatal("login response leaked the dashboard token")
	}
}

func TestDashboardServesEmbeddedUI(t *testing.T) {
	server, _ := newDashboardTestServer(t, false)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	req := mustRequest(t, http.MethodGet, httpServer.URL+"/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testToken})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "<title>Nectar Console</title>") {
		t.Fatalf("index status = %s body = %q", resp.Status, string(body))
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatalf("missing CSP: %q", resp.Header.Get("Content-Security-Policy"))
	}

	resp, err = http.Get(httpServer.URL + "/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	script, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(script), "loadOverview") {
		t.Fatalf("app.js status = %s", resp.Status)
	}
}
