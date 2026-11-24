//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"sync"
	"syscall"

	"github.com/crmarques/bootwright/internal/workspace/contextfs/kernel"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const trustSubtree = "trust"

// hostKeysFile holds the SSH host keys one context trusts. It carries public
// material only, so it lives in the context's own state rather than in secret
// custody, and a session reads it without opening the keyring.
const hostKeysFile = "hosts.json"

type trustStore struct{ store *Store }

func (s *Store) ReadHostKeys(ctx context.Context, name string) ([]byte, error) {
	return trustStore{s}.readHostKeys(ctx, name)
}

func (s *Store) ReplaceHostKeys(ctx context.Context, name string, data, expected []byte) error {
	return trustStore{s}.replaceHostKeys(ctx, name, data, expected)
}

// readHostKeys returns the context's SSH trust records under the shared root
// lock. It creates, repairs and publishes nothing; a context that has trusted
// nothing yet reads as absent rather than as a failure.
func (t trustStore) readHostKeys(ctx context.Context, name string) ([]byte, error) {
	var data []byte
	err := t.withTrustArea(ctx, name, false, nil, func(area *operationArea) error {
		content, found, err := area.Read(ctx, hostKeysFile, maxOperationRecord)
		if err != nil || !found {
			return err
		}
		data = content
		return nil
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// replaceHostKeys publishes the records atomically against the exact content
// the caller read, so two operators recording a first-use key at once cannot
// lose one another's decision. A nil expectation requires the file to be absent.
func (t trustStore) replaceHostKeys(ctx context.Context, name string, data, expected []byte) error {
	if len(data) == 0 {
		return state("SSH trust publication requires the records to publish")
	}
	return t.withTrustArea(ctx, name, true, nil, func(area *operationArea) error {
		if err := area.EnsureDirectory(ctx, ""); err != nil {
			return err
		}
		return area.Replace(ctx, hostKeysFile, data, expected)
	})
}

// withTrustArea opens one context's trust subtree under the lock its access
// requires. A mutation takes the exclusive root lock for the same reason a
// lifecycle publication does: the records it replaces must not move underneath
// the comparison that authorizes the replacement.
func (t trustStore) withTrustArea(ctx context.Context, name string, mutation bool, expected *contexts.Record, callback func(*operationArea) error) error {
	if !contextName(name) {
		return state("SSH trust access requires an explicit context name")
	}
	how := syscall.LOCK_SH
	if mutation {
		how = syscall.LOCK_EX
	}
	return t.store.withRead(ctx, how, readScope{absent: contexts.AbsentContext(name), registered: true}, func(root *directory, registry contexts.Registry) error {
		record, err := lifecycleRecord(registry, name)
		if err != nil {
			return err
		}
		if expected != nil && record != *expected {
			return state("the context changed during the policy command; repeat it")
		}
		container, dir, err := openContext(root, record)
		if err != nil {
			return safeError(err)
		}
		defer container.File.Close()
		defer dir.File.Close()
		active := true
		defer func() { active = false }()
		area := &operationArea{
			store: t.store, subtree: trustSubtree, context: dir, name: record.Name,
			active: func() bool { return active }, readOnly: !mutation, cached: mutation,
		}
		return safeError(callback(area))
	})
}

const sessionPolicyFile = "session-policy.yaml"

func (s *Store) ReadSessionPolicy(ctx context.Context, name string) ([]byte, error) {
	var data []byte
	err := (trustStore{s}).withTrustArea(ctx, name, false, nil, func(area *operationArea) error {
		var err error
		data, _, err = area.Read(ctx, sessionPolicyFile, maxOperationRecord)
		return err
	})
	return data, err
}

func (s *Store) ReplaceSessionPolicy(ctx context.Context, expected contexts.Record, data []byte) error {
	return (trustStore{s}).withTrustArea(ctx, expected.Name, true, &expected, func(area *operationArea) error {
		if err := area.EnsureDirectory(ctx, ""); err != nil {
			return err
		}
		expected, _, err := area.Read(ctx, sessionPolicyFile, maxOperationRecord)
		if err != nil {
			return err
		}
		return area.Replace(ctx, sessionPolicyFile, data, expected)
	})
}

func (s *Store) RemoveSessionPolicy(ctx context.Context, expected contexts.Record, confirmed []byte) (bool, error) {
	removed := false
	err := (trustStore{s}).withTrustArea(ctx, expected.Name, true, &expected, func(area *operationArea) error {
		current, found, err := area.Read(ctx, sessionPolicyFile, maxOperationRecord)
		if err != nil || !found {
			return err
		}
		if !bytes.Equal(current, confirmed) {
			return state("the session access policy changed after it was confirmed; repeat policy delete")
		}
		if err := area.RemoveRecord(ctx, sessionPolicyFile, current); err != nil {
			return state("the session access policy could not be removed; repeat policy delete")
		}
		removed = true
		return nil
	})
	return removed, err
}

// HoldSessionAdmission pins the context mapping until SSH material is ready.
func (s *Store) HoldSessionAdmission(ctx context.Context) (func(), error) {
	root, err := s.openRoot(ctx, false, nil)
	if err != nil {
		return nil, safeError(err)
	}
	if err := awaitRoot(ctx, s, root, syscall.LOCK_SH); err != nil {
		root.File.Close()
		return nil, safeError(err)
	}
	return sync.OnceFunc(func() { kernel.UnlockAndClose(root.File) }), nil
}
