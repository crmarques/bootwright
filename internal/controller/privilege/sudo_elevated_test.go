package privilege

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"
)

// Only the elevated child bounds its own cancellation; the policy probe and
// each refresh are bounded calls whose executor keeps their kill deadline.
func TestOnlyTheElevatedChildIsRunWithoutADeadline(t *testing.T) {
	var lock sync.Mutex
	elevated := map[string]bool{}
	refreshed, active := make(chan struct{}), make(chan struct{})
	record := func(command string, value bool) {
		lock.Lock()
		defer lock.Unlock()
		elevated[command] = value
	}
	executor := executorFunc(func(ctx context.Context, c Command) (int, error) {
		switch {
		case slices.Equal(c.Arguments, []string{"-n", "-u", "#0", "-ll"}):
			record("probe", c.Elevated)
			return 1, nil
		case slices.Equal(c.Arguments, []string{"-n", "-u", "#0", "-v"}):
			record("refresh", c.Elevated)
			close(refreshed)
			return 1, nil
		}
		record("child", c.Elevated)
		close(active)
		<-refreshed
		return 0, nil
	})
	delay := delayFunc(func(ctx context.Context, _ time.Duration) error {
		select {
		case <-active:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	supervisor := NewSupervisor(SudoOptions{Executable: "/opt/bootwright", Sudo: "/usr/bin/sudo", Executor: executor, Delay: delay, NonInteractive: true, Quiet: true})
	if code, err := supervisor.Run(context.Background(), []string{"status"}); code != 0 || err != nil {
		t.Fatalf("result %d, %v", code, err)
	}
	lock.Lock()
	defer lock.Unlock()
	want := map[string]bool{"probe": false, "child": true, "refresh": false}
	if len(elevated) != len(want) || elevated["probe"] || !elevated["child"] || elevated["refresh"] {
		t.Fatalf("elevated = %v, want %v", elevated, want)
	}
}
