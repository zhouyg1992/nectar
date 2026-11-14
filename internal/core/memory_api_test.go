package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhouyg1992/nectar/internal/memory"
)

func TestMemoryAPIAddListSearchPatchDelete(t *testing.T) {
	store, err := memory.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := NewScheduleAPI(ScheduleAPIOptions{Memory: store})
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	item := memory.Memory{
		Kind: memory.KindProfile, Scope: memory.ScopeChat,
		ScopeID: memory.ChatScope("feishu", "c1"), Content: "以后默认用中文回复",
		Status: memory.StatusActive,
	}
	data, _ := json.Marshal(item)
	resp, err := http.Post(server.URL+"/memory", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %s", resp.Status)
	}
	var created memory.Memory
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("created memory has no id")
	}

	resp, err = http.Get(server.URL + "/memory?q=" + "中文")
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Memories []memory.Memory `json:"memories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(listed.Memories) != 1 {
		t.Fatalf("search results = %#v", listed.Memories)
	}

	patch, _ := json.Marshal(map[string]any{"status": memory.StatusCandidate})
	req, _ := http.NewRequest(http.MethodPatch, server.URL+"/memory/"+created.ID, bytes.NewReader(patch))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %s", resp.Status)
	}

	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/memory/"+created.ID+"?confirm=1", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %s", resp.Status)
	}
	if _, ok, err := store.Get(t.Context(), created.ID); err != nil || ok {
		t.Fatalf("deleted memory visible: ok=%v err=%v", ok, err)
	}
}

func TestMemoryAPIGroupNonAdminBecomesCandidate(t *testing.T) {
	store, err := memory.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := NewScheduleAPI(ScheduleAPIOptions{
		Memory:     store,
		AdminUsers: []string{"admin-1"},
	})
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	item := memory.Memory{
		Kind: memory.KindFact, Scope: memory.ScopeChat,
		ScopeID: memory.ChatScope("feishu", "group-1"), Content: "群里的约定",
		Status: memory.StatusActive,
	}
	data, _ := json.Marshal(item)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/memory", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Nectar-User-ID", "guest-1")
	req.Header.Set("X-Nectar-Chat-Type", "group")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %s", resp.Status)
	}
	var created memory.Memory
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Status != memory.StatusCandidate {
		t.Fatalf("group memory status = %q", created.Status)
	}

	patch, _ := json.Marshal(map[string]any{"status": memory.StatusActive})
	req, _ = http.NewRequest(http.MethodPatch, server.URL+"/memory/"+created.ID, bytes.NewReader(patch))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Nectar-User-ID", "guest-1")
	req.Header.Set("X-Nectar-Chat-Type", "group")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("guest patch status = %s", resp.Status)
	}
}
