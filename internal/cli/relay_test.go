package cli

import (
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

func TestRelaySendWiresTokenAndBody(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "api.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	type captured struct {
		Authorization string
		To            string
		Body          string
		NoReply       bool
		Idempotency   string
	}
	requests := make(chan captured, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/relay/send", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			To             string `json:"to"`
			Body           string `json:"body"`
			NoReply        bool   `json:"no_reply"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		requests <- captured{
			Authorization: first(r.Header["Authorization"]),
			To:            body.To,
			Body:          body.Body,
			NoReply:       body.NoReply,
			Idempotency:   body.IdempotencyKey,
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "env_1", "to": body.To, "kind": "handoff", "gen": 1, "state": "pending",
		})
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	t.Setenv("NECTAR_API_SOCKET", socket)
	t.Setenv("NECTAR_RELAY_TOKEN", "tok-123")
	if err := runRelay([]string{"send", "--to", "reviewer", "--no-reply", "--idempotency-key", "k1", "review", "this"}); err != nil {
		t.Fatal(err)
	}
	got := <-requests
	if got.Authorization != "Bearer tok-123" {
		t.Fatalf("authorization = %q", got.Authorization)
	}
	if got.To != "reviewer" || got.Body != "review this" || !got.NoReply || got.Idempotency != "k1" {
		t.Fatalf("request = %#v", got)
	}
}

func TestRelayCLISurfacesErrorCodes(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "api.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/relay/send", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "ERR_GEN_LIMIT", "message": "too deep"})
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	t.Setenv("NECTAR_API_SOCKET", socket)
	t.Setenv("NECTAR_RELAY_TOKEN", "tok-123")
	err = runRelay([]string{"send", "--to", "reviewer", "brief"})
	if err == nil {
		t.Fatal("expected an error")
	}
	relayErr, ok := err.(*relayCLIError)
	if !ok || relayErr.Code != "ERR_GEN_LIMIT" {
		t.Fatalf("error = %#v", err)
	}
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
