package scheduler

import (
	"fmt"
	"time"

	cron "github.com/netresearch/go-cron"
)

// parser is a pre-configured cron expression parser that supports standard
// five-field specs and descriptors like "@every 1m", with a minimum interval
// of 10 seconds.
var parser = cron.MustNewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor).WithMinEveryInterval(10 * time.Second)

// minCheckInterval is the minimum allowed duration between consecutive
// resource checks.
const minCheckInterval = 10 * time.Second

// ParseCheckInterval parses a cron spec (standard or @every) and returns the Schedule.
func ParseCheckInterval(spec string) (cron.Schedule, error) {
	s, err := parser.Parse(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to parse check interval %q: %w", spec, err)
	}
	return s, nil
}

// ValidateCheckInterval checks that the spec is valid and that the interval is >= 10s.
func ValidateCheckInterval(spec string) error {
	s, err := ParseCheckInterval(spec)
	if err != nil {
		return err
	}

	// Check that the interval between executions is at least 10s
	t := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	next := s.Next(t)
	if next.Sub(t) < minCheckInterval {
		return fmt.Errorf("check interval %q is too short, minimum is %s", spec, minCheckInterval)
	}

	return nil
}

// ComputeNextCheck computes the next check time from the given spec and reference time.
func ComputeNextCheck(spec string, from time.Time) (time.Time, error) {
	s, err := ParseCheckInterval(spec)
	if err != nil {
		return time.Time{}, err
	}
	return s.Next(from), nil
}
