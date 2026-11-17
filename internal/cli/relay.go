package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type relayCLIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *relayCLIError) Error() string {
	if e.Code != "" && e.Message != "" {
		return e.Code + ": " + e.Message
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

type relayCLIBot struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role,omitempty"`
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Busy         bool     `json:"busy"`
}

type relayCLIEnvelope struct {
	ID      string `json:"id"`
	RootID  string `json:"root_id"`
	From    string `json:"from"`
	To      string `json:"to"`
	Gen     int    `json:"gen"`
	Kind    string `json:"kind"`
	State   string `json:"state"`
	Body    string `json:"body"`
	Error   string `json:"error,omitempty"`
	Created string `json:"created_at,omitempty"`
}

type relayCLIRoot struct {
	ID            string             `json:"id"`
	OriginBot     string             `json:"origin_bot"`
	Channel       string             `json:"channel"`
	ChatID        string             `json:"chat_id"`
	State         string             `json:"state"`
	CreatedAt     string             `json:"created_at"`
	EnvelopeCount int                `json:"envelope_count"`
	RunCount      int                `json:"run_count"`
	Envelopes     []relayCLIEnvelope `json:"envelopes,omitempty"`
}

func runRelay(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printRelayUsage()
		return nil
	}
	command := args[0]
	args = args[1:]
	switch command {
	case "bots":
		return relayBots(args)
	case "send":
		return relaySend(args)
	case "roots", "ls":
		return relayRoots(args)
	case "show":
		return relayShow(args)
	case "cancel", "stop":
		return relayCancel(args)
	default:
		printRelayUsage()
		return fmt.Errorf("unknown relay command %q", command)
	}
}

func printRelayUsage() {
	fmt.Print(`Usage:
  nectar relay bots [--json] [--bot ID] [--config PATH] [--socket PATH]
  nectar relay send --to BOT [--no-reply] [--idempotency-key K] [--json] -- BRIEF
  nectar relay roots [--json] [--bot ID] [--config PATH] [--socket PATH]
  nectar relay show ROOT [--json] [--bot ID] [--config PATH] [--socket PATH]
  nectar relay cancel ROOT [--bot ID] [--config PATH] [--socket PATH]

relay send needs NECTAR_RELAY_TOKEN, which Nectar injects into agent runs.
`)
}

func relayBots(args []string) error {
	fs := flag.NewFlagSet("relay bots", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config.toml")
	botID := fs.String("bot", "", "bot id whose local API to query")
	socket := fs.String("socket", "", "Nectar API socket path")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	path, _, _, err := resolveScheduleSocket(*socket, *configPath, *botID)
	if err != nil {
		return err
	}
	var response struct {
		Bots []relayCLIBot `json:"bots"`
	}
	if err := relayRequest(path, http.MethodGet, "/relay/bots", nil, &response); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(response.Bots)
	}
	if len(response.Bots) == 0 {
		fmt.Println("No relay peers registered.")
		return nil
	}
	for _, bot := range response.Bots {
		line := "- " + bot.ID
		if bot.Name != "" && bot.Name != bot.ID {
			line += " (" + bot.Name + ")"
		}
		if bot.Role != "" {
			line += " — " + bot.Role
		}
		if bot.Description != "" {
			line += ": " + bot.Description
		}
		if bot.Busy {
			line += " [busy]"
		}
		fmt.Println(line)
	}
	return nil
}

func relaySend(args []string) error {
	fs := flag.NewFlagSet("relay send", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config.toml")
	botID := fs.String("bot", "", "bot id whose local API to query")
	socket := fs.String("socket", "", "Nectar API socket path")
	to := fs.String("to", "", "target bot id")
	noReply := fs.Bool("no-reply", false, "hand off without an automatic reply")
	idempotencyKey := fs.String("idempotency-key", "", "dedupe key for retries")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(*to) == "" {
		return errors.New("relay send needs --to BOT")
	}
	body := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if body == "" {
		return errors.New("relay send needs a non-empty brief")
	}
	path, _, _, err := resolveScheduleSocket(*socket, *configPath, *botID)
	if err != nil {
		return err
	}
	request := map[string]any{
		"to":       strings.TrimSpace(*to),
		"body":     body,
		"no_reply": *noReply,
	}
	if key := strings.TrimSpace(*idempotencyKey); key != "" {
		request["idempotency_key"] = key
	}
	var envelope relayCLIEnvelope
	if err := relayRequest(path, http.MethodPost, "/relay/send", request, &envelope); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(envelope)
	}
	mode := "ask"
	if envelope.Kind != "" {
		mode = envelope.Kind
	}
	fmt.Printf("Sent %s to %s (%s, gen %d).\n", envelope.ID, envelope.To, mode, envelope.Gen)
	if mode == "ask" {
		fmt.Println("The target runs asynchronously. Its answer returns to this session as a new turn; end the turn instead of waiting or polling.")
	}
	return nil
}

