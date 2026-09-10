//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/secrets/storage"
)

func (a *secretArea) observe(path string, parent, identity syscall.Stat_t) {
	if a.observed == nil {
		a.observed = make(map[string]secretExpectation)
	}
	if _, exists := a.observed[path]; !exists {
		a.observeReplacement(path, parent, identity)
	}
}

func (a *secretArea) observeReplacement(path string, parent, identity syscall.Stat_t) {
	if a.observed == nil {
		a.observed = make(map[string]secretExpectation)
	}
	a.observed[path] = secretExpectation{exists: true, identity: identity, parentIdentity: parent, parentKnown: true}
}

type secretCleanupTarget struct {
	path        string
	parts       []string
	expectation secretExpectation
}

func (a *secretArea) SyncFile(ctx context.Context, path string) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	parts, err := secretPath(path, 1, 2)
	if err != nil {
		return err
	}
	expected, exists := a.observed[path]
	if !exists || !expected.exists || !expected.parentKnown || expected.identity.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return secretConflict(ctx, "secret file synchronization lacks an exact prior observation", nil)
	}
	if err := a.store.checkpoint(ctx, "before-secret-file-sync"); err != nil {
		return secretEffectFailure(ctx, "secret file durability could not be established", err)
	}
	if err := a.verifyExpectedContext(ctx); err != nil {
		return secretConflict(ctx, "context changed before secret file synchronization", err)
	}
	parent, name, close, err := a.parent(ctx, parts)
	if err != nil {
		return secretConflict(ctx, "secret synchronization parent cannot be verified", err)
	}
	defer close()
	if !sameIdentity(parent.identity, expected.parentIdentity) {
		return secretConflict(ctx, "secret synchronization parent was replaced", nil)
	}
	if err := a.store.syncVerifiedFile(ctx, parent, name, expected.identity); err != nil {
		return secretEffectFailure(ctx, "secret file durability could not be established", err)
	}
	if err := a.store.syncDirectory(ctx, parent); err != nil {
		return secretEffectFailure(ctx, "secret file parent durability could not be established", err)
	}
	return nil
}

