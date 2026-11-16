package cli

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/oncall"
)

type oncallWatchResponse struct {
	Watch oncall.Watch `json:"watch"`
}

type oncallWatchListResponse struct {
	Watches []oncall.Watch `json:"watches"`
}

type oncallCheckResponse struct {
	WatchID string      `json:"watch_id"`
	Job     *oncall.Job `json:"job"`
}

type oncallJobResponse struct {
	Job oncall.Job `json:"job"`
}

type oncallJobListResponse struct {
	Jobs []oncall.Job `json:"jobs"`
}

func runOnCall(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printOnCallUsage()
		return nil
	}
	command := args[0]
	args = args[1:]
	var err error
	switch command {
	case "watch":
		err = runOnCallWatch(args)
	case "jobs", "list":
		err = oncallCLIJobs(args)
	case "job", "show":
		err = oncallCLIJob(args)
	case "retry":
		err = oncallCLIAction("retry", args)
	case "cancel", "stop":
		err = oncallCLIAction("cancel", args)
	case "report":
		err = oncallCLIReport(args)
	default:
		printOnCallUsage()
		return fmt.Errorf("unknown oncall command %q", command)
	}
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

func runOnCallWatch(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printOnCallUsage()
		return nil
	}
	command := args[0]
	args = args[1:]
	switch command {
	case "add", "create":
		return oncallCLIWatchAdd(args)
	case "list", "ls":
		return oncallCLIWatchList(args)
	case "pause", "resume", "check", "delete", "del", "rm", "remove":
		if command == "del" || command == "rm" || command == "remove" {
			command = "delete"
		}
		return oncallCLIWatchAction(command, args)
	default:
		printOnCallUsage()
		return fmt.Errorf("unknown oncall watch command %q", command)
	}
}

