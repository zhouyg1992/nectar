package oncall

import (
	"context"
	"fmt"
	"strings"
	"time"

	execguard "github.com/zhouyg1992/nectar/internal/exec"
	"github.com/zhouyg1992/nectar/internal/platform"
)

type Notifier interface {
	NotifyObservation(ctx context.Context, shift Shift, observation Observation) error
	NotifyJob(ctx context.Context, job Job) error
	NotifyReport(ctx context.Context, report Report) error
}

type PlatformNotifier struct {
	Platform platform.PlatformAdapter
}

func (n *PlatformNotifier) NotifyObservation(ctx context.Context, shift Shift, observation Observation) error {
	title := "🌙 On-call event"
	status := platform.CardInfo
	if observation.Alert {
		title = "⚠️ On-call alert"
		status = platform.CardError
	}
	body := fmt.Sprintf("**Objective**\n%s\n\n**Observed**\n%s", shift.Objective, observation.Summary)
	if observation.Details != "" {
		body += "\n\n```text\n" + truncate(observation.Details, 4000) + "\n```"
	}
	card := &platform.Card{
		Title:  title,
		Status: status,
		Body:   body,
		Footer: shift.ID + " · " + shift.Source + " · " + shift.Every,
	}
	return n.send(ctx, replyTargetShift(shift), card, body)
}

func (n *PlatformNotifier) NotifyJob(ctx context.Context, job Job) error {
	title := "🍯 On-call job complete"
	status := platform.CardComplete
	if job.Status == JobFailed {
		title = "⚠️ On-call job failed"
		status = platform.CardError
	} else if job.Status == JobCanceled {
		title = "⚪ On-call job canceled"
		status = platform.CardInfo
	}
	body := jobCardBody(job)
	card := &platform.Card{
		Title:  title,
		Status: status,
		Body:   body,
		Footer: fmt.Sprintf("%s · %s · %s", job.ID, job.Autonomy, formatDuration(job.StartedAt, job.FinishedAt)),
	}
	return n.send(ctx, replyTargetJob(job), card, body)
}

func (n *PlatformNotifier) NotifyReport(ctx context.Context, report Report) error {
	var body strings.Builder
	if report.Shift.Objective != "" {
		fmt.Fprintf(&body, "**Objective**\n%s\n\n", report.Shift.Objective)
	}
	fmt.Fprintf(&body, "✅ %d completed · ⚠️ %d failed · ▶️ %d running · ⚪ %d canceled\n",
		report.Completed, report.Failed, report.Running, report.Canceled)
	fmt.Fprintf(&body, "Checks: %d · Incidents: %d · Agent runs: %d\n",
		report.Shift.Checks, report.Incidents, report.Shift.AgentRuns)
	if len(report.Jobs) == 0 {
		body.WriteString("\nNo on-call jobs in this period.")
	} else {
		body.WriteString("\n")
		for _, job := range report.Jobs {
			line := fmt.Sprintf("- `%s` %s", job.ID, statusLabel(job.Status))
			if job.TriggerSummary != "" {
				line += " — " + firstLine(job.TriggerSummary)
			}
			body.WriteString(line + "\n")
		}
	}
	card := &platform.Card{
		Title:  "🌙 On-call report",
		Status: platform.CardInfo,
		Body:   strings.TrimSpace(body.String()),
		Footer: report.Shift.ID + " · " + report.Shift.Source,
	}
	return n.send(ctx, replyTargetShift(report.Shift), card, strings.TrimSpace(body.String()))
}

func (n *PlatformNotifier) send(ctx context.Context, reply platform.Reply, card *platform.Card, fallback string) error {
	if n.Platform.Capabilities().Card {
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

func replyTargetShift(shift Shift) platform.Reply {
	return platform.Reply{
		ChatID:        shift.ChatID,
		ChatType:      shift.ChatType,
		UserID:        shift.UserID,
		ReplyTo:       shift.MessageID,
		ThreadID:      shift.ThreadID,
		ReplyInThread: shift.ThreadID != "" && shift.MessageID != "",
	}
}

func replyTargetJob(job Job) platform.Reply {
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
	fmt.Fprintf(&body, "**Objective**\n%s\n\n", job.Objective)
	if job.TriggerSummary != "" {
		fmt.Fprintf(&body, "**Observed**\n%s\n", job.TriggerSummary)
	}
	if job.Summary != "" {
		body.WriteString("\n")
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
		fmt.Fprintf(&body, "\n[Observed run](%s)", job.RunURL)
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

func truncate(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return execguard.Truncate(value, max) + "\n… (truncated)"
}
