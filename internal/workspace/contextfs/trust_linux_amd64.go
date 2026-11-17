//go:build linux && amd64

package contextfs

import (
	"context"
	"syscall"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const trustSubtree = "trust"

// hostKeysFile holds the SSH host keys one context trusts. It carries public
// material only, so it lives in the context's own state rather than in secret
// custody, and a session reads it without opening the keyring.
const hostKeysFile = "hosts.json"

// ReadHostKeys returns the context's SSH trust records under the shared root
// lock. It creates, repairs and publishes nothing; a context that has trusted
// nothing yet reads as absent rather than as a failure.
func (s *Store) ReadHostKeys(ctx context.Context, name string) ([]byte, error) {
	var data []byte
	err := s.withTrustArea(ctx, name, false, func(area *operationArea) error {
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

// ReplaceHostKeys publishes the records atomically against the exact content
// the caller read, so two operators recording a first-use key at once cannot
// lose one another's decision. A nil expectation requires the file to be absent.
func (s *Store) ReplaceHostKeys(ctx context.Context, name string, data, expected []byte) error {
	if len(data) == 0 {
		return state("SSH trust publication requires the records to publish")
	}
	return s.withTrustArea(ctx, name, true, func(area *operationArea) error {
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
func (s *Store) withTrustArea(ctx context.Context, name string, mutation bool, callback func(*operationArea) error) error {
	if !contextName(name) {
		return state("SSH trust access requires an explicit context name")
	}
	how := syscall.LOCK_SH
	if mutation {
		how = syscall.LOCK_EX
	}
	return s.withRead(ctx, how, readScope{absent: contexts.AbsentContext(name), registered: true}, func(root *directory, registry contexts.Registry) error {
		record, err := lifecycleRecord(registry, name)
		if err != nil {
			return err
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
			store: s, subtree: trustSubtree, context: dir, name: record.Name,
			active: func() bool { return active }, readOnly: !mutation, cached: mutation,
		}
		return safeError(callback(area))
	})
}
