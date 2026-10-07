//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
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
	root, err := s.openRoot(ctx, false, nil)
	if errors.Is(err, syscall.ENOENT) {
		return contexts.AbsentContext(name)
	}
	if err != nil {
		return safeError(err)
	}
	defer root.file.Close()
	if mutation {
		if err := lock(root); err != nil {
			return err
		}
	} else if err := lockShared(root); err != nil {
		return err
	}
	defer syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN)
	registry, exists, err := readRegistry(ctx, root)
	if err != nil {
		return safeError(err)
	}
	if !exists {
		return contexts.AbsentContext(name)
	}
	if err := verifyMappings(ctx, root, registry); err != nil {
		return safeError(err)
	}
	record, err := lifecycleRecord(registry, name)
	if err != nil {
		return err
	}
	container, err := openDirectory(root, "contexts")
	if err != nil {
		return safeError(err)
	}
	defer container.file.Close()
	dir, err := openDirectory(container, record.Name)
	if err != nil {
		return safeError(err)
	}
	defer dir.file.Close()
	if record.DirectoryInode != 0 && (dir.identity.Ino != record.DirectoryInode || uint64(dir.identity.Dev) != record.DirectoryDevice) {
		return state("context directory was replaced")
	}
	active := true
	defer func() { active = false }()
	area := &operationArea{
		store: s, subtree: trustSubtree, context: dir, name: record.Name,
		active: func() bool { return active }, readOnly: !mutation, cached: mutation,
	}
	return safeError(callback(area))
}
