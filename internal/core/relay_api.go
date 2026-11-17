package core

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// handleRelayBots returns the roster. Read-only, so an operator CLI may call it
// without a run token; the socket's 0600 mode is the access boundary.
func (a *ScheduleAPI) handleRelayBots(w http.ResponseWriter, r *http.Request) {
	if !a.requireRelay(w) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	peers := a.relay.Peers(strings.TrimSpace(r.URL.Query().Get("chat_id")))
	writeJSON(w, http.StatusOK, map[string]any{"bots": peers})
}

// handleRelaySend accepts one relay message. The sender identity comes from the
// bearer run token; body fields like from/gen/chat are ignored by design.
func (a *ScheduleAPI) handleRelaySend(w http.ResponseWriter, r *http.Request) {
	if !a.requireRelay(w) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	binding := a.relayBinding(r)
	if binding == nil {
		writeRelayError(w, relayError(ErrRelayUnauthorized, "missing or expired relay token; relay send only works inside a Nectar agent run"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, relayMaxBodyBytes+4096)
	var req RelaySendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRelayError(w, relayError(ErrRelayRejected, "decode relay request: %v", err))
		return
	}
	envelope, err := a.relay.Send(binding, req)
	if err != nil {
		writeRelayError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, envelope)
}

// handleRelayRoots lists relay roots, optionally filtered by chat_id.
func (a *ScheduleAPI) handleRelayRoots(w http.ResponseWriter, r *http.Request) {
	if !a.requireRelay(w) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	chatID := strings.TrimSpace(r.URL.Query().Get("chat_id"))
	var roots []*RelayRoot
	if chatID != "" {
		roots = a.relay.RootsForChat(chatID)
	} else {
		roots = a.relay.Roots()
	}
	writeJSON(w, http.StatusOK, map[string]any{"roots": roots})
}

// handleRelayRootItem serves /relay/roots/{id} and /relay/roots/{id}/cancel.
func (a *ScheduleAPI) handleRelayRootItem(w http.ResponseWriter, r *http.Request) {
	if !a.requireRelay(w) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/relay/roots/")
	path = strings.Trim(path, "/")
	if path == "" {
		writeError(w, http.StatusNotFound, errors.New("relay root id is required"))
		return
	}
	id := path
	cancel := false
	if strings.HasSuffix(path, "/cancel") {
		id = strings.TrimSuffix(path, "/cancel")
		cancel = true
	}
	if cancel {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		if err := a.relay.CancelRoot(id); err != nil {
			writeRelayError(w, err)
			return
		}
		if root := a.relay.Root(id); root != nil {
			writeJSON(w, http.StatusOK, root)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	root := a.relay.Root(id)
	if root == nil {
		writeRelayError(w, relayError(ErrRelayTargetUnknown, "unknown relay root %q", id))
		return
	}
	writeJSON(w, http.StatusOK, root)
}

func (a *ScheduleAPI) requireRelay(w http.ResponseWriter) bool {
	if a.relay == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("relay is disabled"))
		return false
	}
	return true
}

func (a *ScheduleAPI) relayBinding(r *http.Request) *relayTokenBinding {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return nil
	}
	return a.relay.TokenBinding(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
}

func writeRelayError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if code := RelayErrorCode(err); code != "" {
		switch code {
		case ErrRelayUnauthorized:
			status = http.StatusUnauthorized
		case ErrRelayTargetUnknown:
			status = http.StatusNotFound
		case ErrRelayRejected, ErrRelayNotInChat:
			status = http.StatusForbidden
		case ErrRelayMutual:
			status = http.StatusConflict
		case ErrRelayDisabled:
			status = http.StatusServiceUnavailable
		}
	}
	var relayErr *RelayError
	if errors.As(err, &relayErr) {
		writeJSON(w, status, relayErr)
		return
	}
	writeJSON(w, status, map[string]string{"code": "ERR_RELAY", "message": err.Error()})
}
