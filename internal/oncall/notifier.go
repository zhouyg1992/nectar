package oncall

import (
	"context"
	"fmt"
	"strings"
	"time"

	execguard "github.com/zhouyg1992/nectar/internal/exec"
	"github.com/zhouyg1992/nectar/internal/platform"
)

// Notifier sends job and digest cards back to the chat that owns the watch.
type Notifier interface {
	NotifyJob(ctx context.Context, job Job) error
	NotifyReport(ctx context.Context, watch Watch, report Report) error
}

type PlatformNotifier struct {
	Platform platform.PlatformAdapter
	BotName  string
}

func (n *PlatformNotifier) NotifyJob(ctx context.Context, job Job) error {
	title := "🍯 On-call complete"
	status := platform.CardComplete
	if job.Status == JobFailed {
		title = "⚠️ On-call failed"
		status = platform.CardError
	} else if job.Status == JobCanceled {
		title = "⚪ On-call canceled"
		status = platform.CardInfo
	}
	body := jobCardBody(job)
	card := &platform.Card{
		Title:  title,
		Status: status,
		Body:   body,
		Footer: fmt.Sprintf("%s · %s · %s", job.ID, job.Autonomy, formatDuration(job.StartedAt, job.FinishedAt)),
	}
	return n.send(ctx, ReplyTargetFromJob(job), card, body)
}

func (n *PlatformNotifier) NotifyReport(ctx context.Context, watch Watch, report Report) error {
	var body strings.Builder
	fmt.Fprintf(&body, "**%s** · `%s`\n\n", watch.Repo, watch.Branch)
	fmt.Fprintf(&body, "✅ %d completed · ⚠️ %d failed · ▶️ %d running · ⚪ %d canceled\n",
		report.Completed, report.Failed, report.Running, report.Canceled)
	if len(report.Jobs) == 0 {
		body.WriteString("\nNo on-call jobs in this period.")
	} else {
		body.WriteString("\n")
		for _, job := range report.Jobs {
			line := fmt.Sprintf("- `%s` %s", job.ID, statusLabel(job.Status))
			if job.Workflow != "" {
				line += " · " + job.Workflow
			}
			if job.Summary != "" {
				line += " — " + firstLine(job.Summary)
			}
			body.WriteString(line + "\n")
		}
	}
	card := &platform.Card{
		Title:  "🌙 Nectar on-call report",
		Status: platform.CardInfo,
		Body:   strings.TrimSpace(body.String()),
		Footer: "Deterministic job summary from Nectar",
	}
	return n.send(ctx, ReplyTargetFromWatch(watch), card, strings.TrimSpace(body.String()))
}

func (n *PlatformNotifier) send(ctx context.Context, reply platform.Reply, card *platform.Card, fallback string) error {
	caps := n.Platform.Capabilities()
	if caps.Card {
		reply.Card = card
		if _, err := n.Platform.Send(ctx, reply); err == nil {
			return nil
		}
	}
	reply.Card = nil
	reply.Text = fallback
	_, err := n.Platform.Send(ctx, reply)
	return err
}

func ReplyTargetFromWatch(watch Watch) platform.Reply {
	return platform.Reply{
		ChatID:        watch.ChatID,
		ChatType:      watch.ChatType,
		UserID:        watch.UserID,
		ReplyTo:       watch.MessageID,
		ThreadID:      watch.ThreadID,
		ReplyInThread: watch.ThreadID != "" && watch.MessageID != "",
	}
}

func ReplyTargetFromJob(job Job) platform.Reply {
	return platform.Reply{
		ChatID:        job.ChatID,
		ChatType:      job.ChatType,
		UserID:        job.UserID,
		ReplyTo:       job.MessageID,
		ThreadID:      job.ThreadID,
		ReplyInThread: job.ThreadID != "" && job.MessageID != "",
	}
}

func jobCardBody(job Job) string {
	var body strings.Builder
	fmt.Fprintf(&body, "**%s** · `%s` · %s\n\n", job.Repo, job.Branch, job.Workflow)
	if job.Summary != "" {
		body.WriteString(job.Summary)
		body.WriteString("\n")
	}
	if job.Error != "" {
		body.WriteString("\n**Error**\n```text\n")
		body.WriteString(truncate(job.Error, 3000))
		body.WriteString("\n```\n")
	}
	if job.Diff != "" {
		body.WriteString("\n**Changes**\n")
		body.WriteString(truncate(job.Diff, 12000))
	}
	if job.RunURL != "" {
		fmt.Fprintf(&body, "\n[Failed run](%s)", job.RunURL)
	}
	if job.ArtifactDir != "" {
		fmt.Fprintf(&body, " · `%s`", job.ArtifactDir)
	}
	return strings.TrimSpace(body.String())
}

func formatDuration(start time.Time, finish *time.Time) string {
	if start.IsZero() {
		return "not started"
	}
	end := time.Now()
	if finish != nil {
		end = *finish
	}
	if end.Before(start) {
		end = start
	}
	return end.Sub(start).Round(time.Second).String()
}

func statusLabel(status JobStatus) string {
	switch status {
	case JobCompleted:
		return "completed"
	case JobFailed:
		return "failed"
	case JobRunning:
		return "running"
	case JobQueued:
		return "queued"
	case JobCanceled:
		return "canceled"
	default:
		return string(status)
	}
}

func firstLine(value string) string {
	line := strings.TrimSpace(value)
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	return truncate(strings.TrimSpace(line), 180)
}

func truncate(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return execguard.Truncate(value, max) + "\n… (truncated)"
}