func relayRoots(args []string) error {
	fs := flag.NewFlagSet("relay roots", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config.toml")
	botID := fs.String("bot", "", "bot id whose local API to query")
	socket := fs.String("socket", "", "Nectar API socket path")
	asJSON := fs.Bool("json", false, "print JSON")
	chatID := fs.String("chat", "", "filter by chat id")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	path, _, _, err := resolveScheduleSocket(*socket, *configPath, *botID)
	if err != nil {
		return err
	}
	requestPath := "/relay/roots"
	if value := strings.TrimSpace(*chatID); value != "" {
		requestPath += "?chat_id=" + value
	}
	var response struct {
		Roots []relayCLIRoot `json:"roots"`
	}
	if err := relayRequest(path, http.MethodGet, requestPath, nil, &response); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(response.Roots)
	}
	if len(response.Roots) == 0 {
		fmt.Println("No relay roots.")
		return nil
	}
	for _, root := range response.Roots {
		fmt.Printf("- %s [%s] origin=%s env=%d runs=%d created=%s\n",
			root.ID, root.State, root.OriginBot, root.EnvelopeCount, root.RunCount, root.CreatedAt)
	}
	return nil
}

func relayShow(args []string) error {
	fs := flag.NewFlagSet("relay show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config.toml")
	botID := fs.String("bot", "", "bot id whose local API to query")
	socket := fs.String("socket", "", "Nectar local API socket path")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("relay show needs a root id")
	}
	path, _, _, err := resolveScheduleSocket(*socket, *configPath, *botID)
	if err != nil {
		return err
	}
	var root relayCLIRoot
	if err := relayRequest(path, http.MethodGet, "/relay/roots/"+strings.TrimSpace(fs.Arg(0)), nil, &root); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(root)
	}
	fmt.Printf("%s [%s] origin=%s chat=%s env=%d runs=%d\n", root.ID, root.State, root.OriginBot, root.ChatID, root.EnvelopeCount, root.RunCount)
	for _, envelope := range root.Envelopes {
		summary := envelope.Body
		if index := strings.IndexByte(summary, '\n'); index >= 0 {
			summary = summary[:index]
		}
		if len([]rune(summary)) > 100 {
			summary = string([]rune(summary)[:100]) + "…"
		}
		fmt.Printf("  #%d %s %s→%s [%s] %s\n", envelope.Gen, envelope.Kind, envelope.From, envelope.To, envelope.State, summary)
		if envelope.Error != "" {
			fmt.Printf("     error: %s\n", envelope.Error)
		}
	}
	return nil
}

func relayCancel(args []string) error {
	fs := flag.NewFlagSet("relay cancel", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config.toml")
	botID := fs.String("bot", "", "bot id whose local API to query")
	socket := fs.String("socket", "", "Nectar API socket path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("relay cancel needs a root id")
	}
	path, _, _, err := resolveScheduleSocket(*socket, *configPath, *botID)
	if err != nil {
		return err
	}
	if err := relayRequest(path, http.MethodPost, "/relay/roots/"+strings.TrimSpace(fs.Arg(0))+"/cancel", nil, nil); err != nil {
		return err
	}
	fmt.Printf("Canceled %s.\n", fs.Arg(0))
	return nil
}

// relayRequest talks to the local relay API. It forwards NECTAR_RELAY_TOKEN
// when present; relay send requires it, read-only operations do not.
func relayRequest(socket, method, path string, body any, target any) error {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}
	client := scheduleHTTPClient(socket)
	req, err := http.NewRequestWithContext(context.Background(), method, "http://nectar"+path, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := strings.TrimSpace(os.Getenv("NECTAR_RELAY_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("relay daemon unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var failure struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&failure)
		if failure.Message == "" {
			failure.Message = failure.Error
		}
		if failure.Code == "" && failure.Message == "" {
			failure.Message = "relay api returned " + resp.Status
		}
		return &relayCLIError{Code: failure.Code, Message: failure.Message, Status: resp.StatusCode}
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}
