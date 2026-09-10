//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

var _ contexts.Repository = (*Store)(nil)

const (
	missingRegistryMessage     = "context store is missing registry.json"
	storeRecoveryRemediation   = "restore the whole store from a matching backup or move it aside if disposable, then retry"
	pendingRegistryMessage     = "context initialization is incomplete"
	pendingRegistryRemediation = "retry context init with the original options"
)

type missingRegistryError struct {
	failure     error
	recoverable bool
}

func (e *missingRegistryError) Error() string { return e.failure.Error() }

func (e *missingRegistryError) Unwrap() error { return e.failure }

func (s *Store) CheckInputDirectory(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := s.rootPath()
	if err != nil {
		return err
	}
	input, err := filepath.Abs(path)
	if err != nil || path == "" || !canonicalPath(input) || beneath(input, root) {
		return state("state root overlaps admitted input or input path is invalid")
	}
	return nil
}

func safeError(err error) error {
	if err == nil {
		return nil
	}
	var failure *desiredstate.Failure
	if errors.As(err, &failure) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return state("context storage could not be safely accessed")
}

func (s *Store) View(ctx context.Context) (contexts.Registry, error) {
	root, err := s.openRoot(ctx, false, nil)
	if errors.Is(err, syscall.ENOENT) {
		return emptyRegistry(), nil
	}
	if err != nil {
		return contexts.Registry{}, safeError(err)
	}
	defer root.file.Close()
	if err := lockShared(root); err != nil {
		return contexts.Registry{}, err
	}
	defer syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN)
	registry, _, err := readRegistry(ctx, root)
	if err == nil {
		err = verifyMappings(ctx, root, registry)
	}
	return registry, safeError(err)
}

func readRegistry(ctx context.Context, root *directory) (contexts.Registry, bool, error) {
	data, err := readBounded(ctx, root, "registry.json", maxRegistry, false)
	if errors.Is(err, syscall.ENOENT) {
		names, readErr := root.file.Readdirnames(1)
		if len(names) == 0 && errors.Is(readErr, io.EOF) {
			return emptyRegistry(), false, nil
		}
		_, _, recoverable, inspectErr := inspectInitialRegistry(ctx, root)
		if canceled := ctx.Err(); canceled != nil {
			return contexts.Registry{}, false, canceled
		}
		if inspectErr == nil && recoverable {
			return contexts.Registry{}, false, pendingInitialRegistry()
		}
		return contexts.Registry{}, false, missingRegistry()
	}
	if err != nil {
		return contexts.Registry{}, false, err
	}
	var registry contexts.Registry
	if err := decodeRecord(data, maxRegistry, &registry); err != nil {
		return contexts.Registry{}, false, err
	}
	if err := validateRegistry(registry); err != nil {
		return contexts.Registry{}, false, err
	}
	if err := verifyEmptyRegistryRoot(ctx, root, registry); err != nil {
		return contexts.Registry{}, false, err
	}
	return registry, true, nil
}

func missingRegistry() error {
	return &missingRegistryError{failure: contexts.StateErrorWithRemediation(
		missingRegistryMessage,
		storeRecoveryRemediation,
	)}
}

func inconsistentEmptyRegistry() error {
	return contexts.StateErrorWithRemediation(
		"registry.json is empty but the context store is not",
		storeRecoveryRemediation,
	)
}

func pendingInitialRegistry() error {
	return &missingRegistryError{
		failure:     contexts.StateErrorWithRemediation(pendingRegistryMessage, pendingRegistryRemediation),
		recoverable: true,
	}
}

func initialRegistryRecoveryError(message string) error {
	return contexts.StateErrorWithRemediation(message, pendingRegistryRemediation)
}

func uncertainInitialRegistryRecovery() error {
	return contexts.StateErrorWithRemediation(
		"context initialization may have completed",
		pendingRegistryRemediation,
	)
}

