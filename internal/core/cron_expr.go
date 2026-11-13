package core

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronSpec is a parsed five-field cron expression. It intentionally supports
// only the standard fields (minute hour day-of-month month day-of-week); the
// scheduler runs its own loop so it does not depend on a cron runner.
type cronSpec struct {
	minute uint64
	hour   uint64
	dom    uint64
	month  uint64
	dow    uint64
	domAny bool
	dowAny bool
	loc    *time.Location
}

var cronMonthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

var cronWeekdayNames = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

var cronAliases = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

func parseCronExpression(expr string) (*cronSpec, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty cron expression")
	}

	var loc *time.Location
	if prefix := strings.ToUpper(fields[0]); strings.HasPrefix(prefix, "CRON_TZ=") || strings.HasPrefix(prefix, "TZ=") {
		name := strings.TrimSpace(fields[0][strings.Index(fields[0], "=")+1:])
		if name == "" {
			return nil, fmt.Errorf("cron timezone is empty")
		}
		loaded, err := time.LoadLocation(name)
		if err != nil {
			return nil, fmt.Errorf("invalid cron timezone %q: %w", name, err)
		}
		loc = loaded
		fields = fields[1:]
	}

	if len(fields) == 1 {
		if alias, ok := cronAliases[strings.ToLower(fields[0])]; ok {
			fields = strings.Fields(alias)
		}
	}
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression must have 5 fields (minute hour day month weekday)")
	}

	minute, _, err := parseCronField(fields[0], 0, 59, nil)
	if err != nil {
		return nil, fmt.Errorf("cron minute: %w", err)
	}
	hour, _, err := parseCronField(fields[1], 0, 23, nil)
	if err != nil {
		return nil, fmt.Errorf("cron hour: %w", err)
	}
	dom, domAny, err := parseCronField(fields[2], 1, 31, nil)
	if err != nil {
		return nil, fmt.Errorf("cron day-of-month: %w", err)
	}
	month, _, err := parseCronField(fields[3], 1, 12, cronMonthNames)
	if err != nil {
		return nil, fmt.Errorf("cron month: %w", err)
	}
	dow, dowAny, err := parseCronField(fields[4], 0, 7, cronWeekdayNames)
	if err != nil {
		return nil, fmt.Errorf("cron day-of-week: %w", err)
	}

	return &cronSpec{
		minute: minute,
		hour:   hour,
		dom:    dom,
		month:  month,
		dow:    dow,
		domAny: domAny,
		dowAny: dowAny,
		loc:    loc,
	}, nil
}

func parseCronField(raw string, min, max int, names map[string]int) (uint64, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, fmt.Errorf("empty field")
	}
	var bits uint64
	any := raw == "*" || raw == "?"
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return 0, false, fmt.Errorf("empty list item")
		}

		step := 1
		hadStep := false
		if idx := strings.Index(part, "/"); idx >= 0 {
			stepText := strings.TrimSpace(part[idx+1:])
			parsed, err := strconv.Atoi(stepText)
			if err != nil || parsed <= 0 {
				return 0, false, fmt.Errorf("invalid step %q", stepText)
			}
			step = parsed
			part = strings.TrimSpace(part[:idx])
			hadStep = true
		}

		var start, end int
		switch {
		case part == "*" || part == "?":
			start, end = min, max
		case strings.Contains(part, "-"):
			pieces := strings.SplitN(part, "-", 2)
			var err error
			start, err = cronValue(pieces[0], names)
			if err != nil {
				return 0, false, err
			}
			end, err = cronValue(pieces[1], names)
			if err != nil {
				return 0, false, err
			}
			if start > end {
				return 0, false, fmt.Errorf("range start %d is after end %d", start, end)
			}
		default:
			value, err := cronValue(part, names)
			if err != nil {
				return 0, false, err
			}
			start, end = value, value
			if hadStep {
				end = max
			}
		}
		if start < min || end > max {
			return 0, false, fmt.Errorf("value out of range %d-%d", min, max)
		}
		for value := start; value <= end; value += step {
			bits |= cronBit(value, min, max)
		}
	}
	return bits, any, nil
}

func cronValue(raw string, names map[string]int) (int, error) {
	raw = strings.TrimSpace(raw)
	if value, ok := names[strings.ToLower(raw)]; ok {
		return value, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", raw)
	}
	return value, nil
}

func cronBit(value, min, max int) uint64 {
	if max == 7 && value == 7 {
		value = 0
	}
	return uint64(1) << uint(value-min)
}

func (c *cronSpec) next(after time.Time) (time.Time, error) {
	loc := c.loc
	if loc == nil {
		loc = after.Location()
	}
	candidate := after.In(loc).Truncate(time.Minute).Add(time.Minute)
	limit := candidate.AddDate(8, 0, 0)
	for ; candidate.Before(limit); candidate = candidate.Add(time.Minute) {
		if c.matches(candidate) {
			return candidate, nil
		}
	}
	return time.Time{}, fmt.Errorf("cron expression has no occurrence within 8 years")
}

func (c *cronSpec) nextN(after time.Time, count int) ([]time.Time, error) {
	if count <= 0 {
		return nil, nil
	}
	out := make([]time.Time, 0, count)
	cursor := after
	for len(out) < count {
		next, err := c.next(cursor)
		if err != nil {
			return out, err
		}
		out = append(out, next)
		cursor = next
	}
	return out, nil
}

func (c *cronSpec) matches(t time.Time) bool {
	if c.minute&(uint64(1)<<uint(t.Minute())) == 0 {
		return false
	}
	if c.hour&(uint64(1)<<uint(t.Hour())) == 0 {
		return false
	}
	if c.month&(uint64(1)<<uint(int(t.Month())-1)) == 0 {
		return false
	}

	domMatch := c.dom&(uint64(1)<<uint(t.Day()-1)) != 0
	dowMatch := c.dow&(uint64(1)<<uint(int(t.Weekday()))) != 0
	switch {
	case c.domAny && c.dowAny:
		return true
	case c.domAny:
		return dowMatch
	case c.dowAny:
		return domMatch
	default:
		// Standard cron treats day-of-month and day-of-week as OR when both
		// are restricted.
		return domMatch || dowMatch
	}
}

func parseEvery(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("interval is empty")
	}
	lower := strings.ToLower(value)
	if strings.HasSuffix(lower, "d") || strings.HasSuffix(lower, "w") {
		unit := lower[len(lower)-1]
		amount, err := strconv.ParseFloat(strings.TrimSpace(lower[:len(lower)-1]), 64)
		if err != nil || amount <= 0 {
			return 0, fmt.Errorf("invalid interval %q", value)
		}
		hours := amount * 24
		if unit == 'w' {
			hours *= 7
		}
		return time.Duration(hours * float64(time.Hour)), nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid interval %q: %w", value, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("interval must be positive")
	}
	return duration, nil
}

func nextEvery(anchor time.Time, every time.Duration, now time.Time) time.Time {
	if every <= 0 {
		return time.Time{}
	}
	if anchor.IsZero() || !anchor.Before(now) {
		if anchor.IsZero() {
			anchor = now
		}
		return anchor.Add(every)
	}
	elapsed := now.Sub(anchor)
	steps := elapsed/every + 1
	return anchor.Add(steps * every)
}
