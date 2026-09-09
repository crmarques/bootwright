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
	if len(registry.Identities) == 0 {
		return root.verify()
	}
	container, err := openDirectory(root, "contexts")
	if err != nil {
		return state("context identity directory is missing or unsafe")
	}
	defer container.file.Close()
	for _, identity := range registry.Identities {
		if err := ctx.Err(); err != nil {
			return err
		}
		dir, err := openDirectory(container, identity.ID)
		if err != nil {
			return state("mapped context reservation is missing or unsafe")
		}
		err = verifyReservation(ctx, dir, identity.ID, identity.EnvironmentDirectory)
		dir.file.Close()
		if err != nil {
			return err
		}
	}
	manifestSizes := make([]int, len(registry.Contexts))
	manifestBytes := 0
	for index, record := range registry.Contexts {
		if err := ctx.Err(); err != nil {
			return err
		}
		parent := container
		owned := []*directory{}
		var lookupErr error
		for _, name := range []string{record.ID, "revisions", record.Revision} {
			next, err := openDirectory(parent, name)
			if err != nil {
				lookupErr = err
				break
			}
			owned = append(owned, next)
			parent = next
		}
		if lookupErr == nil {
			file, err := openRelative(parent, "manifest.json", pathHandle, 0)
			if err != nil {
				lookupErr = err
			} else {
				stat, err := statHandle(file)
				file.Close()
				if err != nil || !private(stat, syscall.S_IFREG) || stat.Size < 0 || stat.Size > maxManifest {
					lookupErr = state("manifest handle is unsafe")
				} else {
					manifestSizes[index] = int(stat.Size)
					manifestBytes += int(stat.Size)
					if manifestBytes > maxAllManifests {
						lookupErr = state("aggregate referenced manifest bytes exceed their limit")
					}
				}
			}
		}
		for i := len(owned) - 1; i >= 0; i-- {
			owned[i].file.Close()
		}
		if lookupErr != nil {
			return safeError(lookupErr)
		}
	}
	for index, record := range registry.Contexts {
		_, _, close, err := openManifestBounded(ctx, root, record, manifestSizes[index])
		if err != nil {
			return err
		}
		close()
	}
	return nil
}

func verifyReservation(ctx context.Context, dir *directory, id, environment string) error {
	data, err := readBounded(ctx, dir, "reservation.json", maxRecord, true)
	if err != nil {
		return state("context reservation is missing or unsafe")
	}
	var record reservation
	if err := decodeRecord(data, maxRecord, &record); err != nil {
		return err
	}
	return validateReservation(record, id, environment)
}

