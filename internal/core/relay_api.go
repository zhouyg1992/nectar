package core

import (
	"errors"
	"net/http"
	"strings"
)

func (a *ScheduleAPI) handleRelayBots(w http.ResponseWriter, r *http.Request) {
	if a.relay == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("relay is disabled"))
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bots": a.relay.Bots()})
}

func (a *ScheduleAPI) handleRelaySend(w http.ResponseWriter, r *http.Request) {
	if a.relay == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("relay is disabled"))
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	var req RelaySendRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	env, duplicate, err := a.relay.Send(r.Context(), relayTokenFromRequest(r), req)
	if err != nil {
		writeJSON(w, a.relay.SendRequestError(err), map[string]string{
			"error": err.Error(),
			"code":  relayErrorCode(err),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"envelope":  env,
		"duplicate": duplicate,
	})
}

func (a *ScheduleAPI) handleRelayRoots(w http.ResponseWriter, r *http.Request) {
	if a.relay == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("relay is disabled"))
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roots": a.relay.Roots()})
}

func (a *ScheduleAPI) handleRelayRootItem(w http.ResponseWriter, r *http.Request) {
	if a.relay == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("relay is disabled"))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/relay/roots/")
	if strings.HasSuffix(path, "/cancel") {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		rootID := strings.TrimSuffix(path, "/cancel")
		if err := a.relay.CancelRoot(rootID); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"root": rootID, "status": "canceled"})
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	root, envelopes, ok := a.relay.Root(path)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("relay root not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"root": root, "envelopes": envelopes})
}

func relayTokenFromRequest(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) >= 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return strings.TrimSpace(r.Header.Get("X-Nectar-Relay-Token"))
}
