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

const onCallHelpText = "`/oncall` list active shifts\n" +
	"`/oncall show <shift-id>` show a shift\n" +
	"`/oncall check <shift-id>` check immediately\n" +
	"`/oncall pause|resume|stop <shift-id>` manage a shift\n" +
	"`/oncall jobs [limit]` show recent jobs\n" +
	"`/oncall job <job-id>` show one job\n" +
	"`/oncall retry|cancel <job-id>` manage a job\n" +
	"`/oncall report [shift-id] [duration]` show a report\n\n" +
	"Create a shift from natural language, or use `nectar oncall start` with a source."

func isOnCallCommand(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return lower == "/oncall" || strings.HasPrefix(lower, "/oncall ")
}

func (r *Router) handleOnCallCommand(_ SessionKey, msg platform.Message, text string) {
	if r.oncall == nil {
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Disabled", "On-call shifts are disabled in config.")
		return
	}
	if !r.isAdmin(msg.UserID) {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Admin Only", "Only configured admin users can manage on-call shifts.")
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
	case "show":
		r.onCallShow(msg, args)
	case "check":
		r.onCallCheck(msg, args)
	case "pause", "resume", "stop", "delete", "del", "rm":
		if sub == "del" || sub == "rm" {
			sub = "delete"
		}
		r.onCallShiftAction(msg, sub, args)
	case "jobs":
		limit := 10
		if len(args) > 0 {
			if parsed, err := strconv.Atoi(args[0]); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		r.onCallJobs(msg, limit)
	case "job":
		r.onCallJob(msg, args)
	case "retry", "cancel":
		r.onCallJobAction(msg, sub, args)
	case "report":
		r.onCallReport(msg, args)
	case "start", "add", "watch":
		r.sendCommandResult(msg, platform.CardInfo, "🌙 Start an On-call Shift", onCallHelpText)
	default:
		r.sendCommandResult(msg, platform.CardError, "⚠️ Unknown Command", onCallHelpText)
	}
}

func (r *Router) onCallList(msg platform.Message) {
	shifts := r.oncall.ListShifts(true)
	if len(shifts) == 0 {
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Shifts", "No on-call shifts.")
		return
	}
	var body strings.Builder
	for _, shift := range shifts {
		body.WriteString(onCallShiftSummary(shift))
		body.WriteString("\n\n")
	}
	r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Shifts", strings.TrimSpace(body.String()))
}

func (r *Router) onCallShow(msg platform.Message, args []string) {
	if len(args) == 0 {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: `/oncall show <shift-id>`")
		return
	}
	shift, ok := r.oncall.GetShift(args[0])
	if !ok {
		r.sendCommandResult(msg, platform.CardError, "⚠️ On-call Error", "Shift not found.")
		return
	}
	r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Shift", onCallShiftSummary(shift))
}

func (r *Router) onCallCheck(msg platform.Message, args []string) {
	if len(args) == 0 {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: `/oncall check <shift-id>`")
		return
	}
	id := args[0]
	r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Check", "Checking the current observation…")
	go func() {
		job, err := r.oncall.CheckShiftNow(context.Background(), id)
		if err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ On-call Error", err.Error())
			return
		}
		if job == nil {
			r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Check", "No change or alert.")
			return
		}
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Job", onCallJobSummary(*job))
	}()
}

func (r *Router) onCallShiftAction(msg platform.Message, action string, args []string) {
	if len(args) == 0 {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: `/oncall "+action+" <shift-id>`")
		return
	}
	id := args[0]
	switch action {
	case "pause":
		if _, err := r.oncall.PauseShift(id); err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ On-call Error", err.Error())
			return
		}
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Paused", id)
	case "resume":
		if _, err := r.oncall.ResumeShift(id); err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ On-call Error", err.Error())
			return
		}
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Resumed", id)
	case "stop":
		if _, err := r.oncall.StopShift(id); err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ On-call Error", err.Error())
			return
		}
		r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Stopped", id)
	case "delete":
		if err := r.oncall.DeleteShift(id); err != nil {
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
	shiftID := ""
	since := time.Now().Add(-12 * time.Hour)
	if len(args) > 0 {
		if duration, err := time.ParseDuration(args[len(args)-1]); err == nil && duration > 0 {
			since = time.Now().Add(-duration)
			args = args[:len(args)-1]
		}
	}
	if len(args) > 0 {
		shiftID = args[0]
	}
	report := r.oncall.Report(shiftID, since)
	var body strings.Builder
	fmt.Fprintf(&body, "completed=%d failed=%d running=%d canceled=%d total=%d\n",
		report.Completed, report.Failed, report.Running, report.Canceled, report.Total)
	fmt.Fprintf(&body, "checks=%d incidents=%d agent_runs=%d\n\n",
		report.Shift.Checks, report.Incidents, report.Shift.AgentRuns)
	for _, job := range report.Jobs {
		body.WriteString(onCallJobSummary(job))
		body.WriteString("\n\n")
	}
	if report.Total == 0 {
		body.WriteString("No on-call jobs in this period.")
	}
	r.sendCommandResult(msg, platform.CardInfo, "🌙 On-call Report", strings.TrimSpace(body.String()))
}

func onCallShiftSummary(shift oncall.Shift) string {
	body := fmt.Sprintf("`%s` · %s · %s · every %s\n%s",
		shift.ID, shift.Status, shift.Source, shift.Every, shift.Objective)
	if !shift.EndAt.IsZero() {
		body += "\nEnds: " + shift.EndAt.In(time.Local).Format("2006-01-02 15:04 MST")
	}
	body += fmt.Sprintf("\nChecks: %d · Incidents: %d · Agent runs: %d/%d",
		shift.Checks, shift.Incidents, shift.AgentRuns, shift.MaxAgentRuns)
	if shift.LastSummary != "" {
		body += "\nLast: " + firstLine(shift.LastSummary)
	}
	if shift.LastError != "" {
		body += "\nError: " + firstLine(shift.LastError)
	}
	return body
}

func onCallJobSummary(job oncall.Job) string {
	body := fmt.Sprintf("`%s` · %s · %s", job.ID, job.Status, job.Source)
	if job.TriggerSummary != "" {
		body += "\n" + firstLine(job.TriggerSummary)
	}
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