func (a *secretArea) Prune(ctx context.Context, expectedStore []byte, paths []string) error {
	if err := a.available(ctx, false); err != nil {
		return err
	}
	if a.readOnly || a.phase != secretBeforePublication && a.phase != secretCommitted {
		return secretConflict(ctx, "secret cleanup is unavailable in the current publication phase", nil)
	}
	if len(paths) > maxSecretEntries {
		return secretLimit("secret cleanup exceeds its entry bound")
	}
	if len(paths) == 0 {
		return nil
	}
	expected, exists := a.mutable["store.json"]
	if !exists || !expected.exists || !bytes.Equal(expected.data, expectedStore) || a.secrets == nil {
		return secretConflict(ctx, "secret cleanup lacks the exact published store expectation", nil)
	}
	targets, err := a.cleanupTargets(paths)
	if err != nil {
		return err
	}
	if err := a.verifyExpectedContext(ctx); err != nil {
		return secretConflict(ctx, "context changed before secret cleanup", err)
	}
	if err := verifySecretExpectation(ctx, a.secrets, "store.json", expected); err != nil {
		return secretConflict(ctx, "secret store changed before cleanup", err)
	}
	if err := a.store.checkpoint(ctx, "before-secret-prune"); err != nil {
		return secretEffectFailure(ctx, "secret cleanup durability could not be established", err)
	}
	if err := a.store.syncVerifiedFile(ctx, a.secrets, "store.json", expected.identity); err != nil {
		return secretEffectFailure(ctx, "secret cleanup store durability could not be established", err)
	}
	if err := a.store.syncDirectory(ctx, a.secrets); err != nil {
		return secretEffectFailure(ctx, "secret cleanup publication durability could not be established", err)
	}
	for _, target := range targets {
		if err := a.removeObserved(ctx, target, func() error {
			return verifySecretObserved(a.secrets, "store.json", expected.identity)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (a *secretArea) PruneUnpublished(ctx context.Context, guards []storage.RecordExpectation, paths []string) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	if a.phase != secretBeforePublication || a.secrets == nil {
		return secretConflict(ctx, "unpublished secret cleanup requires an untouched publication session", nil)
	}
	absent, exists := a.mutable["store.json"]
	if !exists || absent.exists || len(guards) == 0 || len(guards) > 8 || len(paths) > maxSecretEntries {
		return secretConflict(ctx, "unpublished secret cleanup lacks bounded publication guards", nil)
	}
	guarded := make(map[string]secretExpectation, len(guards))
	guardPaths := make([]string, 0, len(guards))
	total := 0
	for _, guard := range guards {
		if _, err := secretPath(guard.Path, 1, 1); err != nil {
			return err
		}
		expected, exists := a.mutable[guard.Path]
		_, duplicate := guarded[guard.Path]
		if guard.Path == "store.json" || duplicate || !exists || !expected.exists || len(guard.Data) == 0 || len(guard.Data) > (8<<20)-total || !bytes.Equal(expected.data, guard.Data) {
			return secretConflict(ctx, "unpublished secret cleanup guard lacks its exact bounded observation", nil)
		}
		total += len(guard.Data)
		guarded[guard.Path] = expected
		guardPaths = append(guardPaths, guard.Path)
	}
	for _, path := range paths {
		if _, exists := guarded[path]; exists {
			return secretConflict(ctx, "unpublished secret cleanup cannot remove its publication guards", nil)
		}
	}
	targets, err := a.cleanupTargets(paths)
	if err != nil {
		return err
	}
	verifyGuards := func() error {
		if err := verifySecretExpectation(ctx, a.secrets, "store.json", absent); err != nil {
			return err
		}
		for _, path := range guardPaths {
			if err := verifySecretObserved(a.secrets, path, guarded[path].identity); err != nil {
				return err
			}
		}
		return nil
	}
	if err := a.verifyExpectedContext(ctx); err != nil {
		return secretConflict(ctx, "context changed before unpublished secret cleanup", err)
	}
	if err := verifyGuards(); err != nil {
		return secretConflict(ctx, "unpublished secret cleanup guards changed", err)
	}
	for _, path := range guardPaths {
		if err := a.store.syncVerifiedFile(ctx, a.secrets, path, guarded[path].identity); err != nil {
			return secretEffectFailure(ctx, "unpublished secret cleanup guard durability could not be established", err)
		}
	}
	if err := a.store.syncDirectory(ctx, a.secrets); err != nil {
		return secretEffectFailure(ctx, "unpublished secret cleanup publication durability could not be established", err)
	}
	for _, target := range targets {
		if err := a.removeObserved(ctx, target, verifyGuards); err != nil {
			return err
		}
	}
	return nil
}

func (a *secretArea) cleanupTargets(paths []string) ([]secretCleanupTarget, error) {
	targets := make([]secretCleanupTarget, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		parts, err := secretPath(path, 1, 2)
		if err != nil {
			return nil, err
		}
		expected, exists := a.observed[path]
		if path == "store.json" || seen[path] || !exists || !expected.exists || !expected.parentKnown {
			return nil, secretCorrupt("secret cleanup target lacks an exact prior observation")
		}
		seen[path] = true
		targets = append(targets, secretCleanupTarget{path: path, parts: parts, expectation: expected})
	}
	slices.SortFunc(targets, func(left, right secretCleanupTarget) int {
		if len(left.parts) != len(right.parts) {
			return len(right.parts) - len(left.parts)
		}
		return strings.Compare(left.path, right.path)
	})
	return targets, nil
}

func (a *secretArea) removeObserved(ctx context.Context, target secretCleanupTarget, verifyPublication func() error) error {
	if err := a.store.checkpoint(ctx, "before-secret-unlink"); err != nil {
		return secretEffectFailure(ctx, "secret cleanup stopped before removal", err)
	}
	if err := a.available(ctx, false); err != nil {
		return err
	}
	if err := a.verifyExpectedContext(ctx); err != nil {
		return secretConflict(ctx, "context changed during secret cleanup", err)
	}
	if err := verifyPublication(); err != nil {
		return secretConflict(ctx, "secret store changed during cleanup", err)
	}
	parent, name, close, err := a.parent(ctx, target.parts)
	if err != nil {
		return secretConflict(ctx, "secret cleanup parent cannot be verified", err)
	}
	defer close()
	if !sameIdentity(parent.identity, target.expectation.parentIdentity) {
		return secretConflict(ctx, "secret cleanup parent was replaced", nil)
	}
	directory := target.expectation.identity.Mode&syscall.S_IFMT == syscall.S_IFDIR
	if err := unlinkVerified(parent, name, target.expectation.identity, directory); err != nil {
		return secretConflict(ctx, "secret cleanup target changed or could not be removed", err)
	}
	delete(a.observed, target.path)
	a.forgetExpectation(target.path)
	if err := a.store.checkpoint(ctx, "after-secret-unlink"); err != nil {
		return secretEffectFailure(ctx, "secret cleanup removal durability is uncertain; inspect before retrying", err)
	}
	if err := a.store.syncDirectory(ctx, parent); err != nil {
		return secretEffectFailure(ctx, "secret cleanup removal durability is uncertain; inspect before retrying", err)
	}
	return nil
}

func verifySecretObserved(parent *directory, name string, expected syscall.Stat_t) error {
	if err := parent.verify(); err != nil {
		return err
	}
	file, err := openRelative(parent, name, pathHandle, 0)
	if err != nil {
		return state("secret cleanup store cannot be verified")
	}
	defer file.Close()
	actual, err := statHandle(file)
	if err != nil || !sameFile(expected, actual) {
		return state("secret cleanup store was replaced or modified")
	}
	return nil
}
