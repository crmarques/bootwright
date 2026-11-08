package capacity_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/storage/preflightlocal"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type remoteAnswers map[string]string

func (r remoteAnswers) Run(_ context.Context, _, _ string, argv []string, bound int) ([]byte, int, error) {
	if answer, found := r[strings.Join(argv, " ")]; found && len(answer) <= bound {
		return []byte(answer), 0, nil
	}
	return nil, 0, errors.New("unanswered")
}

func remoteBudget(answers remoteAnswers) capacity.Budget {
	return capacity.Budget{
		Observer: preflightlocal.Observer{Remote: answers},
		Reserves: func(capacity.Target) capacity.Reserve { return capacity.Reserve{Configured: true} },
	}
}

func completeAnswers() remoteAnswers {
	return remoteAnswers{
		"/usr/bin/nproc":             "8\n",
		"/usr/bin/cat /proc/meminfo": "MemAvailable: 1024 kB\n",
		"/usr/bin/stat --file-system --format=%a %S %i -- /pool":                                   "10 1024 ab\n",
		"/usr/bin/find /etc/libvirt/qemu /run/libvirt/qemu -maxdepth 1 -type f -name *.xml -print": "",
	}
}

func remoteAllocation() capacity.Allocation {
	return capacity.Allocation{Machine: "guest", Host: "host", Pool: "/pool", Context: "lab", Domain: "guest", UUID: "u1", Quantities: capacity.Quantities{CPU: 2, Memory: 1 << 20, Disk: 10 << 10}}
}

func TestRemoteTargetWithCompleteFactsPassesAndAMissingFactStaysUnknown(t *testing.T) {
	checks, err := remoteBudget(completeAnswers()).Check(context.Background(), []capacity.Allocation{remoteAllocation()}, nil, false)
	if err != nil || len(checks) != 3 {
		t.Fatalf("%+v %v", checks, err)
	}
	for _, check := range checks {
		if check.Status != capacity.Passed {
			t.Fatalf("%+v", check)
		}
	}
	for command := range completeAnswers() {
		answers := completeAnswers()
		delete(answers, command)
		checks, err := remoteBudget(answers).Check(context.Background(), []capacity.Allocation{remoteAllocation()}, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		unknown := 0
		for _, check := range checks {
			if check.Status == capacity.Passed {
				continue
			}
			if check.Status != capacity.Unknown {
				t.Fatalf("%s: %+v", command, check)
			}
			unknown++
		}
		if unknown == 0 {
			t.Fatalf("%s: every fact passed without it", command)
		}
	}
}

func TestRemoteTargetCarriesItsContextAndRefusesAMixedOne(t *testing.T) {
	var seen capacity.Target
	budget := capacity.Budget{Observer: observeFunc(func(_ context.Context, target capacity.Target) (capacity.Facts, error) {
		seen = target
		return capacity.Facts{}, nil
	})}
	other := remoteAllocation()
	other.Machine, other.UUID = "second", "u2"
	if _, err := budget.Check(context.Background(), []capacity.Allocation{remoteAllocation(), other}, nil, false); err != nil || seen.Context != "lab" || len(seen.Allocations) != 2 {
		t.Fatalf("%+v %v", seen, err)
	}
	other.Context = "elsewhere"
	if _, err := budget.Check(context.Background(), []capacity.Allocation{remoteAllocation(), other}, nil, false); err == nil {
		t.Fatal("one check spanned two contexts")
	}
}

type observeFunc func(context.Context, capacity.Target) (capacity.Facts, error)

func (f observeFunc) Observe(ctx context.Context, target capacity.Target) (capacity.Facts, error) {
	return f(ctx, target)
}

type deadlineObserver struct {
	lock    *sync.Mutex
	budgets map[bool]time.Duration
}

func (o deadlineObserver) Observe(ctx context.Context, target capacity.Target) (capacity.Facts, error) {
	deadline, _ := ctx.Deadline()
	o.lock.Lock()
	defer o.lock.Unlock()
	o.budgets[target.Local] = time.Until(deadline)
	return capacity.Facts{}, nil
}

// A host reached over its verified access opens one session per fixed command,
// so its observation has the remote budget; a local one keeps the short one.
func TestRemoteTargetObservesWithinTheRemoteProbeBudget(t *testing.T) {
	observer := deadlineObserver{lock: &sync.Mutex{}, budgets: map[bool]time.Duration{}}
	budget := capacity.Budget{Observer: observer, Reserves: func(capacity.Target) capacity.Reserve { return capacity.Reserve{Configured: true} }}
	allocations := []capacity.Allocation{
		{Machine: "local", Host: "controller", Pool: "/a", Local: true, Quantities: capacity.Quantities{CPU: 1}},
		{Machine: "remote", Host: "libvirt-host", Pool: "/b", Context: "lab", Quantities: capacity.Quantities{CPU: 1}},
	}
	if _, err := budget.Check(context.Background(), allocations, nil, false); err != nil {
		t.Fatal(err)
	}
	if local, remote := observer.budgets[true], observer.budgets[false]; local > capacity.ProbeTimeout || local <= 0 ||
		remote <= capacity.ProbeTimeout || remote > capacity.RemoteProbeTimeout {
		t.Fatalf("local %v remote %v", local, remote)
	}
}
