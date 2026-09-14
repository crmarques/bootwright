//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

var _ secretstore.Workspace = (*Store)(nil)

func secretCorrupt(message string) error { return secretstore.Failure("store.corrupt", message) }

func secretLimit(message string) error { return secretstore.Failure("store.limit", message) }

func secretCorruption(ctx context.Context, message string, err error) error {
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return secretCorrupt(message)
}

func secretConflict(ctx context.Context, message string, err error) error {
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return secretstore.Failure("store.conflict", message)
}

func secretEffectFailure(ctx context.Context, message string, err error) error {
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	var failure *diagnostics.Failure
	if errors.As(err, &failure) {
		if len(failure.Diagnostics) == 1 && strings.HasPrefix(failure.Diagnostics[0].Code, "secret.store.") {
			return err
		}
		return secretCorrupt(message)
	}
	return secretstore.Failure("store.conflict", message)
}

func (s *Store) SecretContext(ctx context.Context, name string) (secretstore.ContextSnapshot, error) {
	root, err := s.openRoot(ctx, false, nil)
	if err != nil {
		return secretstore.ContextSnapshot{}, safeError(err)
	}
	defer root.file.Close()
	if err := lockShared(root); err != nil {
		return secretstore.ContextSnapshot{}, err
	}
	defer syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN)
	registry, _, err := readRegistry(ctx, root)
	if err == nil {
		err = verifyMappings(ctx, root, registry)
	}
	if err != nil {
		return secretstore.ContextSnapshot{}, safeError(err)
	}

	record, err := namedSecretRecord(registry, name)
	if err != nil {
		return secretstore.ContextSnapshot{}, err
	}
	if record.Mode != contexts.Ready {
		return secretstore.ContextSnapshot{}, state("context is incomplete; repeat its init or delete command")
	}
	inputs := desiredstate.Sources{}
	if record.Revision != "" {
		inputs, err = readSnapshot(ctx, root, record)
		if err != nil {
			return secretstore.ContextSnapshot{}, safeError(err)
		}
	}
	return secretstore.ContextSnapshot{Context: secretContext(record), Inputs: inputs, SecretStoreType: record.SecretStoreType}, nil
}

func (s *Store) ReadSecrets(ctx context.Context, expected secretstore.Context, callback func(secretstore.Area) error) error {
	if callback == nil {
		return secretstore.Failure("store.implementation", "secret storage callback is missing")
	}
	root, err := s.openRoot(ctx, false, nil)
	if err != nil {
		return safeError(err)
	}
	defer root.file.Close()
	if err := lockShared(root); err != nil {
		return err
	}
	defer syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN)
	registry, _, err := readRegistry(ctx, root)
	if err == nil {
		err = verifyMappings(ctx, root, registry)
	}
	if err != nil {
		return safeError(err)
	}
	record, err := exactSecretRecord(registry, expected)
	if err != nil {
		return err
	}
	if record.Mode != contexts.Ready {
		return state("context is incomplete; repeat its init or delete command")
	}
	container, dir, err := openSecretContext(ctx, root, record)
	if err != nil {
		return safeError(err)
	}
	defer container.file.Close()
	defer dir.file.Close()
	area := &secretArea{store: s, root: root, context: dir, readOnly: true, active: true, mutable: make(map[string]secretExpectation)}
	defer func() {
		if area.secrets != nil {
			area.secrets.file.Close()
		}
	}()
	defer area.close()
	if secrets, openErr := openDirectory(dir, "secrets"); openErr == nil {
		area.secrets = secrets
	} else if !errors.Is(openErr, syscall.ENOENT) {
		return secretCorrupt("secret storage directory is unsafe")
	}
	if _, _, err := area.scan(ctx); err != nil {
		return safeError(err)
	}
	err = callback(area)
	return safeError(err)
}

