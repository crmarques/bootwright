//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

var _ contexts.Repository = (*Store)(nil)

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
		return contexts.Registry{}, false, state("nonempty state root has no registry; recovery is required")
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
	return registry, true, nil
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

func (s *Store) candidate(prefix string) (string, error) {
	var bytes [16]byte
	if s.random == nil {
		return "", state("context identity randomness is unavailable")
	}
	if _, err := io.ReadFull(s.random, bytes[:]); err != nil {
		return "", state("context identity randomness is unavailable")
	}
	return prefix + hex.EncodeToString(bytes[:]), nil
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
			return state("initial registry destination unexpectedly exists")
		}
	}
	currentPending, err := verifyPending(ctx, root, name, data)
	if err != nil {
		return err
	}
	if !sameFile(pendingIdentity, currentPending) {
		return state("pending registry was replaced before publication")
	}
	if err := syscall.Renameat(int(root.file.Fd()), name, int(root.file.Fd()), "registry.json"); err != nil {
		return state("registry could not be atomically published")
	}
	if err := s.checkpoint(ctx, "after-registry-rename"); err != nil {
		return state("registry publication has uncertain durability; inspect the selected context before retrying")
	}
	if err := s.syncDirectory(ctx, root); err != nil {
		return state("registry publication has uncertain durability; inspect the selected context before retrying")
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
	if !slices.Equal(registry.Identities, t.registry.Identities) || len(registry.Contexts) != len(t.registry.Contexts) {
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
	names, err := dir.file.Readdirnames(maxRevisions + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, state("retained state cannot be enumerated")
	}
	if len(names) >= maxRevisions {
		return nil, state("retained revision count exceeds its limit")
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
