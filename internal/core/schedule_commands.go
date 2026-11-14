package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/platform"
)

func isScheduleCommand(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return lower == "/cron" || lower == "/schedule" ||
		strings.HasPrefix(lower, "/cron ") || strings.HasPrefix(lower, "/schedule ")
}

func (r *Router) handleScheduleCommand(key SessionKey, msg platform.Message, text string) {
	if r.scheduler == nil {
		r.sendCommandResult(msg, platform.CardInfo, "🗓 Scheduling Disabled", "Scheduled tasks are disabled in config.")
		return
	}
	if !r.isAdmin(msg.UserID) {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Admin Only", "Only configured admin users can manage scheduled tasks.")
		return
	}

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return
	}
	args := fields[1:]
	sub := "list"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
		args = args[1:]
	}

	switch sub {
	case "list", "ls":
		includeAll := len(args) > 0 && strings.EqualFold(args[0], "all")
		tasks := r.scheduler.List(includeAll)
		if len(tasks) == 0 {
			r.sendCommandResult(msg, platform.CardInfo, "🗓 Scheduled Tasks", "No scheduled tasks.")
			return
		}
		var body strings.Builder
		for _, task := range tasks {
			body.WriteString(formatScheduleTask(task))
			body.WriteString("\n\n")
		}
		r.sendCommandResult(msg, platform.CardInfo, "🗓 Scheduled Tasks", strings.TrimSpace(body.String()))
	case "help", "?":
		r.sendCommandResult(msg, platform.CardInfo, "🗓 Schedule Commands", scheduleHelpText)
	case "add", "cron":
		if len(args) < 6 {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /cron add <min> <hour> <day> <month> <weekday> <prompt>")
			return
		}
		req := ScheduleRequest{
			Mode:     "cron",
			Cron:     strings.Join(args[:5], " "),
			Prompt:   strings.Join(args[5:], " "),
			Channel:  key.Channel,
			ChatID:   key.ChatID,
			ThreadID: key.ThreadID,
			AgentID:  key.AgentID,
		}
		r.addScheduledTask(msg, req)
	case "every":
		if len(args) < 2 {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /cron every <duration> <prompt>")
			return
		}
		r.addScheduledTask(msg, ScheduleRequest{
			Mode:     "every",
			Every:    args[0],
			Prompt:   strings.Join(args[1:], " "),
			Channel:  key.Channel,
			ChatID:   key.ChatID,
			ThreadID: key.ThreadID,
			AgentID:  key.AgentID,
		})
	case "in":
		if len(args) < 2 {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /cron in <duration> <prompt>")
			return
		}
		r.addScheduledTask(msg, ScheduleRequest{
			Mode:     "in",
			In:       args[0],
			Prompt:   strings.Join(args[1:], " "),
			Channel:  key.Channel,
			ChatID:   key.ChatID,
			ThreadID: key.ThreadID,
			AgentID:  key.AgentID,
		})
	case "at":
		if len(args) < 2 {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /cron at <RFC3339-time> <prompt>")
			return
		}
		r.addScheduledTask(msg, ScheduleRequest{
			Mode:     "at",
			At:       args[0],
			Prompt:   strings.Join(args[1:], " "),
			Channel:  key.Channel,
			ChatID:   key.ChatID,
			ThreadID: key.ThreadID,
			AgentID:  key.AgentID,
		})
	case "pause", "resume", "run", "cancel", "stop", "delete", "del", "rm":
		if len(args) < 1 {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /cron "+sub+" <id>")
			return
		}
		id := args[0]
		var err error
		switch sub {
		case "pause":
			err = r.scheduler.Pause(id)
		case "resume":
			err = r.scheduler.Resume(id)
		case "run":
			err = r.scheduler.RunNow(id)
		case "cancel", "stop":
			err = r.scheduler.Cancel(id)
		default:
			err = r.scheduler.Delete(id)
		}
		if err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Schedule Error", err.Error())
			return
		}
		r.sendCommandResult(msg, platform.CardInfo, "🗓 Schedule Updated", fmt.Sprintf("%s %s", id, sub))
	case "update":
		r.handleScheduleUpdate(msg, args)
	default:
		r.sendCommandResult(msg, platform.CardError, "⚠️ Unknown Command", scheduleHelpText)
	}
}

