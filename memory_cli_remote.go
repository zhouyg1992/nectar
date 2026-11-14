package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/zhouyg1992/nectar/internal/memory"
)

type memoryAPIError struct {
	Status  int
	Message string
}

func (e *memoryAPIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("memory api returned HTTP %d", e.Status)
}

// remoteMemoryStore lets the agent-side CLI write through the Nectar daemon
// instead of hitting a sandbox-protected path under ~/.nectar/memory.
type remoteMemoryStore struct {
	socket string
}

func (s *remoteMemoryStore) request(ctx context.Context, method, path string, body any, target any) error {
	var payload *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	} else {
		payload = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://nectar"+path, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, env := range map[string]string{
		"X-Nectar-User-ID":     "NECTAR_USER_ID",
		"X-Nectar-Chat-Type":   "NECTAR_CHAT_TYPE",
		"X-Nectar-Chat-ID":     "NECTAR_CHAT_ID",
		"X-Nectar-Working-Dir": "NECTAR_WORKING_DIR",
		"X-Nectar-Channel":     "NECTAR_CHANNEL",
	} {
		if value := strings.TrimSpace(os.Getenv(env)); value != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := scheduleHTTPClient(s.socket).Do(req)
	if err != nil {
		return fmt.Errorf("memory daemon unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&failure)
		return &memoryAPIError{Status: resp.StatusCode, Message: failure.Error}
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (s *remoteMemoryStore) Put(ctx context.Context, item memory.Memory) (memory.Memory, error) {
	var out memory.Memory
	if err := s.request(ctx, http.MethodPost, "/memory", item, &out); err != nil {
		return memory.Memory{}, err
	}
	return out, nil
}

func (s *remoteMemoryStore) Get(ctx context.Context, id string) (memory.Memory, bool, error) {
	var out memory.Memory
	err := s.request(ctx, http.MethodGet, "/memory/"+url.PathEscape(id), nil, &out)
	if err != nil {
		var apiErr *memoryAPIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return memory.Memory{}, false, nil
		}
		return memory.Memory{}, false, err
	}
	return out, true, nil
}

func (s *remoteMemoryStore) List(ctx context.Context, opts memory.ListOptions) ([]memory.Memory, error) {
	query := url.Values{}
	if opts.Status != "" {
		query.Set("status", string(opts.Status))
	}
	if opts.Scope != "" {
		query.Set("scope", string(opts.Scope))
	}
	if len(opts.ScopeIDs) > 0 {
		query.Set("scope_ids", strings.Join(opts.ScopeIDs, ","))
	}
	var out memoryCLIListResult
	if err := s.request(ctx, http.MethodGet, "/memory?"+query.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out.Memories, nil
}

func (s *remoteMemoryStore) Search(ctx context.Context, opts memory.SearchOptions) ([]memory.Memory, error) {
	query := url.Values{}
	query.Set("q", opts.Query)
	if opts.Scope != "" {
		query.Set("scope", string(opts.Scope))
	}
	if len(opts.ScopeIDs) > 0 {
		query.Set("scope_ids", strings.Join(opts.ScopeIDs, ","))
	}
	if opts.Limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	var out memoryCLIListResult
	if err := s.request(ctx, http.MethodGet, "/memory?"+query.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out.Memories, nil
}

func (s *remoteMemoryStore) SetStatus(ctx context.Context, id string, status memory.Status) error {
	return s.request(ctx, http.MethodPatch, "/memory/"+url.PathEscape(id), map[string]any{"status": status}, nil)
}

func (s *remoteMemoryStore) SetPinned(ctx context.Context, id string, pinned bool) error {
	return s.request(ctx, http.MethodPatch, "/memory/"+url.PathEscape(id), map[string]any{"pinned": pinned}, nil)
}

func (s *remoteMemoryStore) UpdateContent(ctx context.Context, id, content string) (memory.Memory, error) {
	var out memory.Memory
	if err := s.request(ctx, http.MethodPatch, "/memory/"+url.PathEscape(id), map[string]any{"content": content}, &out); err != nil {
		return memory.Memory{}, err
	}
	return out, nil
}

func (s *remoteMemoryStore) Touch(context.Context, []string) error {
	return nil
}

func (s *remoteMemoryStore) Delete(ctx context.Context, id string) error {
	return s.request(ctx, http.MethodDelete, "/memory/"+url.PathEscape(id)+"?confirm=1", nil, nil)
}

func (s *remoteMemoryStore) Counts(ctx context.Context, scopeIDs []string) (map[memory.Status]int, error) {
	items, err := s.List(ctx, memory.ListOptions{ScopeIDs: scopeIDs})
	if err != nil {
		return nil, err
	}
	counts := make(map[memory.Status]int)
	for _, item := range items {
		counts[item.Status]++
	}
	return counts, nil
}

var _ memory.Store = (*remoteMemoryStore)(nil)