func oncallCLIWatchAdd(args []string) error {
	fs := flag.NewFlagSet("oncall watch add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repo := fs.String("repo", "", "GitHub repository (owner/name); inferred from origin when empty")
	path := fs.String("path", "", "local git checkout; defaults to NECTAR_WORKING_DIR or cwd")
	branch := fs.String("branch", "", "branch to watch; defaults from git or main")
	every := fs.String("every", "", "poll interval, e.g. 5m")
	window := fs.String("window", "", "local time window, e.g. 22:00-08:00")
	autonomy := fs.String("autonomy", "", "diagnose or fix")
	maxMinutes := fs.Int("max-minutes", 0, "per-job timeout in minutes")
	maxAttempts := fs.Int("max-attempts", 0, "maximum attempts for manual retries")
	channel := fs.String("channel", "", "target channel")
	chat := fs.String("chat", "", "target chat id")
	chatType := fs.String("chat-type", "", "target chat type")
	thread := fs.String("thread", "", "target thread id")
	agent := fs.String("agent", "", "target agent id")
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "local API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	req := oncall.WatchRequest{
		Repo:        *repo,
		Path:        firstNonEmpty(*path, os.Getenv("NECTAR_WORKING_DIR")),
		Branch:      *branch,
		Every:       *every,
		Window:      *window,
		Autonomy:    *autonomy,
		MaxMinutes:  *maxMinutes,
		MaxAttempts: *maxAttempts,
		Channel:     firstNonEmpty(*channel, os.Getenv("NECTAR_CHANNEL")),
		ChatID:      firstNonEmpty(*chat, os.Getenv("NECTAR_CHAT_ID")),
		ChatType:    firstNonEmpty(*chatType, os.Getenv("NECTAR_CHAT_TYPE")),
		MessageID:   os.Getenv("NECTAR_MESSAGE_ID"),
		ThreadID:    firstNonEmpty(*thread, os.Getenv("NECTAR_THREAD_ID")),
		UserID:      os.Getenv("NECTAR_USER_ID"),
		AgentID:     firstNonEmpty(*agent, os.Getenv("NECTAR_AGENT_ID")),
	}
	if req.Path == "" {
		req.Path = "."
	}
	if req.ChatID == "" {
		return errors.New("--chat is required (or run inside a nectar agent session)")
	}
	var response oncallWatchResponse
	if err := scheduleRequest(*socket, http.MethodPost, "/oncall/watches", req, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	fmt.Println(formatOnCallWatch(response.Watch))
	return nil
}

func oncallCLIWatchList(args []string) error {
	fs := flag.NewFlagSet("oncall watch list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	all := fs.Bool("all", false, "include paused watches")
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "local API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := "/oncall/watches"
	if *all {
		path += "?all=1"
	}
	var response oncallWatchListResponse
	if err := scheduleRequest(*socket, http.MethodGet, path, nil, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	if len(response.Watches) == 0 {
		fmt.Println("No on-call watches.")
		return nil
	}
	for _, watch := range response.Watches {
		fmt.Println(formatOnCallWatch(watch))
		fmt.Println()
	}
	return nil
}

func oncallCLIWatchAction(command string, args []string) error {
	id, args := splitScheduleID(args)
	fs := flag.NewFlagSet("oncall watch "+command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "local API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if len(fs.Args()) == 0 {
			return fmt.Errorf("oncall watch %s requires an id", command)
		}
		id = fs.Args()[0]
	}
	method := http.MethodPost
	path := "/oncall/watches/" + id + "/" + command
	if command == "delete" {
		method = http.MethodDelete
		path = "/oncall/watches/" + id
	}
	if command == "check" {
		var response oncallCheckResponse
		if err := scheduleRequest(*socket, method, path, nil, &response); err != nil {
			return err
		}
		if *jsonOut {
			return printJSON(response)
		}
		if response.Job == nil {
			fmt.Printf("%s checked: no new failed run\n", id)
			return nil
		}
		fmt.Println(formatOnCallJob(*response.Job))
		return nil
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

func oncallCLIJobs(args []string) error {
	fs := flag.NewFlagSet("oncall jobs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	limit := fs.Int("limit", 20, "maximum number of jobs")
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "local API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := fmt.Sprintf("/oncall/jobs?limit=%d", *limit)
	var response oncallJobListResponse
	if err := scheduleRequest(*socket, http.MethodGet, path, nil, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	if len(response.Jobs) == 0 {
		fmt.Println("No on-call jobs.")
		return nil
	}
	for _, job := range response.Jobs {
		fmt.Println(formatOnCallJob(job))
		fmt.Println()
	}
	return nil
}

func oncallCLIJob(args []string) error {
	id, args := splitScheduleID(args)
	fs := flag.NewFlagSet("oncall job", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "local API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if len(fs.Args()) == 0 {
			return errors.New("oncall job requires an id")
		}
		id = fs.Args()[0]
	}
	var response oncallJobResponse
	if err := scheduleRequest(*socket, http.MethodGet, "/oncall/jobs/"+id, nil, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	fmt.Println(formatOnCallJob(response.Job))
	if response.Job.Evidence != "" {
		fmt.Println()
		fmt.Println(response.Job.Evidence)
	}
	return nil
}

func oncallCLIAction(command string, args []string) error {
	id, args := splitScheduleID(args)
	fs := flag.NewFlagSet("oncall "+command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "local API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if len(fs.Args()) == 0 {
			return fmt.Errorf("oncall %s requires a job id", command)
		}
		id = fs.Args()[0]
	}
	var response oncallJobResponse
	if err := scheduleRequest(*socket, http.MethodPost, "/oncall/jobs/"+id+"/"+command, nil, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	fmt.Println(formatOnCallJob(response.Job))
	return nil
}

func oncallCLIReport(args []string) error {
	fs := flag.NewFlagSet("oncall report", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	since := fs.String("since", "12h", "duration or RFC3339 start time")
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "local API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var response oncall.Report
	path := "/oncall/report?since=" + url.QueryEscape(*since)
	if err := scheduleRequest(*socket, http.MethodGet, path, nil, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	fmt.Printf("On-call report %s → %s\n", formatOnCallTime(response.Since), formatOnCallTime(response.Until))
	fmt.Printf("completed=%d failed=%d running=%d canceled=%d total=%d\n",
		response.Completed, response.Failed, response.Running, response.Canceled, response.Total)
	for _, job := range response.Jobs {
		fmt.Println()
		fmt.Println(formatOnCallJob(job))
	}
	return nil
}

func formatOnCallWatch(watch oncall.Watch) string {
	lines := []string{
		fmt.Sprintf("%s  %s  %s@%s", watch.ID, watch.Status, watch.Repo, watch.Branch),
		"  path: " + watch.Path,
		fmt.Sprintf("  every %s · autonomy %s · max %dm", watch.Every, watch.Autonomy, watch.MaxMinutes),
	}
	if watch.Window != "" {
		lines = append(lines, "  window: "+watch.Window)
	}
	if !watch.NextCheck.IsZero() {
		lines = append(lines, "  next: "+formatOnCallTime(watch.NextCheck))
	}
	if watch.LastError != "" {
		lines = append(lines, "  error: "+watch.LastError)
	}
	return strings.Join(lines, "\n")
}

func formatOnCallJob(job oncall.Job) string {
	lines := []string{
		fmt.Sprintf("%s  %s  %s@%s", job.ID, job.Status, job.Repo, job.Branch),
		fmt.Sprintf("  run: %s · %s", job.Workflow, job.RunURL),
	}
	if job.Summary != "" {
		lines = append(lines, "  summary: "+firstOnCallLine(job.Summary))
	}
	if job.Error != "" {
		lines = append(lines, "  error: "+firstOnCallLine(job.Error))
	}
	if job.ArtifactDir != "" {
		lines = append(lines, "  artifacts: "+job.ArtifactDir)
	}
	return strings.Join(lines, "\n")
}

func firstOnCallLine(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		value = value[:idx]
	}
	if runes := []rune(value); len(runes) > 200 {
		value = string(runes[:200]) + "…"
	}
	return value
}

func formatOnCallTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.In(time.Local).Format("2006-01-02 15:04 MST")
}

func printOnCallUsage() {
	fmt.Print(`Usage:
  nectar oncall watch add [--repo OWNER/NAME] [--path DIR] [--branch BRANCH]
    [--every 5m] [--window 22:00-08:00] [--autonomy diagnose|fix]
    [--max-minutes 20] [--max-attempts 2]
    [--channel CHANNEL] [--chat CHAT_ID] [--thread THREAD_ID] [--agent AGENT]
  nectar oncall watch list [--all] [--json]
  nectar oncall watch pause|resume|check|delete <watch-id>
  nectar oncall jobs [--limit N] [--json]
  nectar oncall job <job-id> [--json]
  nectar oncall retry|cancel <job-id>
  nectar oncall report [--since 12h] [--json]

Target chat defaults come from NECTAR_CHANNEL, NECTAR_CHAT_ID,
NECTAR_CHAT_TYPE, NECTAR_THREAD_ID, and NECTAR_AGENT_ID, which Nectar injects
into agent sessions. The local checkout defaults to NECTAR_WORKING_DIR.
`)
}