func uncertainRegistryPublication(initial bool) error {
	if initial {
		return uncertainInitialRegistryRecovery()
	}
	return contexts.StateErrorWithRemediation(
		"registry publication may have completed, but its disk state is unconfirmed",
		"inspect the target context, then retry the same command",
	)
}

func pendingInitialRegistryName(name string) bool {
	const suffix = ".json"
	return strings.HasSuffix(name, suffix) && identifier(strings.TrimSuffix(name, suffix), "pending-")
}

func rootEntryNames(root *directory, maximum int) ([]string, error) {
	if err := root.verify(); err != nil {
		return nil, err
	}
	file, err := openWithin(root, ".", syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	identity, err := statHandle(file)
	if err != nil || !sameIdentity(root.identity, identity) {
		return nil, state("state root changed during enumeration")
	}
	names, readErr := file.Readdirnames(maximum + 1)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, state("state root cannot be enumerated safely")
	}
	if len(names) > maximum {
		return nil, state("state root entry count exceeds its limit")
	}
	if err := root.verify(); err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil

}

func soleRootEntry(root *directory) (string, bool, error) {
	names, err := rootEntryNames(root, 2)
	if err != nil || len(names) != 1 {
		return "", false, err
	}
	return names[0], true, nil
}

func inspectInitialRegistry(ctx context.Context, root *directory) (string, syscall.Stat_t, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", syscall.Stat_t{}, false, err
	}
	name, sole, err := soleRootEntry(root)
	if err != nil || !sole || !pendingInitialRegistryName(name) {
		return "", syscall.Stat_t{}, false, err
	}
	want, err := encodeRecord(emptyRegistry(), maxRegistry)
	if err != nil {
		return "", syscall.Stat_t{}, false, err
	}
	data, identity, err := readBoundedIdentity(ctx, root, name, maxRegistry, true)
	if err != nil || !bytes.Equal(data, want) {
		return "", syscall.Stat_t{}, false, err
	}
	return name, identity, true, nil
}

func verifyEmptyRegistryRoot(ctx context.Context, root *directory, registry contexts.Registry) error {
	if registry.Version == 3 || len(registry.Identities) != 0 || len(registry.Contexts) != 0 {
		return nil
	}
	names, err := rootEntryNames(root, maxIdentities+1)
	if err != nil {
		return inconsistentEmptyRegistry()
	}
	registryFound := false
	for _, name := range names {
		if name == "registry.json" {
			if registryFound {
				return inconsistentEmptyRegistry()
			}
			registryFound = true
			continue
		}
		if !pendingInitialRegistryName(name) {
			return inconsistentEmptyRegistry()
		}
		if err := verifyIgnoredRegistryStage(ctx, root, name); err != nil {
			if canceled := ctx.Err(); canceled != nil {
				return canceled
			}
			return inconsistentEmptyRegistry()
		}
	}
	current, err := rootEntryNames(root, maxIdentities+1)
	if !registryFound || err != nil || !slices.Equal(names, current) {
		return inconsistentEmptyRegistry()
	}
	return nil
}

func verifyIgnoredRegistryStage(ctx context.Context, root *directory, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := openRelative(root, name, pathHandle, 0)
	if err != nil {
		return err
	}
	before, err := statHandle(file)
	file.Close()
	if err != nil || before.Dev != root.identity.Dev || !private(before, syscall.S_IFREG, root.identity.Uid, root.identity.Gid) || before.Size < 0 || before.Size > maxRegistry {
		return state("unpublished registry stage is unsafe")
	}
	file, err = openRelative(root, name, pathHandle, 0)
	if err != nil {
		return err
	}
	after, err := statHandle(file)
	file.Close()
	if err != nil || !sameFile(before, after) {
		return state("unpublished registry stage changed during verification")
	}
	return root.verify()
}

