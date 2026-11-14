package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type scheduleCLIRequest struct {
	Mode        string `json:"mode,omitempty"`
	Cron        string `json:"cron,omitempty"`
	Every       string `json:"every,omitempty"`
	At          string `json:"at,omitempty"`
	In          string `json:"in,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
	Description string `json:"description,omitempty"`
	Timeout     string `json:"timeout,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	Channel     string `json:"channel,omitempty"`
	ChatID      string `json:"chat_id,omitempty"`
	ThreadID    string `json:"thread_id,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
}

type scheduleCLITask struct {
	ID          string    `json:"id"`
	Mode        string    `json:"mode"`
	CronExpr    string    `json:"cron,omitempty"`
	Every       string    `json:"every,omitempty"`
	Timezone    string    `json:"timezone,omitempty"`
	Prompt      string    `json:"prompt"`
	Description string    `json:"description,omitempty"`
	Timeout     string    `json:"timeout,omitempty"`
	Status      string    `json:"status"`
	Running     bool      `json:"running,omitempty"`
	NextRun     time.Time `json:"next_run,omitempty"`
	LastRun     time.Time `json:"last_run,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	RunCount    int       `json:"run_count,omitempty"`
}

type scheduleCLITaskResponse struct {
	Task scheduleCLITask `json:"task"`
}

type scheduleCLIListResponse struct {
	Tasks []scheduleCLITask `json:"tasks"`
}

type scheduleCLIPreviewResponse struct {
	NextRuns []time.Time `json:"next_runs"`
}

func runSchedule(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printScheduleUsage()
		return nil
	}
	command := args[0]
	args = args[1:]

	var err error
	switch command {
	case "list", "ls":
		err = scheduleCLIList(args)
	case "add":
		err = scheduleCLIAdd(args, false)
	case "preview":
		err = scheduleCLIAdd(args, true)
	case "pause", "resume", "run", "cancel", "stop", "delete", "del", "rm":
		err = scheduleCLIAction(command, args)
	case "update":
		err = scheduleCLIUpdate(args)
	default:
		printScheduleUsage()
		return fmt.Errorf("unknown schedule command %q", command)
	}
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

