//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

var _ contexts.Repository = (*Store)(nil)

const (
	unsupportedRootStateMessage = "the context store root contains unsupported state"
	unsafeRootRemediation       = "nothing repairs it, because Bootwright never changes the owner or mode of an existing state root; " + earlierBuildGuidance
	pendingRegistryMessage      = "context initialization is incomplete"
	pendingRegistryRemediation  = "retry context init with the original options"
)

// unsafeRoot names what the state root is and what it must be.
func unsafeRoot(stat syscall.Stat_t, uid, gid uint32) error {
	return contexts.StateErrorWithRemediation(
		"the state root is "+fileKind(stat.Mode)+" owned by "+ownerText(stat.Uid, stat.Gid)+" with mode "+modeText(stat.Mode)+
			", but it must be a directory owned by "+ownerText(uid, gid)+" with mode 0700",
		unsafeRootRemediation)
}

func fileKind(mode uint32) string {
	switch mode & syscall.S_IFMT {
	case syscall.S_IFDIR:
		return "a directory"
	case syscall.S_IFREG:
		return "a regular file"
	case syscall.S_IFLNK:
		return "a symbolic link"
	}
	return "a special file"
}

func ownerText(uid, gid uint32) string {
	if uid == 0 && gid == 0 {
		return "root:root"
	}
	return strconv.FormatUint(uint64(uid), 10) + ":" + strconv.FormatUint(uint64(gid), 10)
}