func (s *Store) syncPendingInitialRegistry(ctx context.Context, root *directory, name string, expected syscall.Stat_t) (syscall.Stat_t, error) {
	file, err := openRelative(root, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return syscall.Stat_t{}, initialRegistryRecoveryError("pending initial registry cannot be verified")
	}
	defer file.Close()
	before, err := statHandle(file)
	if err != nil || !sameFile(expected, before) {
		return syscall.Stat_t{}, initialRegistryRecoveryError("pending initial registry changed during recovery")
	}
	if err := s.checkpoint(ctx, "sync-initial-registry-file"); err != nil {
		return syscall.Stat_t{}, err
	}
	if err := file.Sync(); err != nil {
		return syscall.Stat_t{}, initialRegistryRecoveryError("pending initial registry durability could not be established")
	}
	after, err := statHandle(file)
	if err != nil || !sameFile(before, after) {
		return syscall.Stat_t{}, initialRegistryRecoveryError("pending initial registry changed during recovery")
	}
	if err := root.verify(); err != nil {
		return syscall.Stat_t{}, initialRegistryRecoveryError("state root changed during initial registry recovery")
	}
	return after, nil
}

func (s *Store) recoverInitialRegistry(ctx context.Context, root *directory) (bool, error) {
	name, identity, recoverable, err := inspectInitialRegistry(ctx, root)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return false, canceled
		}
		return false, initialRegistryRecoveryError("pending initial registry cannot be inspected safely")
	}
	if !recoverable {
		return false, initialRegistryRecoveryError("pending initial registry changed before recovery")
	}
	identity, err = s.syncPendingInitialRegistry(ctx, root, name, identity)
	if err != nil {
		return false, err
	}
	if err := s.checkpoint(ctx, "before-initial-registry-recovery"); err != nil {
		return false, err
	}
	currentName, currentIdentity, recoverable, err := inspectInitialRegistry(ctx, root)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return false, canceled
		}
		return false, initialRegistryRecoveryError("pending initial registry cannot be reverified safely")
	}
	if !recoverable || currentName != name || !sameFile(identity, currentIdentity) {
		return false, initialRegistryRecoveryError("pending initial registry changed during recovery")
	}
	if err := renameNoReplaceAt(root, name, "registry.json"); err != nil {
		return false, initialRegistryRecoveryError("initial registry could not be atomically recovered")
	}
	want, err := encodeRecord(emptyRegistry(), maxRegistry)
	if err != nil {
		return false, uncertainInitialRegistryRecovery()
	}
	published, publishedIdentity, err := readBoundedIdentity(ctx, root, "registry.json", maxRegistry, true)
	if err != nil || !bytes.Equal(published, want) || !sameIdentity(currentIdentity, publishedIdentity) {
		return false, uncertainInitialRegistryRecovery()
	}
	if err := s.checkpoint(ctx, "after-initial-registry-recovery"); err != nil {
		return false, uncertainInitialRegistryRecovery()
	}
	entry, sole, err := soleRootEntry(root)
	if err != nil || !sole || entry != "registry.json" {
		return false, uncertainInitialRegistryRecovery()
	}
	if err := s.syncDirectory(ctx, root); err != nil {
		return false, uncertainInitialRegistryRecovery()
	}
	entry, sole, err = soleRootEntry(root)
	if err != nil || !sole || entry != "registry.json" {
		return false, uncertainInitialRegistryRecovery()
	}
	final, finalIdentity, err := readBoundedIdentity(ctx, root, "registry.json", maxRegistry, true)
	if err != nil || !bytes.Equal(final, want) || !sameFile(publishedIdentity, finalIdentity) {
		return false, uncertainInitialRegistryRecovery()
	}
	return true, nil
}