func (s *Store) MutateSecrets(ctx context.Context, expected secretstore.Context, callback func(secretstore.Area) error) error {
	if callback == nil {
		return secretstore.Failure("store.implementation", "secret storage callback is missing")
	}
	root, err := s.openRoot(ctx, false, nil)
	if err != nil {
		return safeError(err)
	}
	defer root.file.Close()
	if err := lock(root); err != nil {
		return secretConflict(ctx, "secret storage is held by another mutator", err)
	}
	defer syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN)
	registry, _, err := readRegistry(ctx, root)
	if err == nil {
		err = verifyMappings(ctx, root, registry)
	}
	if err != nil {
		return safeError(err)
	}
	record, err := exactSecretRecord(registry, expected)
	if err != nil {
		return err
	}
	if record.Mode != contexts.Ready {
		return state("context is incomplete; repeat its init or delete command")
	}
	registryExpected, err := registryExpectation(ctx, root, registry)
	if err != nil {
		return safeError(err)
	}
	container, dir, err := openSecretContext(ctx, root, record)
	if err != nil {
		return safeError(err)
	}
	defer container.file.Close()
	defer dir.file.Close()
	if err := lock(dir); err != nil {
		return secretConflict(ctx, "secret context is held by another mutator", err)
	}
	defer syscall.Flock(int(dir.file.Fd()), syscall.LOCK_UN)
	if err := verifySecretContextLayout(ctx, dir); err != nil {
		return safeError(err)
	}
	area := &secretArea{store: s, root: root, context: dir, expected: registryExpected, token: expected, active: true, mutable: make(map[string]secretExpectation)}
	defer func() {
		if area.secrets != nil {
			area.secrets.file.Close()
		}
	}()
	defer area.close()
	if secrets, openErr := openDirectory(dir, "secrets"); openErr == nil {
		area.secrets = secrets
	} else if !errors.Is(openErr, syscall.ENOENT) {
		return secretCorrupt("secret storage directory is unsafe")
	}
	if _, _, err := area.scan(ctx); err != nil {
		return safeError(err)
	}
	err = callback(area)
	return safeError(err)
}

func secretContext(record contexts.Record) secretstore.Context {
	return secretstore.Context{Name: record.Name, Mode: string(record.Mode), Revision: record.Revision}
}

func namedSecretRecord(registry contexts.Registry, name string) (contexts.Record, error) {
	if !contextName(name) {
		return contexts.Record{}, state("no valid current context is selected")
	}
	for _, record := range registry.Contexts {
		if record.Name == name {
			return record, nil
		}
	}
	return contexts.Record{}, state("requested context does not exist")
}

func exactSecretRecord(registry contexts.Registry, expected secretstore.Context) (contexts.Record, error) {
	record, err := namedSecretRecord(registry, expected.Name)
	if err != nil {
		return contexts.Record{}, err
	}
	if secretContext(record) != expected {
		return contexts.Record{}, secretstore.Failure("store.conflict", "context identity changed before secret access")
	}
	return record, nil
}

func openSecretContext(ctx context.Context, root *directory, record contexts.Record) (*directory, *directory, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	container, dir, err := openContext(root, record)
	if err != nil {
		return nil, nil, err
	}
	if err := verifyReservation(ctx, dir, record.Name); err != nil {
		dir.file.Close()
		container.file.Close()
		return nil, nil, err
	}
	return container, dir, nil
}

func verifyContextLayout(ctx context.Context, dir *directory) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	names, err := directoryNames(dir, 4)
	if err != nil {
		return state("context layout cannot be verified")
	}
	for _, name := range names {
		switch name {
		case "context.yaml":
			if _, err := readBounded(ctx, dir, name, maxRecord, true); err != nil {
				return err
			}
		case "state", "desired-state", "secrets":
			child, err := openDirectory(dir, name)
			if err != nil {
				return state("context subdirectory is unsafe")
			}
			if name != "secrets" {
				maximum := 1
				if name == "state" {
					maximum = 3
				}
				entries, listErr := directoryNames(child, maximum)
				if listErr == nil {
					for _, entry := range entries {
						if name == "state" && entry != "reservation.json" && entry != "mutation.json" && entry != "operations" || name == "desired-state" && entry != "revisions" {
							listErr = state("context contains unsupported state; mutation is refused")
							break
						}
					}
				}
				if listErr != nil {
					child.file.Close()
					return listErr
				}
			}
			child.file.Close()
		default:
			return state("context contains unsupported state; mutation is refused")
		}
	}
	return dir.verify()
}

