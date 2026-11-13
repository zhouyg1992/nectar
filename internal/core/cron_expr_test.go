package core

import (
	"testing"
	"time"
)

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestCronNextWeekday(t *testing.T) {
	loc := mustLocation(t, "Asia/Shanghai")
	spec, err := parseCronExpression("0 9 * * 1-5")
	if err != nil {
		t.Fatal(err)
	}
	after := time.Date(2026, 11, 13, 10, 0, 0, 0, loc) // Friday
	next, err := spec.next(after)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 16, 9, 0, 0, 0, loc) // Monday
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}

func TestCronDayOfMonthOrWeekday(t *testing.T) {
	loc := time.UTC
	spec, err := parseCronExpression("0 0 1 * 1")
	if err != nil {
		t.Fatal(err)
	}
	after := time.Date(2026, 11, 1, 0, 0, 1, 0, loc)
	next, err := spec.next(after)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 2, 0, 0, 0, 0, loc) // Monday before Dec 1
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}

func TestCronTimezonePrefix(t *testing.T) {
	spec, err := parseCronExpression("CRON_TZ=Asia/Shanghai 0 9 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if spec.loc == nil || spec.loc.String() != "Asia/Shanghai" {
		t.Fatalf("location = %v", spec.loc)
	}
}

func TestCronAliasAndSteps(t *testing.T) {
	spec, err := parseCronExpression("@daily")
	if err != nil {
		t.Fatal(err)
	}
	after := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	next, err := spec.next(after)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}

	every30, err := parseCronExpression("*/30 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	runs, err := every30.nextN(after, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || !runs[0].Equal(time.Date(2026, 11, 13, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("runs = %v", runs)
	}
}

func TestCronRejectsInvalid(t *testing.T) {
	for _, expr := range []string{"", "* * * *", "60 * * * *", "0 25 * * *", "abc * * * *"} {
		if _, err := parseCronExpression(expr); err == nil {
			t.Fatalf("expected %q to fail", expr)
		}
	}
}

func TestParseEvery(t *testing.T) {
	cases := map[string]time.Duration{
		"90m": 90 * time.Minute,
		"2h":  2 * time.Hour,
		"1d":  24 * time.Hour,
		"2w":  14 * 24 * time.Hour,
	}
	for value, want := range cases {
		got, err := parseEvery(value)
		if err != nil {
			t.Fatalf("parseEvery(%q): %v", value, err)
		}
		if got != want {
			t.Fatalf("parseEvery(%q) = %s, want %s", value, got, want)
		}
	}
}

func TestNextEveryKeepsAnchor(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	anchor := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	next := nextEvery(anchor, 90*time.Minute, now)
	want := time.Date(2026, 11, 13, 13, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}
