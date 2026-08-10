package oc

import (
	"context"
	"io"
	"time"
)

type waitBudget struct {
	timeout  time.Duration
	deadline time.Time
}

func newWaitBudget(timeout string, startedAt time.Time) (waitBudget, error) {
	duration, err := parsePositiveDuration(timeout)
	if err != nil {
		return waitBudget{}, err
	}
	now := time.Now()
	if startedAt.IsZero() || startedAt.After(now) {
		startedAt = now
	}
	return waitBudget{timeout: duration, deadline: startedAt.Add(duration)}, nil
}

type waitCheck func(context.Context) (bool, string, error)
type waitTimeout func(context.Context, string, *waitProgress) (string, error)

func pollUntilReady(parent context.Context, budget waitBudget, pollInterval time.Duration, progress io.Writer, retryCheckErrors bool, check waitCheck, onTimeout waitTimeout) (string, error) {
	ctx, cancel := context.WithDeadline(parent, budget.deadline)
	defer cancel()
	if pollInterval <= 0 {
		pollInterval = WaitInterval(budget.timeout)
	}
	tracker := startWaitProgress(progress)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var last string
	for {
		select {
		case <-ctx.Done():
			return waitDeadlineResult(parent, onTimeout, last, tracker)
		default:
		}
		ready, detail, err := check(ctx)
		if detail != "" {
			last = detail
		} else if err != nil {
			last = err.Error()
		}
		select {
		case <-ctx.Done():
			return waitDeadlineResult(parent, onTimeout, last, tracker)
		default:
		}
		if err != nil && !retryCheckErrors {
			return last, err
		}
		if ready && err == nil {
			tracker.done(detail)
			return detail, nil
		}
		tracker.observe(currentObservation(last))
		select {
		case <-ctx.Done():
			return waitDeadlineResult(parent, onTimeout, last, tracker)
		case <-ticker.C:
		}
	}
}

func waitDeadlineResult(parent context.Context, onTimeout waitTimeout, last string, tracker *waitProgress) (string, error) {
	if parent.Err() != nil {
		return last, parent.Err()
	}
	diagnosisCtx, cancel := context.WithTimeout(parent, diagnosisBudget)
	defer cancel()
	observed, err := onTimeout(diagnosisCtx, last, tracker)
	if parent.Err() != nil {
		return last, parent.Err()
	}
	return observed, err
}
