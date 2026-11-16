package core

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/oncall"
	"github.com/zhouyg1992/nectar/internal/platform"
)

const onCallHelpText = "`/oncall watch [repo] [branch]` create a watch\n" +
	"`/oncall list` show watches\n" +
	"`/oncall check|pause|resume|delete <watch-id>` manage a watch\n" +
	"`/oncall jobs [limit]` show recent jobs\n" +
	"`/oncall job <job-id>` show one job\n" +
	"`/oncall retry|cancel <job-id>` manage a job\n" +
	"`/oncall report [duration]` show a deterministic report"

func isOnCallCommand(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return lower == "/oncall" || strings.HasPrefix(lower, "/oncall ")
}

func (r *Router) handleOnCallCommand(key SessionKey, msg platform.Message, text string) {
	if r.oncall == nil {
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Disabled", "On-call mode is disabled in config.")
		return
	}
	if !r.isAdmin(msg.UserID) {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Admin Only", "Only configured admin users can manage on-call watches.")
		return
	}
	fields := strings.Fields(text)
	args := fields[1:]
	sub := "list"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
		args = args[1:]
	}
	switch sub {
	case "help", "?":
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Commands", onCallHelpText)
	case "list", "ls", "status":
		r.onCallList(msg)
	case "watch", "add":
		r.onCallAddWatch(key, msg, args)
	case "check":
		r.onCallWatchAction(msg, "check", args)
	case "pause", "resume", "delete", "del", "rm":
		if sub == "del" || sub == "rm" {
			sub = "delete"
		}
		r.onCallWatchAction(msg, sub, args)
	case "jobs":
		limit := 10
		if len(args) > 0 {
			if parsed, err := strconv.Atoi(args[0]); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		r.onCallJobs(msg, limit)
	case "job", "show":
		r.onCallJob(msg, args)
	case "retry", "cancel", "stop":
		if sub == "stop" {
			sub = "cancel"
		}
		r.onCallJobAction(msg, sub, args)
	case "report":
		r.onCallReport(msg, args)
	default:
		r.sendCommandResult(msg, platform.CardError, "⚠️ Unknown Command", onCallHelpText)
	}
}

func (r *Router) onCallList(msg platform.Message) {
	watches := r.oncall.ListWatches(true)
	if len(watches) == 0 {
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Watches", "No on-call watches.")
		return
	}
	var body strings.Builder
	for _, watch := range watches {
		fmt.Fprintf(&body, "`%s` · %s · %s@%s\n", watch.ID, watch.Status, watch.Repo, watch.Branch)
		fmt.Fprintf(&body, "every %s · %s · max %dm\n", watch.Every, watch.Autonomy, watch.MaxMinutes)
		if watch.Window != "" {
			fmt.Fprintf(&body, "window %s\n", watch.Window)
		}
		if watch.LastError != "" {
			fmt.Fprintf(&body, "error: %s\n", watch.LastError)
		}
		body.WriteString("\n")
	}
	r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Watches", strings.TrimSpace(body.String()))
}

func (r *Router) onCallAddWatch(key SessionKey, msg platform.Message, args []string) {
	workdir, err := r.workspaces.ForChat(msg.ChatID)
	if err != nil {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Workspace Error", err.Error())
		return
	}
	req := oncall.WatchRequest{
		Path:      workdir,
		Channel:   key.Channel,
		ChatID:    key.ChatID,
		ChatType:  msg.ChatType,
		MessageID: msg.MessageID,
		ThreadID:  key.ThreadID,
		UserID:    msg.UserID,
		AgentID:   key.AgentID,
	}
	if len(args) > 0 {
		req.Repo = args[0]
	}
	if len(args) > 1 {
		req.Branch = args[1]
	}
	watch, err := r.oncall.AddWatch(context.Background(), req, msg.UserID)
	if err != nil {
		r.sendCommandResult(msg, platform.CardError, "⚠️ On-call Error", err.Error())
		return
	}
	body := fmt.Sprintf("`%s` · %s@%s\nevery %s · autonomy %s\nUse `/oncall check %s` to diagnose the current failure.",
		watch.ID, watch.Repo, watch.Branch, watch.Every, watch.Autonomy, watch.ID)
	r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Watch Created", body)
}

