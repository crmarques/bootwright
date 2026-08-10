package oc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPollUntilReadySeparatesParentCancellationFromTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget, err := newWaitBudget("1m", time.Time{})
	if err != nil {
		t.Fatalf("newWaitBudget: %v", err)
	}
	checks := 0
	timeouts := 0
	_, err = pollUntilReady(ctx, budget, time.Millisecond, nil, true,
		func(context.Context) (bool, string, error) {
			checks++
			return false, "pending", nil
		},
		func(context.Context, string, *waitProgress) (string, error) {
			timeouts++
			return "", errors.New("timeout")
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pollUntilReady error = %v, want context cancellation", err)
	}
	if checks != 0 || timeouts != 0 {
		t.Fatalf("canceled poll ran checks=%d timeout callbacks=%d", checks, timeouts)
	}
}

func TestPollUntilReadyCanRejectTerminalCheckError(t *testing.T) {
	budget, err := newWaitBudget("1m", time.Time{})
	if err != nil {
		t.Fatalf("newWaitBudget: %v", err)
	}
	want := errors.New("invalid check")
	timeouts := 0
	_, err = pollUntilReady(context.Background(), budget, time.Millisecond, nil, false,
		func(context.Context) (bool, string, error) {
			return false, "", want
		},
		func(context.Context, string, *waitProgress) (string, error) {
			timeouts++
			return "", errors.New("timeout")
		})
	if !errors.Is(err, want) || timeouts != 0 {
		t.Fatalf("pollUntilReady error=%v timeout callbacks=%d, want terminal check error", err, timeouts)
	}
}

func TestPollUntilReadyCancellationWinsDuringCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	budget, err := newWaitBudget("1m", time.Time{})
	if err != nil {
		t.Fatalf("newWaitBudget: %v", err)
	}
	_, err = pollUntilReady(ctx, budget, time.Millisecond, nil, false,
		func(context.Context) (bool, string, error) {
			cancel()
			return true, "Ready", errors.New("terminal")
		},
		func(context.Context, string, *waitProgress) (string, error) {
			return "", errors.New("timeout")
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pollUntilReady error = %v, want cancellation to win over check result", err)
	}
}

func TestPollUntilReadyCancellationWinsDuringDiagnosis(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	budget, err := newWaitBudget("1ms", time.Now().Add(-time.Second))
	if err != nil {
		t.Fatalf("newWaitBudget: %v", err)
	}
	_, err = pollUntilReady(ctx, budget, time.Millisecond, nil, true,
		func(context.Context) (bool, string, error) {
			return false, "pending", nil
		},
		func(context.Context, string, *waitProgress) (string, error) {
			cancel()
			return "pending", errors.New("timeout")
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pollUntilReady error = %v, want cancellation to win over diagnosis", err)
	}
}

func TestCoreReadinessPollLoopsStayCentralized(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || filepath.Base(file) == "poll.go" {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if strings.Contains(string(data), "time.NewTicker(") {
			t.Fatalf("%s implements a second polling loop; route it through pollUntilReady", file)
		}
	}
}

func TestNewWaitBudgetDoesNotTrustFutureStartTime(t *testing.T) {
	before := time.Now()
	budget, err := newWaitBudget("1m", before.Add(time.Hour))
	if err != nil {
		t.Fatalf("newWaitBudget: %v", err)
	}
	if budget.deadline.After(before.Add(61 * time.Second)) {
		t.Fatalf("future StartedAt widened the readiness deadline to %s", budget.deadline)
	}
}
