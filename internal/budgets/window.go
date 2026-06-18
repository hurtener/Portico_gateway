// Package budgets computes budget period windows and (later) enforces
// hierarchical spend limits. This file is the pure window math: given an
// instant and a period + alignment, it returns the window the instant falls
// into.
package budgets

import (
	"errors"
	"fmt"
	"time"
)

// Period is a budget reset cadence.
type Period string

const (
	Period1m Period = "1m" // minute
	Period1h Period = "1h" // hour
	Period1d Period = "1d" // day
	Period1w Period = "1w" // week
	Period1M Period = "1M" // month
	Period1Y Period = "1Y" // year
)

// Alignment selects how a period's window boundaries are computed.
type Alignment string

const (
	// AlignCalendar aligns to civil UTC calendar boundaries (start of minute /
	// hour / day 00:00 / ISO week Monday 00:00 / first-of-month / Jan 1).
	AlignCalendar Alignment = "calendar"
	// AlignRolling uses fixed-duration buckets anchored to the Unix epoch for
	// 1m/1h/1d/1w; 1M is a fixed 30-day bucket and 1Y a fixed 365-day bucket
	// (documented approximations — calendar alignment is the civil-correct one).
	AlignRolling Alignment = "rolling"
)

var (
	// ErrUnknownPeriod is returned when a period string is not one of the
	// supported cadences (1m/1h/1d/1w/1M/1Y).
	ErrUnknownPeriod = errors.New("budgets: unknown period")
	// ErrUnknownAlignment is returned when an alignment string is neither
	// "calendar" nor "rolling".
	ErrUnknownAlignment = errors.New("budgets: unknown alignment")
)

// Window is the time bucket an instant falls into.
type Window struct {
	Key      string    // stable identifier, unique per (period, alignment, bucket)
	Start    time.Time // inclusive start (UTC)
	ResetsAt time.Time // exclusive end == next window start (UTC)
}

// ParsePeriod validates and returns the typed Period for a string read from
// config or storage. Unknown values yield ErrUnknownPeriod.
func ParsePeriod(s string) (Period, error) {
	switch p := Period(s); p {
	case Period1m, Period1h, Period1d, Period1w, Period1M, Period1Y:
		return p, nil
	}
	return "", ErrUnknownPeriod
}

// ParseAlignment validates and returns the typed Alignment for a string read
// from config or storage. Unknown values yield ErrUnknownAlignment.
func ParseAlignment(s string) (Alignment, error) {
	switch a := Alignment(s); a {
	case AlignCalendar, AlignRolling:
		return a, nil
	}
	return "", ErrUnknownAlignment
}

// WindowFor returns the window that `now` falls into for the given period and
// alignment. `now` is always converted to UTC first. Pure + deterministic:
// it never calls time.Now; callers pass the instant explicitly.
func WindowFor(now time.Time, period Period, alignment Alignment) (Window, error) {
	now = now.UTC()
	switch alignment {
	case AlignCalendar:
		return calendarWindow(now, period)
	case AlignRolling:
		return rollingWindow(now, period)
	default:
		return Window{}, ErrUnknownAlignment
	}
}

// calendarWindow computes the civil-UTC calendar window for `now` (already
// UTC) under the given period. See the package doc for the exact boundary
// rules. Unknown periods yield ErrUnknownPeriod.
func calendarWindow(now time.Time, period Period) (Window, error) {
	switch period {
	case Period1m:
		start := now.Truncate(time.Minute)
		return Window{
			Key:      "cal:1m:" + start.Format("2006-01-02T15:04"),
			Start:    start,
			ResetsAt: start.Add(time.Minute),
		}, nil
	case Period1h:
		start := now.Truncate(time.Hour)
		return Window{
			Key:      "cal:1h:" + start.Format("2006-01-02T15"),
			Start:    start,
			ResetsAt: start.Add(time.Hour),
		}, nil
	case Period1d:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return Window{
			Key:      "cal:1d:" + start.Format("2006-01-02"),
			Start:    start,
			ResetsAt: start.AddDate(0, 0, 1),
		}, nil
	case Period1w:
		off := (int(now.Weekday()) + 6) % 7 // Monday=0 ... Sunday=6
		day00 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		start := day00.AddDate(0, 0, -off)
		return Window{
			Key:      "cal:1w:" + start.Format("2006-01-02"),
			Start:    start,
			ResetsAt: start.AddDate(0, 0, 7),
		}, nil
	case Period1M:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return Window{
			Key:      "cal:1M:" + start.Format("2006-01"),
			Start:    start,
			ResetsAt: start.AddDate(0, 1, 0),
		}, nil
	case Period1Y:
		start := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		return Window{
			Key:      "cal:1Y:" + start.Format("2006"),
			Start:    start,
			ResetsAt: start.AddDate(1, 0, 0),
		}, nil
	default:
		return Window{}, ErrUnknownPeriod
	}
}

// rollingWindow computes a fixed-duration bucket anchored to the Unix epoch
// (1970-01-01T00:00:00Z) for `now` (already UTC). 1M and 1Y use documented
// fixed 30-/365-day approximations; calendar alignment is the civil-correct
// one. Unknown periods yield ErrUnknownPeriod.
func rollingWindow(now time.Time, period Period) (Window, error) {
	dur, ok := rollingDuration(period)
	if !ok {
		return Window{}, ErrUnknownPeriod
	}
	secs := int64(dur.Seconds())
	bucket := now.Unix() / secs
	start := time.Unix(bucket*secs, 0).UTC()
	return Window{
		Key:      "roll:" + string(period) + ":" + fmt.Sprintf("%d", bucket),
		Start:    start,
		ResetsAt: start.Add(dur),
	}, nil
}

// rollingDuration maps a period to its fixed rolling duration. The second
// result is false for an unknown period.
func rollingDuration(period Period) (time.Duration, bool) {
	switch period {
	case Period1m:
		return time.Minute, true
	case Period1h:
		return time.Hour, true
	case Period1d:
		return 24 * time.Hour, true
	case Period1w:
		return 7 * 24 * time.Hour, true
	case Period1M:
		return 30 * 24 * time.Hour, true
	case Period1Y:
		return 365 * 24 * time.Hour, true
	default:
		return 0, false
	}
}