func verifySecretContextLayout(ctx context.Context, dir *directory) error {
	secrets, err := openDirectory(dir, "secrets")
	if err == nil {
		secrets.file.Close()
	} else if !errors.Is(err, syscall.ENOENT) {
		return secretCorruption(ctx, "secret storage directory is unsafe", err)
	}
	return verifyContextLayout(ctx, dir)
}

func directoryNames(dir *directory, maximum int) ([]string, error) {
	copy, err := openDirectory(dir.parent, dir.name)
	if err != nil {
		return nil, err
	}
	defer copy.file.Close()
	names := make([]string, 0, min(maximum, 1024))
	for len(names) <= maximum {
		want := min(1024, maximum+1-len(names))
		part, readErr := copy.file.Readdirnames(want)
		names = append(names, part...)
		if len(names) > maximum {
			return nil, state("state directory entry count exceeds its limit")
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil || len(part) == 0 {
			return nil, state("state directory cannot be enumerated")
		}
	}
	slices.Sort(names)
	return names, copy.verify()
}

type secretExpectation struct {
	exists         bool
	data           []byte
	identity       syscall.Stat_t
	parentIdentity syscall.Stat_t
	parentKnown    bool
}

type secretPublicationPhase uint8

const (
	secretBeforePublication secretPublicationPhase = iota
	secretPublishing
	secretCommitted
	secretUncertain
)

type secretArea struct {
	store    *Store
	root     *directory
	context  *directory
	secrets  *directory
	expected *expectedRegistry
	token    secretstore.Context
	mutable  map[string]secretExpectation
	observed map[string]secretExpectation
	readOnly bool
	active   bool
	phase    secretPublicationPhase
}

func (a *secretArea) forgetExpectation(path string) {
	if expected, exists := a.mutable[path]; exists {
		clear(expected.data)
		delete(a.mutable, path)
	}
}

func (a *secretArea) rememberReadExpectation(path string, next secretExpectation) error {
	if prior, exists := a.mutable[path]; exists {
		if prior.exists != next.exists || prior.parentKnown != next.parentKnown || prior.parentKnown && !sameIdentity(prior.parentIdentity, next.parentIdentity) || prior.exists && (!sameFile(prior.identity, next.identity) || !bytes.Equal(prior.data, next.data)) {
			return secretCorrupt("secret storage observation changed during the callback")
		}
		return nil
	}
	next.data = slices.Clone(next.data)
	a.mutable[path] = next
	return nil
}

func (a *secretArea) close() {
	a.active = false
	for path := range a.mutable {
		a.forgetExpectation(path)
	}
	a.observed = nil
}

func (a *secretArea) available(ctx context.Context, mutation bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !a.active || mutation && (a.phase == secretCommitted || a.phase == secretUncertain) {
		return secretConflict(ctx, "secret storage callback has already finished", nil)
	}
	if mutation && a.readOnly {
		return secretConflict(ctx, "read-only secret storage refuses mutation", nil)
	}
	if err := a.root.verify(); err != nil {
		return secretConflict(ctx, "context state changed during secret access", err)
	}
	if err := a.context.verify(); err != nil {
		return secretConflict(ctx, "context identity changed during secret access", err)
	}
	if a.secrets != nil {
		if err := a.secrets.verify(); err != nil {
			return secretCorruption(ctx, "secret storage directory is unsafe", err)
		}
	}
	return nil
}

func secretPath(path string, minimum, maximum int) ([]string, error) {
	if path == "" && minimum == 0 {
		return []string{}, nil
	}
	parts := strings.Split(path, "/")
	if len(parts) < minimum || len(parts) > maximum {
		return nil, secretCorrupt("secret storage path depth is invalid")
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > 255 || part == "." || part == ".." {
			return nil, secretCorrupt("secret storage path component is invalid")
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
				return nil, secretCorrupt("secret storage path component is invalid")
			}
		}
	}
	return parts, nil
}

