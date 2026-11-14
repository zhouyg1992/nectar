package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
	Parameters  *apiParameters  `json:"parameters"`
}

type apiParameters struct {
	RetryAfter int `json:"retry_after"`
}

type apiError struct {
	Method      string
	Code        int
	Description string
	RetryAfter  int
}

func (e *apiError) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Method, e.Code, e.Description)
}

func (a *Adapter) getMe(ctx context.Context) (tgUser, error) {
	var user tgUser
	if err := a.call(ctx, "getMe", nil, &user); err != nil {
		return tgUser{}, err
	}
	return user, nil
}

func (a *Adapter) getUpdates(ctx context.Context, offset int64) ([]update, error) {
	payload := map[string]any{
		"offset":          offset,
		"timeout":         pollTimeout,
		"allowed_updates": []string{"message"},
	}
	var updates []update
	if err := a.call(ctx, "getUpdates", payload, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

func (a *Adapter) call(ctx context.Context, method string, payload any, result any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode telegram %s request: %w", method, err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.apiURL(method), body)
	if err != nil {
		return fmt.Errorf("create telegram %s request: %w", method, a.redactError(err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "nectar")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call telegram %s: %w", method, a.redactError(err))
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read telegram %s response: %w", method, err)
	}

	var envelope apiResponse
	if err := json.Unmarshal(data, &envelope); err != nil {
		if resp.StatusCode >= http.StatusBadRequest {
			return &apiError{
				Method:      method,
				Code:        resp.StatusCode,
				Description: http.StatusText(resp.StatusCode),
			}
		}
		return fmt.Errorf(
			"decode telegram %s response (status %d): %w",
			method, resp.StatusCode, err,
		)
	}
	if !envelope.OK {
		code := envelope.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		description := strings.TrimSpace(envelope.Description)
		if description == "" {
			description = http.StatusText(code)
		}
		apiErr := &apiError{
			Method:      method,
			Code:        code,
			Description: description,
		}
		if envelope.Parameters != nil {
			apiErr.RetryAfter = envelope.Parameters.RetryAfter
		}
		return apiErr
	}
	if result == nil || len(envelope.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("decode telegram %s result: %w", method, err)
	}
	return nil
}

func (a *Adapter) apiURL(method string) string {
	return a.apiBaseURL + "/bot" + a.token + "/" + method
}

func (a *Adapter) fileURL(path string) string {
	return a.apiBaseURL + "/file/bot" + a.token + "/" + strings.TrimLeft(path, "/")
}

func (a *Adapter) redactError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if a.token != "" {
		message = strings.ReplaceAll(message, a.token, "<redacted>")
	}
	return errors.New(message)
}