func (s *Store) ReadInputs(ctx context.Context, name string) (desiredstate.Sources, error) {
	root, err := s.openRoot(ctx, false, nil)
	if err != nil {
		return desiredstate.Sources{}, safeError(err)
	}
	defer root.file.Close()
	registry, _, err := readRegistry(ctx, root)
	if err != nil {
		return desiredstate.Sources{}, safeError(err)
	}
	if err := verifyMappings(ctx, root, registry); err != nil {
		return desiredstate.Sources{}, safeError(err)
	}
	if name == "" {
		name = registry.Current
	}
	if !contextName(name) {
		return desiredstate.Sources{}, state("no valid current context is selected")
	}
	for _, record := range registry.Contexts {
		if record.Name == name {
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
	tx := &transaction{store: s, root: root, registry: registry, reservations: make(map[string]string), leases: make(map[string]*directory), evidence: make(map[string][]byte)}
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
	store        *Store
	root         *directory
	container    *directory
	registry     contexts.Registry
	reservations map[string]string
	leases       map[string]*directory
	evidence     map[string][]byte
	committed    bool
	closed       bool
	archived     map[string]string
	expected     *expectedRegistry
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

func (t *transaction) contextDirectory(ctx context.Context, id string) (*directory, error) {
	if !identifier(id, "ctx-") {
		return nil, state("context identity is invalid")
	}
	known := t.reservations[id] != ""
	for _, identity := range t.registry.Identities {
		if identity.ID == id {
			known = true
			break
		}
	}
	if !known {
		return nil, state("context identity has no published or current reservation")
	}
	if t.container == nil {
		container, err := openDirectory(t.root, "contexts")
		if err != nil {
			return nil, err
		}
		t.container = container
	}
	return openDirectory(t.container, id)
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

func (t *transaction) Reserve(ctx context.Context, environment string) (string, error) {
	if err := t.available(ctx); err != nil {
		return "", err
	}
	if !canonicalPath(environment) || beneath(environment, t.root.path) {
		return "", state("context Environment directory is invalid or overlaps runtime state")
	}
	for _, identity := range t.registry.Identities {
		if identity.EnvironmentDirectory == environment {
			return identity.ID, nil
		}
	}
	for id, path := range t.reservations {
		if path == environment {
			return id, nil
		}
	}
	if len(t.registry.Identities)+len(t.reservations) >= maxIdentities {
		return "", state("context identity count exceeds its limit")
	}
	if t.container == nil {
		container, err := t.store.ensureDirectory(ctx, t.root, "contexts")
		if err != nil {
			return "", safeError(err)
		}
		t.container = container
	}
	scan, err := openDirectory(t.root, "contexts")
	if err != nil {
		return "", safeError(err)
	}
	names, err := scan.file.Readdirnames(maxIdentities + 1)
	scan.file.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return "", state("context reservations cannot be enumerated")
	}
	if len(names) >= maxIdentities {
		return "", state("retained context reservation count exceeds its limit")
	}
	for _, name := range names {
		if !identifier(name, "ctx-") {
			return "", state("context reservation directory contains unknown state")
		}
	}
	for range 16 {
		id, err := t.store.candidate("ctx-")
		if err != nil {
			return "", err
		}
		dir, err := t.store.newDirectory(ctx, t.container, id)
		if errors.Is(err, syscall.EEXIST) {
			continue
		}
		if err != nil {
			return "", safeError(err)
		}
		data, err := encodeRecord(reservation{Version: 1, ID: id, EnvironmentDirectory: environment}, maxRecord)
		if err == nil {
			err = t.store.writeExclusive(ctx, dir, "reservation.json", data)
		}
		if err == nil {
			err = t.store.writeExclusive(ctx, dir, "mutation.json", []byte("{\"version\":1,\"operation\":\"none\",\"ownership\":\"none\"}\n"))
		}
		dir.file.Close()
		if err != nil {
			return "", safeError(err)
		}
		t.reservations[id] = environment
		return id, nil
	}
	return "", state("context identity reservation exhausted its collision limit")
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
		return nil, safeError(err)
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
	data, err := readBounded(ctx, dir, "mutation.json", maxRecord, true)
	if err != nil {
		return nil, safeError(err)
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
	for _, prior := range t.registry.Identities {
		found := false
		for _, next := range registry.Identities {
			if next == prior {
				found = true
				break
			}
		}
		if !found {
			return state("permanent context identity cannot be removed or reassigned")
		}
	}
	for _, next := range registry.Identities {
		if !slices.Contains(t.registry.Identities, next) && t.reservations[next.ID] != next.EnvironmentDirectory {
			return state("unpublished context reservation cannot be adopted")
		}
	}
	for _, prior := range t.registry.Contexts {
		found := false
		for _, next := range registry.Contexts {
			if next.ID == prior.ID {
				found = true
				if next.Mode != prior.Mode && (next.Mode != contexts.RecoveryOnly || t.archived[prior.ID] != "recoveryOnly") {
					return state("context mode transition has no durable archive")
				}
				break
			}
		}
		if !found && t.archived[prior.ID] != "deleted" {
			return state("context deletion has no durable archive")
		}
	}
	if err := verifyMappings(ctx, t.root, registry); err != nil {
		return err
	}
	for _, record := range registry.Contexts {
		unchanged := slices.Contains(t.registry.Contexts, record)
		if !unchanged {
			if _, ok := t.leases[record.ID]; !ok {
				return state("context publication requires its mutation lease")
			}
			if _, err := readSnapshot(ctx, t.root, record); err != nil {
				return safeError(err)
			}
		}
	}
	for _, dir := range t.leases {
		if err := dir.verify(); err != nil {
			return err
		}
	}
	for id, dir := range t.leases {
		current, err := readBounded(ctx, dir, "mutation.json", maxRecord, true)
		if err != nil || !bytes.Equal(current, t.evidence[id]) {
			return state("context mutation evidence changed before publication")
		}
	}
	t.committed = true
	return safeError(t.store.writeRegistry(ctx, t.root, cloneRegistry(registry), t.expected))
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
		return nil, state("retained revision or archive count exceeds its limit")
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
