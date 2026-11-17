package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/zhouyg1992/nectar/internal/core"
)

func runRelay(args []string) error {
	if len(args) == 0 {
		printRelayUsage()
		return nil
	}
	switch args[0] {
	case "bots":
		return runRelayBots(args[1:])
	case "send":
		return runRelaySend(args[1:])
	case "roots", "ls":
		return runRelayRoots(args[1:])
	case "show":
		return runRelayShow(args[1:])
	case "cancel":
		return runRelayCancel(args[1:])
	case "help", "-h", "--help":
		printRelayUsage()
		return nil
	default:
		return fmt.Errorf("unknown relay command %q", args[0])
	}
}

func runRelayBots(args []string) error {
	fs := flag.NewFlagSet("relay bots", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	socket := fs.String("socket", "", "relay API socket")
	configPath := fs.String("config", "", "path to config.toml")
	botID := fs.String("bot", "", "bot id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	socketPath, err := relaySocket(*socket, *configPath, *botID, false)
	if err != nil {
		return err
	}
	var response struct {
		Bots []core.RelayBotInfo `json:"bots"`
	}
	if err := relayRequest(context.Background(), socketPath, http.MethodGet, "/relay/bots", nil, &response); err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tROLE\tBUSY\tCAPABILITIES\tACCEPTS FROM\tDESCRIPTION")
	for _, bot := range response.Bots {
		fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%s\t%s\t%s\n",
			bot.ID, bot.Name, bot.Role, bot.Busy,
			strings.Join(bot.Capabilities, ","), strings.Join(bot.AcceptFrom, ","), bot.Description)
	}
	return w.Flush()
}

func runRelaySend(args []string) error {
	fs := flag.NewFlagSet("relay send", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	socket := fs.String("socket", "", "relay API socket")
	configPath := fs.String("config", "", "path to config.toml")
	botID := fs.String("bot", "", "bot id")
	to := fs.String("to", "", "target bot id")
	session := fs.String("session", "", "target session name")
	mode := fs.String("mode", "queue", "delivery mode (queue)")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key")
	echo := fs.String("echo", "", "echo mode: full, summary, off")
	promptMode := fs.String("prompt-mode", "", "prompt mode: full, minimal, none")
	as := fs.String("as", "", "operator bot id (requires relay.allow_operator_send)")
	wait := fs.Bool("wait", false, "wait for the final answer (not implemented in M0)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *wait {
		return fmt.Errorf("relay send --wait is not implemented yet")
	}
	if strings.TrimSpace(*to) == "" {
		return fmt.Errorf("relay send requires --to")
	}
	body := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if body == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		body = strings.TrimSpace(string(data))
	}
	if body == "" {
		return fmt.Errorf("relay send requires a body")
	}
	socketPath, err := relaySocket(*socket, *configPath, *botID, true)
	if err != nil {
		return err
	}
	request := core.RelaySendRequest{
		To:             *to,
		Body:           body,
		Session:        *session,
		Mode:           *mode,
		IdempotencyKey: *idempotencyKey,
		Echo:           *echo,
		PromptMode:     *promptMode,
		From:           *as,
	}
	var response struct {
		Envelope  core.RelayEnvelope `json:"envelope"`
		Duplicate bool               `json:"duplicate"`
	}
	if err := relayRequest(context.Background(), socketPath, http.MethodPost, "/relay/send", request, &response); err != nil {
		return err
	}
	fmt.Printf("envelope_id=%s root_id=%s state=%s duplicate=%v\n",
		response.Envelope.ID, response.Envelope.RootID, response.Envelope.State, response.Duplicate)
	fmt.Println("relay accepted; do not poll or wait. The reply will arrive as a new turn.")
	return nil
}

func runRelayRoots(args []string) error {
	fs := flag.NewFlagSet("relay roots", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	socket := fs.String("socket", "", "relay API socket")
	configPath := fs.String("config", "", "path to config.toml")
	botID := fs.String("bot", "", "bot id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	socketPath, err := relaySocket(*socket, *configPath, *botID, false)
	if err != nil {
		return err
	}
	var response struct {
		Roots []core.RelayRootInfo `json:"roots"`
	}
	if err := relayRequest(context.Background(), socketPath, http.MethodGet, "/relay/roots", nil, &response); err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ROOT\tORIGIN BOT\tCREATED\tENVELOPES\tRUNS\tSTATE")
	for _, root := range response.Roots {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n",
			root.ID, root.OriginBot, root.CreatedAt.Format("2006-01-02 15:04:05"), root.Envelopes, root.Runs, root.State)
	}
	return w.Flush()
}

func runRelayShow(args []string) error {
	fs := flag.NewFlagSet("relay show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	socket := fs.String("socket", "", "relay API socket")
	configPath := fs.String("config", "", "path to config.toml")
	botID := fs.String("bot", "", "bot id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("relay show requires a root id")
	}
	socketPath, err := relaySocket(*socket, *configPath, *botID, false)
	if err != nil {
		return err
	}
	var response struct {
		Root      core.RelayRootInfo   `json:"root"`
		Envelopes []core.RelayEnvelope `json:"envelopes"`
	}
	if err := relayRequest(context.Background(), socketPath, http.MethodGet, "/relay/roots/"+fs.Arg(0), nil, &response); err != nil {
		return err
	}
	data, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func runRelayCancel(args []string) error {
	fs := flag.NewFlagSet("relay cancel", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	socket := fs.String("socket", "", "relay API socket")
	configPath := fs.String("config", "", "path to config.toml")
	botID := fs.String("bot", "", "bot id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("relay cancel requires a root id")
	}
	socketPath, err := relaySocket(*socket, *configPath, *botID, false)
	if err != nil {
		return err
	}
	return relayRequest(context.Background(), socketPath, http.MethodPost, "/relay/roots/"+fs.Arg(0)+"/cancel", nil, nil)
}

func relaySocket(explicitSocket, configPath, botID string, requireToken bool) (string, error) {
	if requireToken && strings.TrimSpace(os.Getenv("NECTAR_RELAY_TOKEN")) == "" &&
		strings.TrimSpace(botID) == "" && strings.TrimSpace(explicitSocket) == "" && strings.TrimSpace(configPath) == "" {
		// The API will reject this with a clear message; keep the CLI usable for
		// diagnostics when a local socket is provided explicitly.
		return "", fmt.Errorf("relay token is missing; run this from an agent session or pass --socket/--bot")
	}
	socketPath, _, _, err := resolveScheduleSocket(explicitSocket, configPath, botID)
	return socketPath, err
}

func relayRequest(ctx context.Context, socket, method, path string, body any, target any) error {
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
	if strings.TrimSpace(socket) == "" {
		socket = scheduleSocketPath()
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://nectar"+path, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := strings.TrimSpace(os.Getenv("NECTAR_RELAY_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := scheduleHTTPClient(socket).Do(req)
	if err != nil {
		return fmt.Errorf("relay daemon unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var failure struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&failure)
		if failure.Code != "" {
			return fmt.Errorf("%s: %s", failure.Code, failure.Error)
		}
		if failure.Error != "" {
			return fmt.Errorf("relay api: %s", failure.Error)
		}
		return fmt.Errorf("relay api returned HTTP %d", resp.StatusCode)
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func printRelayUsage() {
	fmt.Print(`Usage:
  nectar relay bots [--socket PATH] [--config PATH] [--bot ID]
  nectar relay send --to BOT [--session NAME] [--mode queue] [--echo full|summary|off]
                     [--prompt-mode full|minimal|none] [--idempotency-key K] BODY
  nectar relay roots [--socket PATH]
  nectar relay show ROOT [--socket PATH]
  nectar relay cancel ROOT [--socket PATH]
`)
}