func (a *secretArea) ensureRoot(ctx context.Context) error {
	if a.secrets != nil {
		if err := a.secrets.verify(); err != nil {
			return secretCorruption(ctx, "secret storage directory is unsafe", err)
		}
		return nil
	}
	a.phase = secretPublishing
	dir, err := a.store.newDirectory(ctx, a.context, "secrets")
	if err != nil {
		return secretEffectFailure(ctx, "secret storage directory could not be created safely", err)
	}
	a.secrets = dir
	return nil
}

func (a *secretArea) parent(ctx context.Context, parts []string) (*directory, string, func(), error) {
	if a.secrets == nil {
		return nil, "", func() {}, syscall.ENOENT
	}
	if len(parts) == 1 {
		return a.secrets, parts[0], func() {}, nil
	}
	parent, err := openDirectory(a.secrets, parts[0])
	if err != nil {
		return nil, "", func() {}, err
	}
	return parent, parts[1], func() { parent.file.Close() }, nil
}

func (a *secretArea) Read(ctx context.Context, path string, maximum int) ([]byte, bool, error) {
	return a.read(ctx, path, maximum, false)
}

func (a *secretArea) ReadMutable(ctx context.Context, path string, maximum int) ([]byte, bool, error) {
	return a.read(ctx, path, maximum, true)
}