func verifyMappings(ctx context.Context, root *directory, registry contexts.Registry) error {
	sizes := make([]int, len(registry.Contexts))
	total := int64(0)
	for index, record := range registry.Contexts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if record.Mode != contexts.Ready {
			continue
		}
		container, dir, err := openContext(root, record)
		if err != nil {
			return err
		}
		err = verifyReservation(ctx, dir, record.ID, record.Name)
		var config []byte
		if err == nil {
			config, err = readBounded(ctx, dir, "context.yaml", maxRecord, true)
		}
		if err == nil {
			parsed, parseErr := contexts.ParseConfiguration(record.Name, config)
			if parseErr != nil || !bytes.Equal(config, parsed.Canonical()) || parsed.SecretStore.Type != record.SecretStoreType {
				err = state("persisted context configuration is inconsistent")
			}
		}
		if err == nil && record.Revision != "" {
			owned := []*directory{}
			parent := dir
			for _, name := range []string{"desired-state", "revisions", record.Revision} {
				child, openErr := openDirectory(parent, name)
				if openErr != nil {
					err = openErr
					break
				}
				owned = append(owned, child)
				parent = child
			}
			if err == nil {
				file, openErr := openRelative(parent, "manifest.json", pathHandle, 0)
				if openErr != nil {
					err = openErr
				} else {
					stat, statErr := statHandle(file)
					file.Close()
					if statErr != nil || !private(stat, syscall.S_IFREG, parent.identity.Uid, parent.identity.Gid) || stat.Size < 0 || stat.Size > maxManifest {
						err = state("manifest handle is unsafe")
					} else {
						sizes[index] = int(stat.Size)
						total += stat.Size
						if total > maxAllManifests {
							err = state("aggregate referenced manifest bytes exceed their limit")
						}
					}
				}
			}
			for i := len(owned) - 1; i >= 0; i-- {
				owned[i].file.Close()
			}
		}
		dir.file.Close()
		container.file.Close()
		if err != nil {
			return err
		}
	}
	for index, record := range registry.Contexts {
		if record.Mode != contexts.Ready || record.Revision == "" {
			continue
		}
		_, _, close, err := openManifestBounded(ctx, root, record, sizes[index])
		if err != nil {
			return err
		}
		close()
	}
	return root.verify()
}

func openContext(root *directory, record contexts.Record) (*directory, *directory, error) {
	container, err := openDirectory(root, "contexts")
	if err != nil {
		return nil, nil, err
	}
	dir, err := openDirectory(container, record.Name)
	if err != nil {
		container.file.Close()
		return nil, nil, err
	}
	if record.DirectoryInode != 0 && (uint64(dir.identity.Dev) != record.DirectoryDevice || dir.identity.Ino != record.DirectoryInode) {
		dir.file.Close()
		container.file.Close()
		return nil, nil, state("context directory was replaced")
	}
	return container, dir, nil
}

func verifyReservation(ctx context.Context, dir *directory, id, name string) error {
	runtime, err := openDirectory(dir, "state")
	if err != nil {
		return state("context reservation directory is missing or unsafe")
	}
	defer runtime.file.Close()
	data, err := readBounded(ctx, runtime, "reservation.json", maxRecord, true)
	if err != nil {
		return state("context reservation is missing or unsafe")
	}
	var record reservation
	if err := decodeRecord(data, maxRecord, &record); err != nil {
		return err
	}
	return validateReservation(record, id, name)
}

func readMutation(ctx context.Context, dir *directory) ([]byte, error) {
	runtime, err := openDirectory(dir, "state")
	if err != nil {
		return nil, err
	}
	defer runtime.file.Close()
	return readBounded(ctx, runtime, "mutation.json", maxRecord, true)
}

