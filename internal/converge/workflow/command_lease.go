package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type CommandRunLease struct {
	RunID     string
	StartedAt time.Time

	runsDir string
	lease   RunLease
	ctx     context.Context
	cancel  context.CancelFunc
	stop    func()
	errors  chan error
	done    chan struct{}

	mu       sync.Mutex
	runErr   error
	closeErr error
	closed   bool
}

func AcquireCommandRunLease(parent context.Context, runsDir, command string) (*CommandRunLease, error) {
	now := time.Now().UTC()
	var runID string
	switch command {
	case "apply":
		runID = applyRunID(now)
	case "destroy":
		runID = destroyRunID(now)
	case "context-update", "diff-adopt", "replace-arbiter":
		runID = command + "-" + now.Format("20060102T150405.000000000Z")
	default:
		return nil, fmt.Errorf("unsupported mutating command %q", command)
	}
	lease := NewRunLease(runID, now)
	if err := AcquireRunLease(runsDir, lease, now); err != nil {
		return nil, err
	}
	if err := SweepGitCredentialResidue(runsDir); err != nil {
		releaseErr := RemoveRunLeaseIfOwner(runsDir, runID)
		if releaseErr != nil {
			return nil, fmt.Errorf("%w; additionally failed to release the mutating run lease: %v", err, releaseErr)
		}
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	stop, heartbeatErrors := startRunLeaseHeartbeat(ctx, runsDir, lease)
	guard := &CommandRunLease{
		RunID:     runID,
		StartedAt: now,
		runsDir:   runsDir,
		lease:     lease,
		ctx:       ctx,
		cancel:    cancel,
		stop:      stop,
		errors:    make(chan error, 1),
		done:      make(chan struct{}),
	}
	go func() {
		defer close(guard.done)
		select {
		case err := <-heartbeatErrors:
			if err != nil {
				guard.mu.Lock()
				guard.runErr = err
				guard.mu.Unlock()
				guard.errors <- err
				guard.cancel()
			}
		case <-ctx.Done():
		}
	}()
	return guard, nil
}

func (g *CommandRunLease) Context() context.Context {
	if g == nil {
		return context.Background()
	}
	return g.ctx
}

func (g *CommandRunLease) Errors() <-chan error {
	if g == nil {
		return nil
	}
	return g.errors
}

func (g *CommandRunLease) RequireOwned() error {
	if g == nil {
		return errors.New("mutating run lease is required")
	}
	g.mu.Lock()
	runErr := g.runErr
	closed := g.closed
	g.mu.Unlock()
	if runErr != nil {
		return runErr
	}
	if closed {
		return errors.New("mutating run lease is already closed")
	}
	existing, found, err := LoadRunLease(g.runsDir)
	if err != nil {
		return err
	}
	if !found || existing.RunID != g.RunID {
		return fmt.Errorf("mutating run lease taken over by another run: %w", ErrLeaseNotOwned)
	}
	return nil
}

func (g *CommandRunLease) Close() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	if g.closed {
		err := g.closeErr
		g.mu.Unlock()
		return err
	}
	g.closed = true
	g.mu.Unlock()
	g.cancel()
	g.stop()
	<-g.done
	removeErr := RemoveRunLeaseIfOwner(g.runsDir, g.RunID)
	g.mu.Lock()
	switch {
	case g.runErr != nil && removeErr != nil:
		g.closeErr = fmt.Errorf("%w; additionally failed to release the mutating run lease: %v", g.runErr, removeErr)
	case g.runErr != nil:
		g.closeErr = g.runErr
	default:
		g.closeErr = removeErr
	}
	err := g.closeErr
	g.mu.Unlock()
	return err
}