func (a *secretArea) read(ctx context.Context, path string, maximum int, mutable bool) ([]byte, bool, error) {
	if err := a.available(ctx, false); err != nil {
		return nil, false, err
	}
	if maximum < 0 || maximum > maxSecretBytes {
		return nil, false, secretLimit("secret storage read limit is invalid")
	}
	parts, err := secretPath(path, 1, 2)
	if err != nil {
		return nil, false, err
	}
	parent, name, close, err := a.parent(ctx, parts)
	if errors.Is(err, syscall.ENOENT) {
		if mutable {
			if err := a.rememberReadExpectation(path, secretExpectation{}); err != nil {
				return nil, false, err
			}
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, secretCorruption(ctx, "secret storage path is unsafe", err)
	}
	defer close()
	data, identity, err := readBoundedIdentity(ctx, parent, name, maximum, !mutable)
	if errors.Is(err, syscall.ENOENT) {
		if mutable {
			if err := a.rememberReadExpectation(path, secretExpectation{parentIdentity: parent.identity, parentKnown: true}); err != nil {
				return nil, false, err
			}
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, secretCorruption(ctx, "secret storage file is unsafe", err)
	}
	if mutable {
		if err := a.rememberReadExpectation(path, secretExpectation{exists: true, data: data, identity: identity, parentIdentity: parent.identity, parentKnown: true}); err != nil {
			clear(data)
			return nil, false, err
		}
		a.observe(path, parent.identity, identity)
	}
	return data, true, nil
}

func (a *secretArea) Entries(ctx context.Context, path string) ([]secretstore.Entry, error) {
	if err := a.available(ctx, false); err != nil {
		return nil, err
	}
	parts, err := secretPath(path, 0, 1)
	if err != nil {
		return nil, err
	}
	if a.secrets == nil {
		if len(parts) == 0 {
			return []secretstore.Entry{}, nil
		}
		return nil, secretCorrupt("secret storage directory does not exist")
	}
	if _, _, err := a.scan(ctx); err != nil {
		return nil, err
	}
	dir := a.secrets
	if len(parts) == 1 {
		dir, err = openDirectory(a.secrets, parts[0])
		if err != nil {
			return nil, secretCorruption(ctx, "secret storage directory is unsafe", err)
		}
		defer dir.file.Close()
	}
	names, err := secretDirectoryNames(ctx, dir, maxSecretEntries)
	if err != nil {
		return nil, err
	}
	observations := make(map[string]syscall.Stat_t, len(names))
	entries, err := inspectSecretDirectoryNamesObserved(ctx, dir, names, len(parts) == 0, a.readOnly, func(name string, identity syscall.Stat_t) {
		if path != "" {
			name = path + "/" + name
		}
		observations[name] = identity
	})
	if err != nil {
		return nil, err
	}
	for name, identity := range observations {
		a.observe(name, dir.identity, identity)
	}
	return entries, nil
}

func listSecretDirectory(ctx context.Context, dir *directory, allowDirectories, allowVanishedPending bool) ([]secretstore.Entry, error) {
	names, err := secretDirectoryNames(ctx, dir, maxSecretEntries)
	if err != nil {
		return nil, err
	}
	return inspectSecretDirectoryNames(ctx, dir, names, allowDirectories, allowVanishedPending)
}

func inspectSecretDirectoryNames(ctx context.Context, dir *directory, names []string, allowDirectories, allowVanishedPending bool) ([]secretstore.Entry, error) {
	return inspectSecretDirectoryNamesObserved(ctx, dir, names, allowDirectories, allowVanishedPending, nil)
}

func inspectSecretDirectoryNamesObserved(ctx context.Context, dir *directory, names []string, allowDirectories, allowVanishedPending bool, observe func(string, syscall.Stat_t)) ([]secretstore.Entry, error) {
	entries := make([]secretstore.Entry, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := secretPath(name, 1, 1); err != nil {
			return nil, secretCorrupt("secret storage contains an invalid entry")
		}
		file, err := openRelative(dir, name, pathHandle, 0)
		if errors.Is(err, syscall.ENOENT) && allowVanishedPending && secretPendingName(name) {
			continue
		}
		if err != nil {
			return nil, secretCorruption(ctx, "secret storage entry is unsafe", err)
		}
		stat, statErr := statHandle(file)
		file.Close()
		if statErr != nil {
			return nil, secretCorruption(ctx, "secret storage entry is unsafe", statErr)
		}
		if stat.Dev != dir.identity.Dev {
			return nil, secretCorrupt("secret storage entry is unsafe")
		}
		switch stat.Mode & syscall.S_IFMT {
		case syscall.S_IFDIR:
			if !allowDirectories || !private(stat, syscall.S_IFDIR, dir.identity.Uid, dir.identity.Gid) || stat.Mode&0777 != 0700 {
				return nil, secretCorrupt("secret storage directory is unsafe")
			}
			child, err := openDirectory(dir, name)
			if err != nil {
				return nil, secretCorruption(ctx, "secret storage directory is unsafe", err)
			}
			child.file.Close()
			entries = append(entries, secretstore.Entry{Name: name, Directory: true})
		case syscall.S_IFREG:
			if !private(stat, syscall.S_IFREG, dir.identity.Uid, dir.identity.Gid) || stat.Mode&0777 != 0600 || stat.Size < 0 || stat.Size > maxSecretBytes {
				return nil, secretCorrupt("secret storage file is unsafe")
			}
			entries = append(entries, secretstore.Entry{Name: name, Size: stat.Size})
		default:
			return nil, secretCorrupt("secret storage entry type is unsafe")
		}
		if observe != nil {
			observe(name, stat)
		}
	}
	if err := dir.verify(); err != nil {
		return nil, secretCorruption(ctx, "secret storage directory changed during enumeration", err)
	}
	return entries, nil
}

func secretPendingName(name string) bool {
	const prefix = "pending-"
	if len(name) != len(prefix)+32 || !strings.HasPrefix(name, prefix) {
		return false
	}
	for _, c := range name[len(prefix):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func secretDirectoryNames(ctx context.Context, dir *directory, maximum int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	copy, err := openDirectory(dir.parent, dir.name)
	if err != nil {
		return nil, secretCorruption(ctx, "secret storage directory cannot be enumerated", err)
	}
	defer copy.file.Close()
	names := make([]string, 0, min(maximum, 1024))
	for len(names) <= maximum {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		want := min(1024, maximum+1-len(names))
		part, readErr := copy.file.Readdirnames(want)
		names = append(names, part...)
		if len(names) > maximum {
			return nil, secretLimit("secret storage directory entry count exceeds its limit")
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil || len(part) == 0 {
			return nil, secretCorruption(ctx, "secret storage directory cannot be enumerated", readErr)
		}
	}
	slices.Sort(names)
	if err := copy.verify(); err != nil {
		return nil, secretCorruption(ctx, "secret storage directory changed during enumeration", err)
	}
	return names, nil
}

func (a *secretArea) scan(ctx context.Context) (int, int64, error) {
	if a.secrets == nil {
		return 0, 0, nil
	}
	rootEntries, err := listSecretDirectory(ctx, a.secrets, true, a.readOnly)
	if err != nil {
		return 0, 0, err
	}
	count, size := len(rootEntries), int64(0)
	for _, entry := range rootEntries {
		if !entry.Directory {
			size += entry.Size
			continue
		}
		dir, err := openDirectory(a.secrets, entry.Name)
		if err != nil {
			return 0, 0, secretCorruption(ctx, "secret storage directory is unsafe", err)
		}
		children, childErr := listSecretDirectory(ctx, dir, false, a.readOnly)
		dir.file.Close()
		if childErr != nil {
			return 0, 0, childErr
		}
		count += len(children)
		if count > maxSecretEntries {
			return 0, 0, secretLimit("secret storage physical entry count exceeds its limit")
		}
		for _, child := range children {
			size += child.Size
			if size > maxSecretBytes {
				return 0, 0, secretLimit("secret storage physical bytes exceed their limit")
			}
		}
	}
	if count > maxSecretEntries || size > maxSecretBytes {
		return 0, 0, secretLimit("secret storage physical bounds are exceeded")
	}
	return count, size, nil
}

func (a *secretArea) capacity(ctx context.Context, entries int, bytes int64) error {
	count, size, err := a.scan(ctx)
	if err != nil {
		return err
	}
	if entries < 0 || bytes < 0 || count > maxSecretEntries-entries || size > maxSecretBytes-bytes {
		return secretLimit("secret storage physical bounds would be exceeded")
	}
	return nil
}

func (a *secretArea) EnsureDirectory(ctx context.Context, path string) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	parts, err := secretPath(path, 1, 1)
	if err != nil {
		return err
	}
	if err := a.ensureRoot(ctx); err != nil {
		return err
	}
	if child, err := openDirectory(a.secrets, parts[0]); err == nil {
		child.file.Close()
		return nil
	} else if !errors.Is(err, syscall.ENOENT) {
		return secretCorruption(ctx, "secret storage directory is unsafe", err)
	}
	if err := a.capacity(ctx, 1, 0); err != nil {
		return err
	}
	a.phase = secretPublishing
	child, err := a.store.newDirectory(ctx, a.secrets, parts[0])
	if err != nil {
		return secretEffectFailure(ctx, "secret storage directory could not be created safely", err)
	}
	return child.file.Close()
}

func (a *secretArea) WriteExclusive(ctx context.Context, path string, data []byte) error {
	return a.writeExclusive(ctx, path, data, false)
}

func (a *secretArea) PublishExclusive(ctx context.Context, path string, data []byte) error {
	return a.writeExclusive(ctx, path, data, true)
}

func (a *secretArea) writeExclusive(ctx context.Context, path string, data []byte, atomic bool) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	parts, err := secretPath(path, 1, 2)
	if err != nil {
		return err
	}
	if len(data) > maxSecretBytes {
		return secretLimit("secret storage file exceeds its byte limit")
	}
	a.phase = secretPublishing
	if err := a.ensureRoot(ctx); err != nil {
		return err
	}
	if err := a.capacity(ctx, 1, int64(len(data))); err != nil {
		return err
	}
	parent, name, close, err := a.parent(ctx, parts)
	if err != nil {
		return secretCorruption(ctx, "secret storage parent directory is unsafe", err)
	}
	defer close()
	if atomic {
		err = a.store.writeExclusiveAtomic(ctx, parent, name, data)
	} else {
		err = a.store.writeExclusive(ctx, parent, name, data)
	}
	if err != nil {
		return secretEffectFailure(ctx, "exclusive secret state could not be published safely", err)
	}
	if err := a.rememberPublishedFile(ctx, parent, name, path, data); err != nil {
		a.phase = secretUncertain
		return err
	}
	return nil
}

func (a *secretArea) rememberPublishedFile(ctx context.Context, parent *directory, name, path string, data []byte) error {
	actual, identity, err := readBoundedIdentity(ctx, parent, name, len(data), true)
	defer clear(actual)
	if err != nil || !bytes.Equal(actual, data) {
		return secretConflict(ctx, "published secret state changed before verification", err)
	}
	a.forgetExpectation(path)
	a.mutable[path] = secretExpectation{exists: true, data: slices.Clone(data), identity: identity, parentIdentity: parent.identity, parentKnown: true}
	a.observeReplacement(path, parent.identity, identity)
	return nil
}

func (a *secretArea) Replace(ctx context.Context, path string, data, expected []byte) (secretstore.Outcome, error) {
	if err := a.available(ctx, true); err != nil {
		return secretstore.NotCommitted, err
	}
	parts, err := secretPath(path, 1, 2)
	if err != nil {
		return secretstore.NotCommitted, err
	}
	expectation, ok := a.mutable[path]
	if !ok || !bytes.Equal(expectation.data, expected) {
		return secretstore.NotCommitted, state("secret storage replacement lacks its exact read expectation")
	}
	if len(data) > maxSecretBytes {
		return secretstore.NotCommitted, secretLimit("secret storage file exceeds its byte limit")
	}
	a.phase = secretPublishing
	if err := a.ensureRoot(ctx); err != nil {
		return secretstore.NotCommitted, err
	}
	if err := a.capacity(ctx, 1, int64(len(data))); err != nil {
		return secretstore.NotCommitted, err
	}
	parent, name, close, err := a.parent(ctx, parts)
	if err != nil {
		return secretstore.NotCommitted, secretCorruption(ctx, "secret storage parent directory is unsafe", err)
	}
	defer close()
	var pending string
	written := false
	for range 16 {
		candidate, err := a.store.candidate("pending-")
		if err != nil {
			return secretstore.NotCommitted, err
		}
		pending = candidate
		err = a.store.writeExclusive(ctx, parent, pending, data)
		if errors.Is(err, syscall.EEXIST) {
			continue
		}
		if err != nil {
			return secretstore.NotCommitted, err
		}
		written = true
		break
	}
	if !written {
		return secretstore.NotCommitted, state("secret storage replacement exhausted its collision limit")
	}
	pendingIdentity, err := verifySecretPending(ctx, parent, pending, data)
	if err != nil {
		return secretstore.NotCommitted, err
	}
	if err := a.store.checkpoint(ctx, "before-secret-rename"); err != nil {
		return secretstore.NotCommitted, err
	}
	if err := a.verifyExpectedContext(ctx); err != nil {
		return secretstore.NotCommitted, err
	}
	if err := verifySecretExpectation(ctx, parent, name, expectation); err != nil {
		return secretstore.NotCommitted, err
	}
	if path == secretstore.RecordPath {
		if err := a.verifyReadDependencies(ctx, path); err != nil {
			return secretstore.NotCommitted, err
		}
	}
	currentPending, err := verifySecretPending(ctx, parent, pending, data)
	if err != nil || !sameFile(pendingIdentity, currentPending) {
		return secretstore.NotCommitted, state("pending secret state changed before publication")
	}
	if err := syscall.Renameat(int(parent.file.Fd()), pending, int(parent.file.Fd()), name); err != nil {
		return secretstore.NotCommitted, state("secret state could not be atomically published")
	}
	a.forgetExpectation(path)
	if path == secretstore.RecordPath {
		a.phase = secretUncertain
	}
	if err := a.store.checkpoint(ctx, "after-secret-rename"); err != nil {
		return secretstore.Uncertain, state("secret state publication has uncertain durability; inspect it before retrying")
	}
	if err := a.store.syncDirectory(ctx, parent); err != nil {
		return secretstore.Uncertain, state("secret state publication has uncertain durability; inspect it before retrying")
	}
	if err := a.rememberPublishedFile(ctx, parent, name, path, data); err != nil {
		a.phase = secretUncertain
		return secretstore.Committed, err
	}
	if path == secretstore.RecordPath {
		a.phase = secretCommitted
	}
	return secretstore.Committed, nil
}

func verifySecretExpectation(ctx context.Context, parent *directory, name string, expected secretExpectation) error {
	if expected.parentKnown && !sameIdentity(parent.identity, expected.parentIdentity) {
		return state("secret storage parent was replaced before publication")
	}
	if !expected.exists {
		file, err := openRelative(parent, name, pathHandle, 0)
		if file != nil {
			file.Close()
		}
		if !errors.Is(err, syscall.ENOENT) {
			return state("secret state appeared before publication")
		}
		return parent.verify()
	}
	actual, identity, err := readBoundedIdentity(ctx, parent, name, len(expected.data), true)
	defer clear(actual)
	if err != nil || !sameFile(expected.identity, identity) || !bytes.Equal(expected.data, actual) {
		return state("secret state was replaced or modified before publication")
	}
	return nil
}

func (a *secretArea) verifyReadDependencies(ctx context.Context, target string) error {
	paths := make([]string, 0, len(a.mutable))
	for path := range a.mutable {
		if path != target {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	for _, path := range paths {
		parts, err := secretPath(path, 1, 2)
		if err != nil {
			return err
		}
		parent, name, close, err := a.parent(ctx, parts)
		if err != nil {
			return secretConflict(ctx, "secret publication dependency parent changed", err)
		}
		err = verifySecretExpectation(ctx, parent, name, a.mutable[path])
		close()
		if err != nil {
			return secretConflict(ctx, "secret publication dependency changed", err)
		}
	}
	return nil
}

func verifySecretPending(ctx context.Context, parent *directory, name string, data []byte) (syscall.Stat_t, error) {
	actual, identity, err := readBoundedIdentity(ctx, parent, name, len(data), true)
	defer clear(actual)
	if err != nil || !bytes.Equal(data, actual) {
		return syscall.Stat_t{}, state("pending secret state changed before publication")
	}
	return identity, nil
}

func (a *secretArea) verifyExpectedContext(ctx context.Context) error {
	if a.expected == nil {
		return state("secret mutation has no context expectation")
	}
	registry, _, err := readRegistry(ctx, a.root)
	if err != nil {
		return err
	}
	actual, err := registryExpectation(ctx, a.root, registry)
	if err != nil || !sameFile(a.expected.identity, actual.identity) || !bytes.Equal(a.expected.data, actual.data) {
		return state("context identity changed before secret publication")
	}
	if _, err := exactSecretRecord(registry, a.token); err != nil {
		return err
	}
	return verifyReservation(ctx, a.context, a.token.Name)
}

func (a *secretArea) Sync(ctx context.Context, path string) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	parts, err := secretPath(path, 0, 1)
	if err != nil {
		return err
	}
	if a.secrets == nil {
		return secretCorrupt("secret storage directory does not exist")
	}
	dir := a.secrets
	if len(parts) == 1 {
		dir, err = openDirectory(a.secrets, parts[0])
		if err != nil {
			return secretCorruption(ctx, "secret storage directory is unsafe", err)
		}
		defer dir.file.Close()
	}
	if err := a.store.syncDirectory(ctx, dir); err != nil {
		return secretEffectFailure(ctx, "secret storage directory could not be synchronized", err)
	}
	return nil
}