func (s *Store) ReadInputs(ctx context.Context, name, expectedID string) (desiredstate.Sources, error) {
	root, err := s.openRoot(ctx, false, nil)
	if err != nil {
		return desiredstate.Sources{}, safeError(err)
	}
	defer root.file.Close()
	if err := lockShared(root); err != nil {
		return desiredstate.Sources{}, err
	}
	defer syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN)
	registry, _, err := readRegistry(ctx, root)
	if err != nil {
		return desiredstate.Sources{}, safeError(err)
	}
	if err := verifyMappings(ctx, root, registry); err != nil {
		return desiredstate.Sources{}, safeError(err)
	}

	if !contextName(name) {
		return desiredstate.Sources{}, state("no valid current context is selected")
	}
	for _, record := range registry.Contexts {
		if record.Name == name {
			if expectedID != "" && record.ID != expectedID {
				return desiredstate.Sources{}, state("current context identity changed; select a context again")
			}
			if record.Mode != contexts.Ready {
				return desiredstate.Sources{}, state("context is incomplete; repeat its init or delete command")
			}
			if record.Revision == "" {
				return desiredstate.Sources{}, desiredstate.NewFailure("context.input", "context has no desired state; run context update --name "+record.Name+" --input-dir <dir>", "")
			}
			result, err := readSnapshot(ctx, root, record)
			return result, safeError(err)
		}
	}
	return desiredstate.Sources{}, state("requested context does not exist")
}

func (s *Store) Transact(ctx context.Context, create bool, inputs []string, callback func(contexts.Transaction) error) error {
	root, err := s.openRoot(ctx, create, inputs)
	if err != nil {
		return safeError(err)
	}
	defer root.file.Close()
	if err := lock(root); err != nil {
		return safeError(err)
	}
	defer syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN)
	registry, exists, err := readRegistry(ctx, root)
	var missing *missingRegistryError
	if create && errors.As(err, &missing) && missing.recoverable {
		recovered, recoveryErr := s.recoverInitialRegistry(ctx, root)
		if recoveryErr != nil {
			return safeError(recoveryErr)
		}
		if recovered {
			registry, exists, err = readRegistry(ctx, root)
		}
	}
	if err != nil {
		return safeError(err)
	}
	if !exists {
		if !create {
			return state("context store does not exist")
		}
		if err := s.writeRegistry(ctx, root, registry, nil); err != nil {
			return safeError(err)
		}
	}
	if err := verifyMappings(ctx, root, registry); err != nil {
		return safeError(err)
	}
	expected, err := registryExpectation(ctx, root, registry)
	if err != nil {
		return safeError(err)
	}
	tx := &transaction{store: s, root: root, registry: registry, leases: make(map[string]*directory), evidence: make(map[string][]byte)}
	tx.expected = expected
	defer tx.close()
	if callback == nil {
		return state("context transaction callback is missing")
	}
	if err := callback(tx); err != nil {
		return safeError(err)
	}
	return nil
}

type transaction struct {
	store     *Store
	root      *directory
	container *directory
	registry  contexts.Registry
	leases    map[string]*directory
	evidence  map[string][]byte
	committed bool
	closed    bool
	expected  *expectedRegistry
}

func (t *transaction) close() {
	t.closed = true
	for _, dir := range t.leases {
		syscall.Flock(int(dir.file.Fd()), syscall.LOCK_UN)
		dir.file.Close()
	}
	if t.container != nil {
		t.container.file.Close()
	}
}

func (t *transaction) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.closed || t.committed {
		return state("context transaction has already finished")
	}
	return t.root.verify()
}

func (t *transaction) Registry() contexts.Registry { return cloneRegistry(t.registry) }

func (t *transaction) record(id string) (contexts.Record, error) {
	for _, record := range t.registry.Contexts {
		if record.ID == id {
			return record, nil
		}
	}
	return contexts.Record{}, state("context identity has no registry reservation")
}

func (t *transaction) contextDirectory(ctx context.Context, id string) (*directory, error) {
	if !identifier(id, "ctx-") {
		return nil, state("context identity is invalid")
	}
	record, err := t.record(id)
	if err != nil {
		return nil, err
	}
	if t.container == nil {
		t.container, err = openDirectory(t.root, "contexts")
		if err != nil {
			return nil, err
		}
	}
	dir, err := openDirectory(t.container, record.Name)
	if err != nil {
		return nil, err
	}
	if record.DirectoryInode != 0 && (dir.identity.Ino != record.DirectoryInode || uint64(dir.identity.Dev) != record.DirectoryDevice) {
		dir.file.Close()
		return nil, state("context directory was replaced")
	}
	return dir, nil
}

