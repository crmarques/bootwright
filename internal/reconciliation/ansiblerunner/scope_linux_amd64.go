//go:build linux && amd64

package ansiblerunner

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type invocationKey struct{}

type jobOwnership struct{ lock, holder os.FileInfo }

type invocationScope struct {
	mu         sync.Mutex
	active     bool
	jobs       map[string]jobOwnership
	automation *stagedAutomation
}

// BeginInvocation binds live job ownership and staged automation to one
// execution and every copy of its runner; releasing it ends both.
func (Runner) BeginInvocation(ctx context.Context) (context.Context, func()) {
	scope := &invocationScope{active: true, jobs: make(map[string]jobOwnership)}
	ctx, cancel := context.WithCancel(context.WithValue(ctx, invocationKey{}, scope))
	return ctx, sync.OnceFunc(func() {
		scope.mu.Lock()
		defer scope.mu.Unlock()
		scope.active = false
		scope.jobs = nil
		if scope.automation != nil {
			scope.automation.release()
			scope.automation = nil
		}
		cancel()
	})
}

func (r Runner) owns(lock string, held holding) bool {
	if r.invocation == nil || !r.invocation.active || !held.alive {
		return false
	}
	owned, ok := r.invocation.jobs[filepath.Dir(lock)]
	return ok && os.SameFile(owned.lock, held.lock) && os.SameFile(owned.holder, held.holder)
}

// Admission and release share the scope lock so no sibling can see a published
// claim before registration, or a registered job after its holder closes.
func (r Runner) claimRun(request lifecycle.RunRequest) (string, *os.File, *os.File, error) {
	if r.invocation != nil {
		r.invocation.mu.Lock()
		defer r.invocation.mu.Unlock()
		if !r.invocation.active {
			return "", nil, nil, failure("lifecycle.state", "the lifecycle adapter invocation has ended", "")
		}
	}
	if err := r.ensureScratchParent(); err != nil {
		return "", nil, nil, err
	}
	if err := r.sweepLocked(request.Context); err != nil {
		return "", nil, nil, err
	}
	job, err := os.MkdirTemp(r.jobParent, jobPrefix)
	if err != nil {
		return "", nil, nil, failure("lifecycle.state", "private adapter invocation storage is unavailable", "")
	}
	if err := os.Chmod(job, 0700); err != nil {
		_ = os.RemoveAll(job)
		return "", nil, nil, failure("lifecycle.state", "private adapter invocation storage is unsafe", "")
	}
	lock, holder, err := claim(job, request)
	if err != nil {
		_ = os.RemoveAll(job)
		return "", nil, nil, err
	}
	if r.invocation != nil {
		lockIdentity, lockErr := lock.Stat()
		holderIdentity, holderErr := holder.Stat()
		if lockErr != nil || holderErr != nil {
			lock.Close()
			holder.Close()
			_ = os.RemoveAll(job)
			return "", nil, nil, failure("lifecycle.state", "the adapter job ownership could not be verified", "")
		}
		r.invocation.jobs[job] = jobOwnership{lockIdentity, holderIdentity}
	}
	return job, lock, holder, nil
}
