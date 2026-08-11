package workflow

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type CommandRunLease struct {
	RunID     string
	Command   string
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

	hostMutationNonce [32]byte
}

type HostMutationLeaseIdentity struct {
	APIVersion   string `json:"apiVersion"`
	Kind         string `json:"kind"`
	Token        string `json:"token"`
	RunID        string `json:"runId"`
	Command      string `json:"command"`
	Controller   string `json:"controller"`
	PID          int    `json:"pid"`
	ProcessStart string `json:"processStart"`
	StartedAt    string `json:"startedAt"`
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
	return acquireCommandRunLease(parent, runsDir, runID, command, now, true)
}

func AcquireSharedServiceMutationLease(parent context.Context, runsDir, contextName, command string) (*CommandRunLease, error) {
	contextName = strings.TrimSpace(contextName)
	if contextName == "" {
		return nil, errors.New("shared-service mutation lease requires a context name")
	}
	switch command {
	case "apply", "destroy":
	default:
		return nil, fmt.Errorf("unsupported shared-service mutating command %q", command)
	}
	now := time.Now().UTC()
	runID := "shared-services-" + contextName + "-" + command + "-" + now.Format("20060102T150405.000000000Z")
	return acquireCommandRunLease(parent, runsDir, runID, command, now, false)
}

func acquireCommandRunLease(parent context.Context, runsDir, runID, command string, now time.Time, sweepGitCredentials bool) (*CommandRunLease, error) {
	var hostMutationNonce [32]byte
	if _, err := rand.Read(hostMutationNonce[:]); err != nil {
		return nil, fmt.Errorf("generate host mutation lease identity: %w", err)
	}
	lease := NewRunLease(runID, now)
	if err := AcquireRunLease(runsDir, lease, now); err != nil {
		return nil, err
	}
	if sweepGitCredentials {
		if err := SweepGitCredentialResidue(runsDir); err != nil {
			releaseErr := RemoveRunLeaseIfOwner(runsDir, runID)
			if releaseErr != nil {
				return nil, fmt.Errorf("%w; additionally failed to release the mutating run lease: %v", err, releaseErr)
			}
			return nil, err
		}
	}
	ctx, cancel := context.WithCancel(parent)
	stop, heartbeatErrors := startRunLeaseHeartbeat(ctx, runsDir, lease)
	guard := &CommandRunLease{
		RunID:             runID,
		Command:           command,
		StartedAt:         now,
		runsDir:           runsDir,
		lease:             lease,
		ctx:               ctx,
		cancel:            cancel,
		stop:              stop,
		errors:            make(chan error, 1),
		done:              make(chan struct{}),
		hostMutationNonce: hostMutationNonce,
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

func (g *CommandRunLease) HostMutationLease() (HostMutationLeaseIdentity, error) {
	if g == nil {
		return HostMutationLeaseIdentity{}, errors.New("host mutation lease requires a held command lease")
	}
	if err := g.RequireOwned(); err != nil {
		return HostMutationLeaseIdentity{}, err
	}
	lease := g.lease
	if strings.TrimSpace(lease.RunID) == "" || strings.TrimSpace(g.Command) == "" || strings.TrimSpace(lease.Hostname) == "" || lease.PID <= 0 || lease.StartedAt.IsZero() {
		return HostMutationLeaseIdentity{}, errors.New("held command lease has incomplete host mutation identity")
	}
	startedAt := lease.StartedAt.UTC().Format(time.RFC3339Nano)
	payload := []byte(strings.Join([]string{
		lease.RunID,
		g.Command,
		lease.Hostname,
		fmt.Sprintf("%d", lease.PID),
		lease.ProcessStart,
		startedAt,
	}, "\x00"))
	payload = append(g.hostMutationNonce[:], payload...)
	token := sha256.Sum256(payload)
	return HostMutationLeaseIdentity{
		APIVersion:   "bootwright.io/host-mutation-lease/v1alpha1",
		Kind:         "host-mutation-lease",
		Token:        fmt.Sprintf("sha256:%x", token),
		RunID:        lease.RunID,
		Command:      g.Command,
		Controller:   lease.Hostname,
		PID:          lease.PID,
		ProcessStart: lease.ProcessStart,
		StartedAt:    startedAt,
	}, nil
}

func (g *CommandRunLease) Context() context.Context {
	if g == nil {
		return context.Background()
	}
	return g.ctx
}

func (g *CommandRunLease) BindContext(parent context.Context) (context.Context, func(), error) {
	if err := g.RequireOwned(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(g.Context(), cancel)
	return ctx, func() {
		stop()
		cancel()
	}, nil
}

func (g *CommandRunLease) BindCleanupContext(parent context.Context) (context.Context, func(), error) {
	if err := g.RequireOwned(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, nil
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