func (t *transaction) save(ctx context.Context, registry contexts.Registry) error {
	slices.SortFunc(registry.Identities, func(a, b contexts.Identity) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	slices.SortFunc(registry.Contexts, func(a, b contexts.Record) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	if err := validateRegistry(registry); err != nil {
		return err
	}
	registry, err := t.store.upgradeRegistry(registry)
	if err != nil {
		return err
	}
	if err := t.store.writeRegistry(ctx, t.root, registry, t.expected); err != nil {
		return err
	}
	t.registry = cloneRegistry(registry)
	expected, err := registryExpectation(ctx, t.root, t.registry)
	if err != nil {
		return err
	}
	t.expected = expected
	return nil
}

// An interrupted rename can be visible without being durable. Explicit retries
// establish the recorded intent before they create or remove context content.
func (t *transaction) syncIntent(ctx context.Context) error {
	for pass := range 2 {
		actual, err := registryExpectation(ctx, t.root, t.registry)
		if err != nil {
			return err
		}
		if t.expected == nil || !sameFile(actual.identity, t.expected.identity) || !bytes.Equal(actual.data, t.expected.data) {
			return state("context intent changed before retry")
		}
		if pass == 0 {
			if err := t.store.syncDirectory(ctx, t.root); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *transaction) MutationState(ctx context.Context, id string) ([]byte, error) {
	if err := t.available(ctx); err != nil {
		return nil, err
	}
	if prior, ok := t.evidence[id]; ok {
		return slices.Clone(prior), nil
	}
	dir, err := t.contextDirectory(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := lock(dir); err != nil {
		dir.file.Close()
		return nil, err
	}
	t.leases[id] = dir
	if err := verifyContextLayout(ctx, dir); err != nil {
		return nil, err
	}
	if err := verifyReservation(ctx, dir, id, ""); err != nil {
		return nil, err
	}
	data, err := readMutation(ctx, dir)
	if err != nil {
		return nil, err
	}
	t.evidence[id] = slices.Clone(data)
	return data, nil
}

type expectedRegistry struct {
	data     []byte
	identity syscall.Stat_t
}

func registryExpectation(ctx context.Context, root *directory, registry contexts.Registry) (*expectedRegistry, error) {
	file, err := openRelative(root, "registry.json", pathHandle, 0)
	if err != nil {
		return nil, state("registry identity cannot be verified")
	}
	before, err := statHandle(file)
	file.Close()
	if err != nil {
		return nil, err
	}
	data, err := readBounded(ctx, root, "registry.json", maxRegistry, true)
	if err != nil {
		return nil, err
	}
	canonical, err := encodeRecord(registry, maxRegistry)
	if err != nil || !bytes.Equal(data, canonical) {
		return nil, state("registry changed during transaction acquisition")
	}
	file, err = openRelative(root, "registry.json", pathHandle, 0)
	if err != nil {
		return nil, state("registry identity cannot be verified")
	}
	after, err := statHandle(file)
	file.Close()
	if err != nil || !sameFile(before, after) {
		return nil, state("registry was replaced during transaction acquisition")
	}
	return &expectedRegistry{data: data, identity: after}, nil
}

func (s *Store) writeRegistry(ctx context.Context, root *directory, registry contexts.Registry, expected *expectedRegistry) error {
	data, err := encodeRecord(registry, maxRegistry)
	if err != nil {
		return err
	}
	var name string
	written := false
	for range 16 {
		candidate, err := s.candidate("pending-")
		if err != nil {
			return err
		}
		name = candidate + ".json"
		err = s.writeExclusive(ctx, root, name, data)
		if errors.Is(err, syscall.EEXIST) {
			continue
		}
		if err != nil {
			return err
		}
		written = true
		break
	}
	if !written {
		return state("registry publication exhausted its collision limit")
	}
	pendingIdentity, err := verifyPending(ctx, root, name, data)
	if err != nil {
		return err
	}
	if err := s.checkpoint(ctx, "before-registry-rename"); err != nil {
		return err
	}
	if err := root.verify(); err != nil {
		return err
	}
	if expected != nil {
		current, _, err := readRegistry(ctx, root)
		if err != nil {
			return err
		}
		actual, err := registryExpectation(ctx, root, current)
		if err != nil {
			return err
		}
		if !sameFile(expected.identity, actual.identity) || !bytes.Equal(expected.data, actual.data) {
			return state("registry was replaced or modified during the transaction")
		}
	} else {
		file, err := openRelative(root, "registry.json", pathHandle, 0)
		if file != nil {
			file.Close()
		}
		if !errors.Is(err, syscall.ENOENT) {
			return initialRegistryRecoveryError("initial registry destination unexpectedly exists")
		}
	}
	currentPending, err := verifyPending(ctx, root, name, data)
	if err != nil {
		return err
	}
	if !sameFile(pendingIdentity, currentPending) {
		if expected == nil {
			return initialRegistryRecoveryError("pending initial registry changed before publication")
		}
		return state("pending registry was replaced before publication")
	}
	if expected == nil {
		entry, sole, err := soleRootEntry(root)
		if err != nil {
			return initialRegistryRecoveryError("state root cannot be verified before initial registry publication")
		}
		if !sole || entry != name {
			return initialRegistryRecoveryError("state root changed before initial registry publication")
		}
	}
	if expected == nil {
		if err := renameNoReplaceAt(root, name, "registry.json"); err != nil {
			return initialRegistryRecoveryError("initial registry could not be atomically published")
		}
	} else if err := syscall.Renameat(int(root.file.Fd()), name, int(root.file.Fd()), "registry.json"); err != nil {
		return state("registry could not be atomically published")
	}
	publishedIdentity, err := verifyPending(ctx, root, "registry.json", data)
	if err != nil || !sameIdentity(currentPending, publishedIdentity) {
		return uncertainRegistryPublication(expected == nil)
	}
	if err := s.checkpoint(ctx, "after-registry-rename"); err != nil {
		return uncertainRegistryPublication(expected == nil)
	}
	if expected == nil {
		entry, sole, err := soleRootEntry(root)
		if err != nil || !sole || entry != "registry.json" {
			return uncertainInitialRegistryRecovery()
		}
	}
	if err := s.syncDirectory(ctx, root); err != nil {
		return uncertainRegistryPublication(expected == nil)
	}
	if expected == nil {
		entry, sole, err := soleRootEntry(root)
		if err != nil || !sole || entry != "registry.json" {
			return uncertainInitialRegistryRecovery()
		}
	}
	finalIdentity, err := verifyPending(ctx, root, "registry.json", data)
	if err != nil || !sameFile(publishedIdentity, finalIdentity) {
		return uncertainRegistryPublication(expected == nil)
	}
	return nil
}

func verifyPending(ctx context.Context, root *directory, name string, data []byte) (syscall.Stat_t, error) {
	file, err := openRelative(root, name, pathHandle, 0)
	if err != nil {
		return syscall.Stat_t{}, state("pending registry cannot be verified")
	}
	before, err := statHandle(file)
	file.Close()
	if err != nil {
		return syscall.Stat_t{}, err
	}
	actual, err := readBounded(ctx, root, name, maxRegistry, true)
	if err != nil || !bytes.Equal(data, actual) {
		return syscall.Stat_t{}, state("pending registry contents changed before publication")
	}
	file, err = openRelative(root, name, pathHandle, 0)
	if err != nil {
		return syscall.Stat_t{}, state("pending registry cannot be verified")
	}
	after, err := statHandle(file)
	file.Close()
	if err != nil || !sameFile(before, after) {
		return syscall.Stat_t{}, state("pending registry changed during verification")
	}
	return after, nil
}

func (t *transaction) Commit(ctx context.Context, registry contexts.Registry) error {
	if err := t.available(ctx); err != nil {
		return err
	}
	if err := validateRegistry(registry); err != nil {
		return err
	}
	if registry.Version != t.registry.Version || registry.IDNamespace != t.registry.IDNamespace || registry.NextIdentity != t.registry.NextIdentity || !slices.Equal(registry.Identities, t.registry.Identities) || len(registry.Contexts) != len(t.registry.Contexts) {
		return state("context commit cannot change reserved identities")
	}
	for _, prior := range t.registry.Contexts {
		index := slices.IndexFunc(registry.Contexts, func(next contexts.Record) bool { return next.ID == prior.ID })
		if index < 0 {
			return state("context commit cannot remove identities")
		}
		next := registry.Contexts[index]
		if next == prior {
			continue
		}
		if next.Name != prior.Name || next.DirectoryDevice != prior.DirectoryDevice || next.DirectoryInode != prior.DirectoryInode || next.SecretStoreType != prior.SecretStoreType {
			return state("context identity or secret implementation cannot change")
		}
		if prior.EnvironmentDirectory != "" && next.EnvironmentDirectory != prior.EnvironmentDirectory || prior.Revision != "" && next.Revision == "" {
			return state("context replacement cannot discard its bound input identity")
		}
		if prior.Mode == contexts.Deleting || next.Mode != contexts.Ready {
			return state("context status transition is invalid")
		}
		if next != prior {
			if prior.Mode == contexts.Initializing {
				if err := t.store.checkpoint(ctx, "before-context-ready"); err != nil {
					return err
				}
			}
			dir, held := t.leases[next.ID]
			if !held {
				return state("context publication requires its mutation lease")
			}
			if err := dir.verify(); err != nil {
				return err
			}
			current, err := readMutation(ctx, dir)
			if err != nil || !bytes.Equal(current, t.evidence[next.ID]) {
				return state("context mutation evidence changed before publication")
			}
			if next.Revision != "" {
				if _, err := readSnapshot(ctx, t.root, next); err != nil {
					return err
				}
			}
			if prior.Mode == contexts.Initializing {
				remaining := maxContextEntries
				if err := t.store.walkContextTree(ctx, dir, "", syncContextTree, &remaining); err != nil {
					return err
				}
				if err := t.store.syncDirectory(ctx, t.container); err != nil {
					return err
				}
			}
		}
	}
	for id, dir := range t.leases {
		if err := verifyContextLayout(ctx, dir); err != nil {
			return err
		}
		current, err := readMutation(ctx, dir)
		if err != nil || !bytes.Equal(current, t.evidence[id]) {
			return state("context mutation evidence changed before publication")
		}
	}
	if err := verifyMappings(ctx, t.root, registry); err != nil {
		return err
	}
	if err := t.save(ctx, registry); err != nil {
		return err
	}
	t.committed = true
	for _, record := range registry.Contexts {
		if err := t.collectRevisions(ctx, record.ID); err != nil {
			return state("context was published, but input revision cleanup is incomplete; inspect the context before retrying")
		}
	}
	return nil
}

func blobName(index int) string {
	const digits = "0123456789"
	name := []byte("file-0000")
	for n := len(name) - 1; n >= 5; n-- {
		name[n] = digits[index%10]
		index /= 10
	}
	return string(name)
}

func revisionEntries(dir *directory, prefix, suffix string) ([]string, error) {
	names, err := directoryNames(dir, maxRevisions)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		candidate := name
		if suffix != "" {
			if filepath.Ext(name) != suffix {
				return nil, state("retained state contains an unknown entry")
			}
			candidate = name[:len(name)-len(suffix)]
		}
		if !identifier(candidate, prefix) {
			return nil, state("retained state contains an unknown entry")
		}
	}
	slices.Sort(names)
	return names, nil
}