func scheduleCLIList(args []string) error {
	fs := flag.NewFlagSet("schedule list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	all := fs.Bool("all", false, "include completed and failed tasks")
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "schedule API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := "/schedule"
	if *all {
		path += "?all=1"
	}
	var response scheduleCLIListResponse
	if err := scheduleRequest(*socket, http.MethodGet, path, nil, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	if len(response.Tasks) == 0 {
		fmt.Println("No scheduled tasks.")
		return nil
	}
	for _, task := range response.Tasks {
		fmt.Println(formatCLITask(task))
		fmt.Println()
	}
	return nil
}

func scheduleCLIAdd(args []string, preview bool) error {
	name := "schedule add"
	if preview {
		name = "schedule preview"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cron := fs.String("cron", "", "5-field cron expression")
	every := fs.String("every", "", "fixed interval, e.g. 90m or 14d")
	at := fs.String("at", "", "one-shot RFC3339 time")
	in := fs.String("in", "", "one-shot delay, e.g. 30m")
	prompt := fs.String("prompt", "", "task prompt")
	desc := fs.String("desc", "", "short description")
	timeout := fs.String("timeout", "", "per-run timeout, e.g. 30m; 0s disables it")
	timezone := fs.String("timezone", "", "IANA timezone")
	channel := fs.String("channel", "", "target channel")
	chat := fs.String("chat", "", "target chat id")
	thread := fs.String("thread", "", "target thread id")
	agent := fs.String("agent", "", "target agent id")
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "schedule API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}

	req := scheduleCLIRequest{
		Cron:        *cron,
		Every:       *every,
		At:          *at,
		In:          *in,
		Prompt:      *prompt,
		Description: *desc,
		Timeout:     *timeout,
		Timezone:    *timezone,
		Channel:     firstNonEmpty(*channel, os.Getenv("NECTAR_CHANNEL")),
		ChatID:      firstNonEmpty(*chat, os.Getenv("NECTAR_CHAT_ID")),
		ThreadID:    firstNonEmpty(*thread, os.Getenv("NECTAR_THREAD_ID")),
		AgentID:     firstNonEmpty(*agent, os.Getenv("NECTAR_AGENT_ID")),
	}
	modes := 0
	for _, value := range []string{req.Cron, req.Every, req.At, req.In} {
		if strings.TrimSpace(value) != "" {
			modes++
		}
	}
	if modes > 1 {
		return errors.New("choose exactly one of --cron, --every, --at, or --in")
	}
	if req.Cron != "" {
		req.Mode = "cron"
	} else if req.Every != "" {
		req.Mode = "every"
	} else if req.At != "" {
		req.Mode = "at"
	} else if req.In != "" {
		req.Mode = "in"
	}
	if req.Prompt == "" && !preview {
		return errors.New("--prompt is required")
	}
	if req.ChatID == "" && !preview {
		return errors.New("--chat is required (or run inside a nectar agent session)")
	}

	path := "/schedule"
	if preview {
		path = "/schedule/preview"
		var response scheduleCLIPreviewResponse
		if err := scheduleRequest(*socket, http.MethodPost, path, req, &response); err != nil {
			return err
		}
		if *jsonOut {
			return printJSON(response)
		}
		for _, next := range response.NextRuns {
			fmt.Println(next.In(scheduleCLILocation(req.Timezone)).Format("2006-01-02 15:04 MST"))
		}
		return nil
	}

	var response scheduleCLITaskResponse
	if err := scheduleRequest(*socket, http.MethodPost, path, req, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	fmt.Println(formatCLITask(response.Task))
	return nil
}

func scheduleCLIAction(command string, args []string) error {
	id, args := splitScheduleID(args)
	fs := flag.NewFlagSet("schedule "+command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "schedule API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		positional := fs.Args()
		if len(positional) == 0 {
			return fmt.Errorf("schedule %s requires an id", command)
		}
		id = positional[0]
	}
	method := http.MethodPost
	path := "/schedule/" + id + "/" + command
	switch command {
	case "stop":
		path = "/schedule/" + id + "/cancel"
	case "delete", "del", "rm":
		method = http.MethodDelete
		path = "/schedule/" + id
	}
	var response map[string]any
	if err := scheduleRequest(*socket, method, path, nil, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	fmt.Printf("%s %s\n", id, command)
	return nil
}

func scheduleCLIUpdate(args []string) error {
	id, args := splitScheduleID(args)
	fs := flag.NewFlagSet("schedule update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	prompt := fs.String("prompt", "", "new prompt")
	desc := fs.String("desc", "", "new description")
	timeout := fs.String("timeout", "", "new per-run timeout")
	cron := fs.String("cron", "", "new cron expression")
	every := fs.String("every", "", "new interval")
	at := fs.String("at", "", "new one-shot time")
	in := fs.String("in", "", "new one-shot delay")
	timezone := fs.String("timezone", "", "new IANA timezone")
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "schedule API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		positional := fs.Args()
		if len(positional) == 0 {
			return errors.New("schedule update requires an id")
		}
		id = positional[0]
	}
	req := scheduleCLIRequest{
		Prompt:      *prompt,
		Description: *desc,
		Timeout:     *timeout,
		Cron:        *cron,
		Every:       *every,
		At:          *at,
		In:          *in,
		Timezone:    *timezone,
	}
	modes := 0
	for _, value := range []string{req.Cron, req.Every, req.At, req.In} {
		if strings.TrimSpace(value) != "" {
			modes++
		}
	}
	if modes > 1 {
		return errors.New("choose at most one of --cron, --every, --at, or --in")
	}
	if req.Cron != "" {
		req.Mode = "cron"
	} else if req.Every != "" {
		req.Mode = "every"
	} else if req.At != "" {
		req.Mode = "at"
	} else if req.In != "" {
		req.Mode = "in"
	}
	var response scheduleCLITaskResponse
	if err := scheduleRequest(*socket, http.MethodPatch, "/schedule/"+id, req, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	fmt.Println(formatCLITask(response.Task))
	return nil
}

func splitScheduleID(args []string) (string, []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") && args[0] != "" {
		return args[0], args[1:]
	}
	return "", args
}

func scheduleRequest(socket, method, path string, body any, target any) error {
	path = strings.TrimSpace(path)
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
	if userID := strings.TrimSpace(os.Getenv("NECTAR_USER_ID")); userID != "" {
		req.Header.Set("X-Nectar-User-ID", userID)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("schedule daemon unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&failure)
		if failure.Error != "" {
			return errors.New(failure.Error)
		}
		return fmt.Errorf("schedule api returned %s", resp.Status)
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func scheduleHTTPClient(socket string) *http.Client {
	path := socket
	if path == "" {
		path = scheduleSocketPath()
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", path)
		},
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}
}

func scheduleSocketPath() string {
	if value := strings.TrimSpace(os.Getenv("NECTAR_API_SOCKET")); value != "" {
		return value
	}
	dataDir := strings.TrimSpace(os.Getenv("NECTAR_DATA_DIR"))
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dataDir = filepath.Join(home, ".nectar")
	}
	return filepath.Join(dataDir, "run", "api.sock")
}

func scheduleCLILocation(timezone string) *time.Location {
	if strings.TrimSpace(timezone) == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Local
	}
	return loc
}

func formatCLITask(task scheduleCLITask) string {
	var window string
	switch task.Mode {
	case "cron":
		window = "cron " + task.CronExpr
	case "every":
		window = "every " + task.Every
	case "once":
		window = "once"
	default:
		window = task.Mode
	}
	lines := []string{
		fmt.Sprintf("%s  %s  %s", task.ID, scheduleCLIStatus(task), window),
		"  " + task.Prompt,
	}
	if task.Timeout != "" {
		lines = append(lines, "  timeout: "+task.Timeout)
	}
	if !task.NextRun.IsZero() {
		lines = append(lines, "  next: "+task.NextRun.In(scheduleCLILocation(task.Timezone)).Format("2006-01-02 15:04 MST"))
	}
	if task.LastError != "" {
		lines = append(lines, "  error: "+task.LastError)
	}
	return strings.Join(lines, "\n")
}

func printJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func printScheduleUsage() {
	fmt.Print(`Usage:
  nectar schedule list [--all] [--json] [--socket PATH]
  nectar schedule add --cron "0 9 * * 1-5" --prompt "..."
  nectar schedule add --every 90m --prompt "..."
  nectar schedule add --in 30m --prompt "..."
  nectar schedule add --at "2026-11-14T15:00:00+08:00" --prompt "..."
  nectar schedule preview --cron "0 9 * * 1-5"
  nectar schedule pause|resume|run|cancel|delete <id>
  nectar schedule update <id> [--prompt TEXT] [--cron EXPR] [--every DUR] [--at TIME] [--timezone TZ] [--timeout DUR]
  nectar schedule add ... [--timeout 30m]

Target defaults come from NECTAR_CHANNEL, NECTAR_CHAT_ID, NECTAR_THREAD_ID and
NECTAR_AGENT_ID, which nectar injects into agent sessions.
`)
}

func scheduleCLIStatus(task scheduleCLITask) string {
	if task.Running {
		return "running"
	}
	return task.Status
}
