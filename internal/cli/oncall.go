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

type oncallShiftResponse struct {
	Shift oncall.Shift `json:"shift"`
}

type oncallShiftListResponse struct {
	Shifts []oncall.Shift `json:"shifts"`
}

type oncallCheckResponse struct {
	ShiftID string      `json:"shift_id"`
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
	case "start", "add":
		err = oncallCLIStart(args)
	case "list", "ls":
		err = oncallCLIList(args)
	case "show", "status":
		err = oncallCLIShow(args)
	case "check":
		err = oncallCLICheck(args)
	case "pause", "resume", "stop", "delete", "del", "rm":
		if command == "del" || command == "rm" {
			command = "delete"
		}
		err = oncallCLIShiftAction(command, args)
	case "jobs":
		err = oncallCLIJobs(args)
	case "job":
		err = oncallCLIJob(args)
	case "retry", "cancel":
		err = oncallCLIJobAction(command, args)
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

func oncallCLIStart(args []string) error {
	fs := flag.NewFlagSet("oncall start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	objective := fs.String("objective", "", "what Nectar should watch and why")
	source := fs.String("source", "", "github | command | http")
	repo := fs.String("repo", "", "GitHub repository owner/name")
	path := fs.String("path", "", "local checkout")
	branch := fs.String("branch", "", "branch to watch")
	command := fs.String("command", "", "shell command probe")
	workdir := fs.String("workdir", "", "command probe working directory")
	urlValue := fs.String("url", "", "HTTP URL probe")
	actionDir := fs.String("action-dir", "", "workspace for agent investigation")
	alertOnExit := fs.Bool("alert-on-exit", true, "treat non-zero command exit as an alert")
	every := fs.String("every", "", "check interval, e.g. 5m or 2s")
	start := fs.String("start", "", "RFC3339 start time")
	forValue := fs.String("for", "", "shift duration, e.g. 10h")
	until := fs.String("until", "", "RFC3339 end time")
	autonomy := fs.String("autonomy", "", "notify | diagnose | fix")
	maxMinutes := fs.Int("max-minutes", 0, "per-agent-job timeout")
	maxAgentRuns := fs.Int("max-agent-runs", 0, "maximum agent jobs in this shift")
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
	req := oncall.ShiftRequest{
		Objective:    *objective,
		Source:       strings.ToLower(strings.TrimSpace(*source)),
		Repo:         *repo,
		Path:         firstNonEmpty(*path, os.Getenv("NECTAR_WORKING_DIR")),
		Branch:       *branch,
		Command:      *command,
		Workdir:      firstNonEmpty(*workdir, os.Getenv("NECTAR_WORKING_DIR")),
		URL:          *urlValue,
		ActionDir:    *actionDir,
		AlertOnExit:  *alertOnExit,
		Every:        *every,
		Start:        *start,
		For:          *forValue,
		Until:        *until,
		Autonomy:     *autonomy,
		MaxMinutes:   *maxMinutes,
		MaxAgentRuns: *maxAgentRuns,
		Channel:      firstNonEmpty(*channel, os.Getenv("NECTAR_CHANNEL")),
		ChatID:       firstNonEmpty(*chat, os.Getenv("NECTAR_CHAT_ID")),
		ChatType:     firstNonEmpty(*chatType, os.Getenv("NECTAR_CHAT_TYPE")),
		MessageID:    os.Getenv("NECTAR_MESSAGE_ID"),
		ThreadID:     firstNonEmpty(*thread, os.Getenv("NECTAR_THREAD_ID")),
		UserID:       os.Getenv("NECTAR_USER_ID"),
		AgentID:      firstNonEmpty(*agent, os.Getenv("NECTAR_AGENT_ID")),
	}
	if req.Objective == "" {
		return errors.New("--objective is required")
	}
	if req.Source == "" {
		switch {
		case req.URL != "":
			req.Source = oncall.SourceHTTP
		case req.Command != "":
			req.Source = oncall.SourceCommand
		default:
			req.Source = oncall.SourceGitHub
		}
	}
	if req.ChatID == "" {
		return errors.New("--chat is required (or run inside a nectar agent session)")
	}
	var response oncallShiftResponse
	if err := scheduleRequest(*socket, http.MethodPost, "/oncall/shifts", req, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	fmt.Println(formatOnCallShift(response.Shift))
	return nil
}

func oncallCLIList(args []string) error {
	fs := flag.NewFlagSet("oncall list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	all := fs.Bool("all", false, "include completed and paused shifts")
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "local API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := "/oncall/shifts"
	if *all {
		path += "?all=1"
	}
	var response oncallShiftListResponse
	if err := scheduleRequest(*socket, http.MethodGet, path, nil, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	if len(response.Shifts) == 0 {
		fmt.Println("No on-call shifts.")
		return nil
	}
	for _, shift := range response.Shifts {
		fmt.Println(formatOnCallShift(shift))
		fmt.Println()
	}
	return nil
}

func oncallCLIShow(args []string) error {
	id, args := splitScheduleID(args)
	fs := flag.NewFlagSet("oncall show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "local API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if len(fs.Args()) == 0 {
			return errors.New("oncall show requires a shift id")
		}
		id = fs.Args()[0]
	}
	var response oncallShiftResponse
	if err := scheduleRequest(*socket, http.MethodGet, "/oncall/shifts/"+id, nil, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	fmt.Println(formatOnCallShift(response.Shift))
	return nil
}

func oncallCLICheck(args []string) error {
	id, args := splitScheduleID(args)
	fs := flag.NewFlagSet("oncall check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "local API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if len(fs.Args()) == 0 {
			return errors.New("oncall check requires a shift id")
		}
		id = fs.Args()[0]
	}
	var response oncallCheckResponse
	if err := scheduleRequest(*socket, http.MethodPost, "/oncall/shifts/"+id+"/check", nil, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	if response.Job == nil {
		fmt.Printf("%s checked: no change or alert\n", id)
		return nil
	}
	fmt.Println(formatOnCallJob(*response.Job))
	return nil
}

func oncallCLIShiftAction(command string, args []string) error {
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
			return fmt.Errorf("oncall %s requires a shift id", command)
		}
		id = fs.Args()[0]
	}
	method := http.MethodPost
	path := "/oncall/shifts/" + id + "/" + command
	if command == "delete" {
		method = http.MethodDelete
		path = "/oncall/shifts/" + id
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
			return errors.New("oncall job requires a job id")
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

func oncallCLIJobAction(command string, args []string) error {
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
	shiftID := fs.String("shift", "", "shift id")
	since := fs.String("since", "12h", "duration or RFC3339 start time")
	jsonOut := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "local API socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var response oncall.Report
	path := "/oncall/report?since=" + url.QueryEscape(*since)
	if *shiftID != "" {
		path += "&shift=" + url.QueryEscape(*shiftID)
	}
	if err := scheduleRequest(*socket, http.MethodGet, path, nil, &response); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(response)
	}
	fmt.Printf("On-call report %s → %s\n", formatOnCallTime(response.Since), formatOnCallTime(response.Until))
	fmt.Printf("completed=%d failed=%d running=%d canceled=%d total=%d\n",
		response.Completed, response.Failed, response.Running, response.Canceled, response.Total)
	fmt.Printf("checks=%d incidents=%d agent_runs=%d\n",
		response.Shift.Checks, response.Incidents, response.Shift.AgentRuns)
	for _, job := range response.Jobs {
		fmt.Println()
		fmt.Println(formatOnCallJob(job))
	}
	return nil
}

func formatOnCallShift(shift oncall.Shift) string {
	lines := []string{
		fmt.Sprintf("%s  %s  %s  every %s", shift.ID, shift.Status, shift.Source, shift.Every),
		"  " + shift.Objective,
		fmt.Sprintf("  checks %d · incidents %d · agent runs %d/%d",
			shift.Checks, shift.Incidents, shift.AgentRuns, shift.MaxAgentRuns),
	}
	if !shift.StartAt.IsZero() {
		lines = append(lines, "  start: "+formatOnCallTime(shift.StartAt))
	}
	if !shift.EndAt.IsZero() {
		lines = append(lines, "  end: "+formatOnCallTime(shift.EndAt))
	}
	if shift.LastSummary != "" {
		lines = append(lines, "  last: "+firstOnCallLine(shift.LastSummary))
	}
	if shift.LastError != "" {
		lines = append(lines, "  error: "+firstOnCallLine(shift.LastError))
	}
	return strings.Join(lines, "\n")
}

func formatOnCallJob(job oncall.Job) string {
	lines := []string{
		fmt.Sprintf("%s  %s  %s", job.ID, job.Status, job.Source),
	}
	if job.TriggerSummary != "" {
		lines = append(lines, "  trigger: "+firstOnCallLine(job.TriggerSummary))
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
  nectar oncall start --objective TEXT --source github|command|http [options]
    github:  --repo OWNER/NAME [--path DIR] [--branch BRANCH]
    command: --command TEXT [--workdir DIR] [--alert-on-exit]
    http:    --url URL
    common:  [--every 5m] [--start RFC3339] [--for 10h | --until RFC3339]
             [--autonomy notify|diagnose|fix] [--max-minutes 20]
             [--max-agent-runs 5] [--action-dir DIR]
             [--channel CHANNEL] [--chat CHAT_ID]
  nectar oncall list [--all] [--json]
  nectar oncall show <shift-id> [--json]
  nectar oncall check <shift-id> [--json]
  nectar oncall pause|resume|stop|delete <shift-id>
  nectar oncall jobs [--limit N] [--json]
  nectar oncall job <job-id> [--json]
  nectar oncall retry|cancel <job-id>
  nectar oncall report [--shift ID] [--since 12h] [--json]

The shift owns the loop; each incident creates at most one bounded agent job.
`)
}