func modeText(mode uint32) string {
	text := strconv.FormatUint(uint64(mode&07777), 8)
	for len(text) < 4 {
		text = "0" + text
	}
	return text
}

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
	var failure *diagnostics.Failure
	if errors.As(err, &failure) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	const refused = "context storage could not be safely accessed"
	errno, found := storeErrno(err)
	switch {
	case !found:
		return state(refused)
	case capacityErrno(errno):
		return storeFailure(refused, errno)
	}
	return state(refused + ": " + errnoText(errno))
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
		held, listErr := rootHoldsEntries(root)
		if listErr != nil {
			return contexts.Registry{}, false, listErr
		}
		if !held {
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
	if err := verifyRootEntries(ctx, root, registry); err != nil {
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

func unsupportedRootState() error {
	return contexts.StateErrorWithRemediation(
		unsupportedRootStateMessage,
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

func uncertainRegistryPublication() error {
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
	names, err := heldNames(root, maximum)
	if err != nil {
		return nil, rootListingError(err)
	}
	return names, nil
}

// rootHoldsEntries answers whether the state root holds any entry, listing it
// through a fresh handle so the held one keeps its directory offset.
func rootHoldsEntries(root *directory) (bool, error) {
	_, err := heldNames(root, 0)
	var failed *listingFailure
	if errors.As(err, &failed) && failed.kind == listingOverLimit {
		return true, nil
	}
	return false, rootListingError(err)
}

func rootListingError(err error) error {
	var failed *listingFailure
	if !errors.As(err, &failed) {
		return err
	}
	switch failed.kind {
	case listingReplaced:
		return state("state root changed during enumeration")
	case listingOverLimit:
		return state("state root entry count exceeds its limit")
	}
	return state("state root cannot be enumerated safely")
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

// verifyRootEntries admits only the store's own published objects: the
// registry, the context container, a declared controller subtree and bounded
// private files left by an interrupted registry replacement. Anything else is
// unattributable state that only a complete restore can resolve.
func verifyRootEntries(ctx context.Context, root *directory, registry contexts.Registry) error {
	names, err := rootEntryNames(root, maxContexts+1)
	if err != nil {
		return unsupportedRootState()
	}
	for _, name := range names {
		switch name {
		case "registry.json":
		case "contexts":
			dir, err := openDirectory(root, name)
			if err != nil {
				return err
			}
			dir.file.Close()
		case "controller":
			if err := verifyControllerRootEntry(root, registry); err != nil {
				return err
			}
		case mediaContainer:
			dir, err := openDirectory(root, name)
			if err != nil {
				return err
			}
			dir.file.Close()
		default:
			if !pendingInitialRegistryName(name) {
				return unsupportedRootState()
			}
			if err := verifyIgnoredRegistryStage(ctx, root, name); err != nil {
				if canceled := ctx.Err(); canceled != nil {
					return canceled
				}
				return unsupportedRootState()
			}
		}
	}
	current, err := rootEntryNames(root, maxContexts+1)
	if err != nil || !slices.Equal(names, current) {
		return unsupportedRootState()
	}
	return root.verify()
}

func verifyControllerRootEntry(root *directory, registry contexts.Registry) error {
	if registry.Controller == (contexts.ControllerDescriptor{}) {
		return state("context store contains an undeclared controller subtree")
	}
	dir, err := openDirectory(root, "controller")
	if err != nil {
		return err
	}
	replaced := registry.Controller.DirectoryInode != 0 && (registry.Controller.DirectoryInode != dir.identity.Ino || registry.Controller.DirectoryDevice != uint64(dir.identity.Dev))
	dir.file.Close()
	if replaced {
		return state("controller directory was replaced")
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
	if err := s.checkpoint(ctx, checkpointSyncInitialRegistryFile); err != nil {
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
	if err := s.checkpoint(ctx, checkpointBeforeInitialRegistryRecovery); err != nil {
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
	if err := s.checkpoint(ctx, checkpointAfterInitialRegistryRecovery); err != nil {
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
	return verifyMappingsExcept(ctx, root, registry, "")
}

// verifyMappingsExcept verifies every ready context's mapping except skip's,
// the one context a scoped deletion admits. Verification stays store-wide, so
// any other damaged context still refuses, named.
func verifyMappingsExcept(ctx context.Context, root *directory, registry contexts.Registry, skip string) error {
	if _, err := readControllerStored(ctx, root, registry); err != nil {
		return err
	}
	sizes := make([]int, len(registry.Contexts))
	total := int64(0)
	for index, record := range registry.Contexts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if record.Mode != contexts.Ready || record.Name == skip {
			continue
		}
		size, err := verifyContextMapping(ctx, root, record)
		if err != nil {
			return err
		}
		sizes[index] = int(size)
		total += size
		if total > maxAllManifests {
			return state("aggregate referenced manifest bytes exceed their limit")
		}
	}
	for index, record := range registry.Contexts {
		if record.Mode != contexts.Ready || record.Revision == "" || record.Name == skip {
			continue
		}
		_, _, close, err := openManifestBounded(ctx, root, record, sizes[index])
		if err != nil {
			entry := "contexts/" + record.Name + "/desired-state/revisions/" + record.Revision + "/manifest.json"
			return contextDamage(ctx, record.Name, entry, err, purgeRemediation(record.Name))
		}
		close()
	}
	return root.verify()
}

// restoreStoreRemediation is the exit of damage a context's deletion cannot
// run over.
const restoreStoreRemediation = "restore the whole store from a matching backup"

const replacedContextRemediation = restoreStoreRemediation + ": context delete refuses a replaced context directory"

const reservationRemediation = restoreStoreRemediation + ": context delete needs the context's reservation to attribute its directory"

const unsafeEntryRemediation = restoreStoreRemediation + ": context delete refuses an entry it cannot open safely"

func purgeRemediation(name string) string {
	return "delete it with bootwright context delete --name " + name + " --purge, or " + restoreStoreRemediation
}

// entryRemediation is the exit of a configuration or revision entry the store
// could not open: the purge removes a missing one, but one it refuses to open,
// for its type, owner, permissions, link count or a kernel error, refuses the
// purge too.
func entryRemediation(name string, err error) string {
	if errors.Is(err, syscall.ENOENT) {
		return purgeRemediation(name)
	}
	return unsafeEntryRemediation
}

func abandonRemediation(name string) string {
	return "abandon it with bootwright context delete --name " + name + " --purge --allow-orphans: its directory is gone, so what it owned cannot be listed; or " + restoreStoreRemediation
}

// evidenceRemediation is the exit of mutation evidence the store refuses to
// open as its own: the deletion never removes an entry it cannot verify, so
// the entry is removed by hand and the context is then abandoned as one whose
// evidence is missing.
func evidenceRemediation(name string) string {
	return "remove contexts/" + name + "/state/mutation.json beneath the state root, which leaves nothing to prove what the context owns, then abandon whatever it owns with bootwright context delete --name " + name + " --purge --allow-orphans; or " + restoreStoreRemediation
}

// contextDamage names a ready context whose mapping failed verification: the
// context, the entry relative to the state root and the kernel's answer or the
// store's own refusal, never the root's absolute path, with that damage's exit.
func contextDamage(ctx context.Context, name, entry string, cause error, remediation string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	message := "context " + name + " cannot be verified: " + entry
	if text := storeCauseText(cause); text != "" {
		message += ": " + text
	}
	return contexts.StateErrorWithRemediation(message, remediation)
}

// reservationDamage names a context whose reservation failed verification;
// any other error passes unchanged.
func reservationDamage(ctx context.Context, name string, err error) error {
	var damaged *reservationFailure
	if !errors.As(err, &damaged) {
		return err
	}
	return contextDamage(ctx, name, "contexts/"+name+"/"+damaged.entry, damaged.cause, reservationRemediation)
}

// verifyContextMapping proves one ready context's directory, reservation,
// configuration and selected revision, and returns the size of the manifest
// that revision references.
func verifyContextMapping(ctx context.Context, root *directory, record contexts.Record) (int64, error) {
	container, err := openDirectory(root, "contexts")
	if err != nil {
		return 0, contextDamage(ctx, record.Name, "contexts", err, restoreStoreRemediation)
	}
	defer container.file.Close()
	entry := "contexts/" + record.Name
	dir, err := openDirectory(container, record.Name)
	switch {
	case errors.Is(err, syscall.ENOENT):
		return 0, contextDamage(ctx, record.Name, entry, err, abandonRemediation(record.Name))
	case err != nil:
		return 0, contextDamage(ctx, record.Name, entry, err, restoreStoreRemediation)
	}
	defer dir.file.Close()
	if record.DirectoryInode != 0 && (uint64(dir.identity.Dev) != record.DirectoryDevice || dir.identity.Ino != record.DirectoryInode) {
		return 0, contextDamage(ctx, record.Name, entry, state("context directory was replaced"), replacedContextRemediation)
	}
	if err := verifyReservation(ctx, dir, record.Name); err != nil {
		return 0, reservationDamage(ctx, record.Name, err)
	}
	config, err := readBounded(ctx, dir, "context.yaml", maxRecord, true)
	if err != nil {
		return 0, contextDamage(ctx, record.Name, entry+"/context.yaml", err, entryRemediation(record.Name, err))
	}
	parsed, err := contexts.ParseConfiguration(record.Name, config)
	if err != nil || !bytes.Equal(config, parsed.Canonical()) || parsed.SecretStore.Type != record.SecretStoreType {
		return 0, contextDamage(ctx, record.Name, entry+"/context.yaml", state("persisted context configuration is inconsistent"), purgeRemediation(record.Name))
	}
	if record.Revision == "" {
		return 0, nil
	}
	return verifyRevisionMapping(ctx, dir, record)
}

func verifyRevisionMapping(ctx context.Context, dir *directory, record contexts.Record) (int64, error) {
	entry := "contexts/" + record.Name
	owned := []*directory{}
	defer func() {
		for i := len(owned) - 1; i >= 0; i-- {
			owned[i].file.Close()
		}
	}()
	parent := dir
	for _, name := range []string{"desired-state", "revisions", record.Revision} {
		entry += "/" + name
		child, err := openDirectory(parent, name)
		if err != nil {
			return 0, contextDamage(ctx, record.Name, entry, err, entryRemediation(record.Name, err))
		}
		owned = append(owned, child)
		parent = child
	}
	entry += "/manifest.json"
	file, err := openRelative(parent, "manifest.json", pathHandle, 0)
	if err != nil {
		return 0, contextDamage(ctx, record.Name, entry, err, entryRemediation(record.Name, err))
	}
	stat, err := statHandle(file)
	file.Close()
	if err != nil || !private(stat, syscall.S_IFREG, parent.identity.Uid, parent.identity.Gid) {
		return 0, contextDamage(ctx, record.Name, entry, state("manifest handle is unsafe"), unsafeEntryRemediation)
	}
	if stat.Size < 0 || stat.Size > maxManifest {
		return 0, contextDamage(ctx, record.Name, entry, state("manifest exceeds its byte limit"), purgeRemediation(record.Name))
	}
	return stat.Size, nil
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

// reservationFailure is a context reservation that failed verification. Its
// diagnostic is the one every caller reports; a refusal naming the context
// reads the entry, relative to the context directory, and the cause.
type reservationFailure struct {
	failure error
	entry   string
	cause   error
}

func (e *reservationFailure) Error() string { return e.failure.Error() }

func (e *reservationFailure) Unwrap() error { return e.failure }

func verifyReservation(ctx context.Context, dir *directory, name string) error {
	runtime, err := openDirectory(dir, "state")
	if err != nil {
		return &reservationFailure{state("context reservation directory is missing or unsafe"), "state", err}
	}
	defer runtime.file.Close()
	data, err := readBounded(ctx, runtime, "reservation.json", maxRecord, true)
	if err != nil {
		return &reservationFailure{state("context reservation is missing or unsafe"), "state/reservation.json", err}
	}
	var record reservation
	if err := decodeRecord(data, maxRecord, &record); err != nil {
		return &reservationFailure{err, "state/reservation.json", err}
	}
	if err := validateReservation(record, name); err != nil {
		return &reservationFailure{err, "state/reservation.json", err}
	}
	return nil
}

func readMutation(ctx context.Context, dir *directory) ([]byte, error) {
	return readEvidence(ctx, dir, false)
}

// readReadyMutation reads a ready context's mutation evidence. Evidence absent
// from its present directory reads as empty, which no guard reads, so the
// context refuses and is abandoned as one with corrupt evidence is, not as
// storage that cannot be accessed. Initialization and publication still
// require the file.
func readReadyMutation(ctx context.Context, dir *directory) ([]byte, error) {
	return readEvidence(ctx, dir, true)
}

func readEvidence(ctx context.Context, dir *directory, absentReadsEmpty bool) ([]byte, error) {
	runtime, err := openDirectory(dir, "state")
	if err != nil {
		return nil, err
	}
	defer runtime.file.Close()
	data, err := readBounded(ctx, runtime, "mutation.json", maxRecord, true)
	if absentReadsEmpty && errors.Is(err, syscall.ENOENT) {
		return []byte{}, nil
	}
	if err != nil && !errors.Is(err, syscall.ENOENT) && ctx.Err() == nil {
		return nil, contextDamage(ctx, dir.name, storeEntry(runtime, "mutation.json"), err, evidenceRemediation(dir.name))
	}
	return data, err
}

func (s *Store) ReadInputs(ctx context.Context, name string) (desiredstate.Sources, error) {
	if !contextName(name) {
		return desiredstate.Sources{}, state("no valid current context is selected")
	}
	root, err := s.openRoot(ctx, false, nil)
	if errors.Is(err, syscall.ENOENT) {
		return desiredstate.Sources{}, contexts.AbsentContext(name)
	}
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
	for _, record := range registry.Contexts {
		if record.Name == name {
			if record.Mode != contexts.Ready {
				return desiredstate.Sources{}, contexts.NotReady(record)
			}
			if record.Revision == "" {
				return desiredstate.Sources{}, contexts.MissingInput(record.Name)
			}
			result, err := readSnapshot(ctx, root, record)
			return result, safeError(err)
		}
	}
	return desiredstate.Sources{}, contexts.AbsentContext(name)
}

// emptyStore is the refusal of a transaction over a store that holds no
// context yet, which a caller asked for one name tells from every other
// refusal by contexts.ErrNoContexts.
func emptyStore() error { return &noContexts{contexts.NoContexts()} }

type noContexts struct{ failure error }

func (e *noContexts) Error() string { return e.failure.Error() }

func (e *noContexts) Unwrap() error { return e.failure }

func (e *noContexts) Is(target error) bool { return target == contexts.ErrNoContexts }

func (s *Store) Transact(ctx context.Context, create bool, inputs []string, callback func(contexts.Transaction) error) error {
	if callback == nil {
		return s.transact(ctx, create, inputs, "", nil)
	}
	return s.transact(ctx, create, inputs, "", func(tx *transaction) error { return callback(tx) })
}

// TransactDeletion is the registry transaction of one context's deletion. It
// takes the exclusive root lock and verifies and collects exactly as Transact
// does, except that it leaves the named context's own mapping to that
// deletion, which may find it damaged. Its transaction serves only the
// deletion of that context.
func (s *Store) TransactDeletion(ctx context.Context, name string, callback func(contexts.Transaction) error) error {
	if !contextName(name) {
		if err := ctx.Err(); err != nil {
			return err
		}
		return state("context name is invalid")
	}
	if callback == nil {
		return s.transact(ctx, false, nil, name, nil)
	}
	return s.transact(ctx, false, nil, name, func(tx *transaction) error {
		return callback(&deletionTransaction{tx: tx, name: name})
	})
}

// transact holds the exclusive root lock across one registry transaction.
// scope names the one context a deletion transaction admits unverified.
func (s *Store) transact(ctx context.Context, create bool, inputs []string, scope string, callback func(*transaction) error) error {
	root, err := s.openRoot(ctx, create, inputs)
	if !create && errors.Is(err, syscall.ENOENT) {
		return emptyStore()
	}
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
			return emptyStore()
		}
		if err := s.publishInitialRegistry(ctx, root, registry); err != nil {
			return safeError(err)
		}
	}
	if err := verifyMappingsExcept(ctx, root, registry, scope); err != nil {
		return safeError(err)
	}
	if err := s.collectControllerStages(ctx, root, registry); err != nil {
		return safeError(err)
	}
	if err := s.collectRegistryStages(ctx, root); err != nil {
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
	store              *Store
	root               *directory
	container          *directory
	registry           contexts.Registry
	leases             map[string]*directory
	evidence           map[string][]byte
	lost               map[string]bool
	committed          bool
	closed             bool
	expected           *expectedRegistry
	controllerInputs   map[string]string
	controllerEvidence *controllerStored
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

func (t *transaction) record(name string) (contexts.Record, error) {
	for _, record := range t.registry.Contexts {
		if record.Name == name {
			return record, nil
		}
	}
	return contexts.Record{}, state("context name has no registry reservation")
}

func (t *transaction) contextDirectory(ctx context.Context, name string) (*directory, error) {
	if !contextName(name) {
		return nil, state("context name is invalid")
	}
	record, err := t.record(name)
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
	if err := t.store.replaceRegistry(ctx, t.root, registry, t.expected); err != nil {
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

func (t *transaction) MutationState(ctx context.Context, name string) ([]byte, error) {
	if err := t.available(ctx); err != nil {
		return nil, err
	}
	if err := t.checkControllerRecovery(ctx, name); err != nil {
		return nil, err
	}
	if prior, ok := t.evidence[name]; ok {
		return slices.Clone(prior), nil
	}
	dir, err := t.leaseContext(ctx, name)
	if err != nil {
		return nil, err
	}
	read := readMutation
	if record, err := t.record(name); err == nil && record.Mode == contexts.Ready {
		read = readReadyMutation
	}
	data, err := read(ctx, dir)
	if err != nil {
		return nil, err
	}
	t.evidence[name] = slices.Clone(data)
	return data, nil
}

func (t *transaction) leaseContext(ctx context.Context, name string) (*directory, error) {
	if err := t.available(ctx); err != nil {
		return nil, err
	}
	if dir := t.leases[name]; dir != nil {
		return dir, dir.verify()
	}
	dir, err := t.contextDirectory(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := lock(dir); err != nil {
		dir.file.Close()
		return nil, err
	}
	if err := t.admitLease(ctx, dir, name); err != nil {
		syscall.Flock(int(dir.file.Fd()), syscall.LOCK_UN)
		dir.file.Close()
		return nil, err
	}
	t.leases[name] = dir
	return dir, nil
}

// admitLease collects a locked context directory's stages and verifies its
// layout and reservation; only a directory that passes all three is held.
func (t *transaction) admitLease(ctx context.Context, dir *directory, name string) error {
	// A directory the registry does not yet attribute may not be this
	// context's own, so nothing in it is collected.
	record, err := t.record(name)
	if err != nil {
		return err
	}
	if record.DirectoryInode != 0 {
		if err := t.store.collectContextStages(ctx, dir, true); err != nil {
			return err
		}
	}
	if err := verifyContextLayout(ctx, dir); err != nil {
		return err
	}
	return verifyReservation(ctx, dir, name)
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

// publishInitialRegistry publishes the first registry into a root that holds
// nothing else. Its complete stage is the recovery artifact init resumes from,
// so every failure after the write keeps it; only a failed write removes what
// it created.
func (s *Store) publishInitialRegistry(ctx context.Context, root *directory, registry contexts.Registry) error {
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
	if err := s.checkpoint(ctx, checkpointBeforeRegistryRename); err != nil {
		return err
	}
	if err := root.verify(); err != nil {
		return err
	}
	file, err := openRelative(root, "registry.json", pathHandle, 0)
	if file != nil {
		file.Close()
	}
	if !errors.Is(err, syscall.ENOENT) {
		return initialRegistryRecoveryError("initial registry destination unexpectedly exists")
	}
	currentPending, err := verifyPending(ctx, root, name, data)
	if err != nil {
		return err
	}
	if !sameFile(pendingIdentity, currentPending) {
		return initialRegistryRecoveryError("pending initial registry changed before publication")
	}
	entry, sole, err := soleRootEntry(root)
	if err != nil {
		return initialRegistryRecoveryError("state root cannot be verified before initial registry publication")
	}
	if !sole || entry != name {
		return initialRegistryRecoveryError("state root changed before initial registry publication")
	}
	if err := renameNoReplaceAt(root, name, "registry.json"); err != nil {
		return initialRegistryRecoveryError("initial registry could not be atomically published")
	}
	publishedIdentity, err := verifyPending(ctx, root, "registry.json", data)
	if err != nil || !sameIdentity(currentPending, publishedIdentity) {
		return uncertainInitialRegistryRecovery()
	}
	if err := s.checkpoint(ctx, checkpointAfterRegistryRename); err != nil {
		return uncertainInitialRegistryRecovery()
	}
	entry, sole, err = soleRootEntry(root)
	if err != nil || !sole || entry != "registry.json" {
		return uncertainInitialRegistryRecovery()
	}
	if err := s.syncDirectory(ctx, root); err != nil {
		return uncertainInitialRegistryRecovery()
	}
	entry, sole, err = soleRootEntry(root)
	if err != nil || !sole || entry != "registry.json" {
		return uncertainInitialRegistryRecovery()
	}
	finalIdentity, err := verifyPending(ctx, root, "registry.json", data)
	if err != nil || !sameFile(publishedIdentity, finalIdentity) {
		return uncertainInitialRegistryRecovery()
	}
	return nil
}

// replaceRegistry publishes a transaction's registry over the one it read,
// proving right before the rename that the root still holds exactly those
// bytes at that identity. A failure before the rename removes the stage; any
// failure after it leaves the publication uncertain.
func (s *Store) replaceRegistry(ctx context.Context, root *directory, registry contexts.Registry, expected *expectedRegistry) error {
	data, err := encodeRecord(registry, maxRegistry)
	if err != nil {
		return err
	}
	outcome, err := s.publishStage(ctx, root, "registry.json", data, stagedPublication{
		subject: "registry", suffix: ".json", replace: true, immutable: true, bound: maxRegistry,
		renameFailure: "registry could not be atomically published",
		prove: func(ctx context.Context) error {
			if err := root.verify(); err != nil {
				return err
			}
			current, _, err := readRegistry(ctx, root)
			if err != nil {
				return err
			}
			actual, err := registryExpectation(ctx, root, current)
			if err != nil {
				return err
			}
			if expected == nil || !sameFile(expected.identity, actual.identity) || !bytes.Equal(expected.data, actual.data) {
				return state("registry was replaced or modified during the transaction")
			}
			return nil
		},
	}, checkpointBeforeRegistryRename, checkpointAfterRegistryRename)
	if outcome == publicationUnknown {
		return uncertainRegistryPublication()
	}
	return err
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
	if registry.Version != t.registry.Version || registry.Controller != t.registry.Controller || len(registry.Contexts) != len(t.registry.Contexts) {
		return state("context commit cannot change reserved identities")
	}
	for _, prior := range t.registry.Contexts {
		index := slices.IndexFunc(registry.Contexts, func(next contexts.Record) bool { return next.Name == prior.Name })
		if index < 0 {
			return state("context commit cannot remove identities")
		}
		next := registry.Contexts[index]
		if next == prior {
			continue
		}
		if err := t.checkControllerRecovery(ctx, prior.Name); err != nil {
			return err
		}
		if next.Revision != prior.Revision {
			if err := t.checkControllerPublication(ctx, prior.Name); err != nil {
				return err
			}
		}
		if next.DirectoryDevice != prior.DirectoryDevice || next.DirectoryInode != prior.DirectoryInode || next.SecretStoreType != prior.SecretStoreType {
			return state("context directory identity or secret implementation cannot change")
		}
		if prior.Revision != "" && next.Revision == "" {
			return state("context replacement cannot discard its selected input")
		}
		if prior.Mode == contexts.Deleting || next.Mode != contexts.Ready {
			return state("context status transition is invalid")
		}
		if next != prior {
			if prior.Mode == contexts.Initializing {
				if err := t.store.checkpoint(ctx, checkpointBeforeContextReady); err != nil {
					return err
				}
			}
			dir, held := t.leases[next.Name]
			if !held {
				return state("context publication requires its mutation lease")
			}
			if err := dir.verify(); err != nil {
				return err
			}
			current, err := readMutation(ctx, dir)
			if err != nil || !bytes.Equal(current, t.evidence[next.Name]) {
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
	for name, dir := range t.leases {
		if err := verifyContextLayout(ctx, dir); err != nil {
			return err
		}
		current, err := readMutation(ctx, dir)
		if err != nil || !bytes.Equal(current, t.evidence[name]) {
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
		if err := t.collectRevisions(ctx, record.Name); err != nil {
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