func (r *Router) onCallWatchAction(msg platform.Message, action string, args []string) {
	if len(args) == 0 {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: `/oncall "+action+" <watch-id>`")
		return
	}
	id := args[0]
	switch action {
	case "check":
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Check", "Checking the latest GitHub Actions run…")
		go func() {
			job, err := r.oncall.CheckWatchNow(context.Background(), id)
			if err != nil {
				r.sendCommandResult(msg, platform.CardError, "⚠️ On-call Error", err.Error())
				return
			}
			if job == nil {
				r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Check", "No new failed run.")
				return
			}
			r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Job", onCallJobSummary(*job))
		}()
	case "pause":
		if _, err := r.oncall.PauseWatch(id); err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ On-call Error", err.Error())
			return
		}
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Paused", id)
	case "resume":
		if _, err := r.oncall.ResumeWatch(id); err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ On-call Error", err.Error())
			return
		}
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Resumed", id)
	case "delete":
		if err := r.oncall.DeleteWatch(id); err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ On-call Error", err.Error())
			return
		}
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Deleted", id)
	}
}

func (r *Router) onCallJobs(msg platform.Message, limit int) {
	jobs := r.oncall.ListJobs(limit)
	if len(jobs) == 0 {
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Jobs", "No on-call jobs.")
		return
	}
	var body strings.Builder
	for _, job := range jobs {
		body.WriteString(onCallJobSummary(job))
		body.WriteString("\n\n")
	}
	r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Jobs", strings.TrimSpace(body.String()))
}

func (r *Router) onCallJob(msg platform.Message, args []string) {
	if len(args) == 0 {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: `/oncall job <job-id>`")
		return
	}
	job, ok := r.oncall.GetJob(args[0])
	if !ok {
		r.sendCommandResult(msg, platform.CardError, "⚠️ On-call Error", "Job not found.")
		return
	}
	body := onCallJobSummary(job)
	if job.Evidence != "" {
		body += "\n\n" + job.Evidence
	}
	r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Job", body)
}

func (r *Router) onCallJobAction(msg platform.Message, action string, args []string) {
	if len(args) == 0 {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: `/oncall "+action+" <job-id>`")
		return
	}
	id := args[0]
	if action == "retry" {
		job, err := r.oncall.RetryJob(context.Background(), id)
		if err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ On-call Error", err.Error())
			return
		}
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Retry", onCallJobSummary(*job))
		return
	}
	if err := r.oncall.CancelJob(id); err != nil {
		r.sendCommandResult(msg, platform.CardError, "⚠️ On-call Error", err.Error())
		return
	}
	r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Cancel", id)
}

func (r *Router) onCallReport(msg platform.Message, args []string) {
	since := time.Now().Add(-12 * time.Hour)
	if len(args) > 0 {
		if duration, err := time.ParseDuration(args[0]); err == nil && duration > 0 {
			since = time.Now().Add(-duration)
		} else {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: `/oncall report [duration]`, for example `24h`.")
			return
		}
	}
	report := r.oncall.Report(since)
	var body strings.Builder
	fmt.Fprintf(&body, "completed=%d failed=%d running=%d canceled=%d total=%d\n\n",
		report.Completed, report.Failed, report.Running, report.Canceled, report.Total)
	for _, job := range report.Jobs {
		body.WriteString(onCallJobSummary(job))
		body.WriteString("\n\n")
	}
	if report.Total == 0 {
		body.WriteString("No on-call jobs in this period.")
	}
	r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Report", strings.TrimSpace(body.String()))
}

func onCallJobSummary(job oncall.Job) string {
	body := fmt.Sprintf("`%s` · %s · %s@%s\n%s", job.ID, job.Status, job.Repo, job.Branch, job.Workflow)
	if job.Summary != "" {
		body += "\n" + firstLine(job.Summary)
	}
	if job.Error != "" {
		body += "\nError: " + firstLine(job.Error)
	}
	if job.ArtifactDir != "" {
		body += "\nArtifacts: `" + job.ArtifactDir + "`"
	}
	return body
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		value = value[:idx]
	}
	if runes := []rune(value); len(runes) > 240 {
		value = string(runes[:240]) + "…"
	}
	return value
}
