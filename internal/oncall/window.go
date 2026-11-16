package oncall

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Window is a local-time daily window. An empty window means all day.
type Window struct {
	raw   string
	start int
	end   int
}

func ParseWindow(value string) (Window, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Window{}, nil
	}
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return Window{}, fmt.Errorf("invalid on-call window %q (use HH:MM-HH:MM)", value)
	}
	start, err := parseClock(parts[0])
	if err != nil {
		return Window{}, fmt.Errorf("invalid on-call window start %q: %w", parts[0], err)
	}
	end, err := parseClock(parts[1])
	if err != nil {
		return Window{}, fmt.Errorf("invalid on-call window end %q: %w", parts[1], err)
	}
	if start == end {
		return Window{}, fmt.Errorf("on-call window start and end must differ")
	}
	return Window{raw: value, start: start, end: end}, nil
}

func parseClock(value string) (int, error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("use HH:MM")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("hour must be 00-23")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("minute must be 00-59")
	}
	return hour*60 + minute, nil
}

func (w Window) String() string { return w.raw }

func (w Window) Empty() bool { return w.raw == "" }

// Contains reports whether t's local clock is inside the window. Windows that
// cross midnight, such as 22:00-08:00, are supported.
func (w Window) Contains(t time.Time) bool {
	if w.Empty() {
		return true
	}
	minute := t.Hour()*60 + t.Minute()
	if w.start < w.end {
		return minute >= w.start && minute < w.end
	}
	return minute >= w.start || minute < w.end
}