func (r *Router) handleScheduleUpdate(msg platform.Message, args []string) {
	if len(args) < 2 {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /cron update <id> <prompt|cron|every|at|timezone> <value...>")
		return
	}
	id, field := args[0], strings.ToLower(args[1])
	patch := SchedulePatch{}
	switch field {
	case "prompt":
		patch.Prompt = strings.Join(args[2:], " ")
	case "timezone":
		if len(args) < 3 {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /cron update <id> timezone <IANA-timezone>")
			return
		}
		patch.Timezone = args[2]
	case "timeout":
		if len(args) < 3 {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /cron update <id> timeout <duration|0s>")
			return
		}
		patch.Timeout = args[2]
	case "cron":
		if len(args) < 7 {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /cron update <id> cron <min> <hour> <day> <month> <weekday> [prompt]")
			return
		}
		patch.Mode = "cron"
		patch.Cron = strings.Join(args[2:7], " ")
		if len(args) > 7 {
			patch.Prompt = strings.Join(args[7:], " ")
		}
	case "every":
		if len(args) < 3 {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /cron update <id> every <duration> [prompt]")
			return
		}
		patch.Mode = "every"
		patch.Every = args[2]
		if len(args) > 3 {
			patch.Prompt = strings.Join(args[3:], " ")
		}
	case "at":
		if len(args) < 3 {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /cron update <id> at <RFC3339-time> [prompt]")
			return
		}
		patch.Mode = "at"
		patch.At = args[2]
		if len(args) > 3 {
			patch.Prompt = strings.Join(args[3:], " ")
		}
	default:
		r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", scheduleHelpText)
		return
	}
	if _, err := r.scheduler.Update(id, patch); err != nil {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Schedule Error", err.Error())
		return
	}
	r.sendCommandResult(msg, platform.CardInfo, "🗓 Schedule Updated", id)
}

func (r *Router) addScheduledTask(msg platform.Message, req ScheduleRequest) {
	task, err := r.scheduler.Add(req, msg.UserID)
	if err != nil {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Schedule Error", err.Error())
		return
	}
	r.sendCommandResult(msg, platform.CardInfo, "🗓 Task Scheduled", formatScheduleTask(*task))
}

func (r *Router) isAdmin(userID string) bool {
	if len(r.cfg.AdminUsers) == 0 {
		return true
	}
	for _, admin := range r.cfg.AdminUsers {
		if strings.TrimSpace(admin) == userID {
			return true
		}
	}
	return false
}

func formatScheduleTask(task ScheduledTask) string {
	status := string(task.Status)
	if task.Running {
		status = "running"
	}
	if task.Status == SchedulePaused && task.Mode == ScheduleOnce && task.RemainingSeconds > 0 {
		status += fmt.Sprintf(" (%s remaining)", time.Duration(task.RemainingSeconds)*time.Second)
	}
	lines := []string{
		fmt.Sprintf("`%s` · %s · %s", task.ID, status, scheduleWindow(task)),
		fmt.Sprintf("Prompt: %s", task.Prompt),
	}
	if !task.NextRun.IsZero() {
		lines = append(lines, "Next: "+formatScheduleTime(task.NextRun, task.Timezone))
	}
	if task.Timeout != "" {
		lines = append(lines, "Timeout: "+task.Timeout)
	}
	if !task.LastRun.IsZero() {
		lines = append(lines, "Last: "+formatScheduleTime(task.LastRun, task.Timezone))
	}
	if task.LastError != "" {
		lines = append(lines, "Last error: "+task.LastError)
	}
	return strings.Join(lines, "\n")
}

func scheduleWindow(task ScheduledTask) string {
	switch task.Mode {
	case ScheduleCron:
		return "cron " + task.CronExpr
	case ScheduleEvery:
		return "every " + task.Every
	case ScheduleOnce:
		if !task.ExecuteAt.IsZero() {
			return "once at " + formatScheduleTime(task.ExecuteAt, task.Timezone)
		}
		return "once"
	default:
		return string(task.Mode)
	}
}

func formatScheduleTime(value time.Time, timezone string) string {
	loc, err := scheduleLocation(timezone)
	if err != nil {
		return value.Format(time.RFC3339)
	}
	return value.In(loc).Format("2006-01-02 15:04 MST")
}

const scheduleHelpText = "**Schedule**\n" +
	"- `/cron list [all]` — List tasks\n" +
	"- `/cron add <min> <hour> <day> <month> <weekday> <prompt>` — Add cron task\n" +
	"- `/cron every <duration> <prompt>` — Fixed interval\n" +
	"- `/cron in <duration> <prompt>` — One-shot delay\n" +
	"- `/cron at <RFC3339> <prompt>` — One-shot time\n" +
	"- `/cron pause|resume|run|cancel|delete <id>` — Manage a task\n" +
	"- `/cron update <id> prompt|cron|every|at|timezone|timeout ...` — Update a task"
