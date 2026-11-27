//go:build linux && amd64

package contextfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"slices"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactcatalog"
	"github.com/crmarques/bootwright/internal/workspace/contextfs/kernel"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const (
	artifactContainer = "artifacts"
	maxArtifactRecord = 6*4096 + 1024
	maxArtifactStages = 16
)

func (s *Store) withArtifacts(ctx context.Context, write bool, callback func(*directory, controllerStored) error) error {
	lock := syscall.LOCK_SH
	if write {
		lock = syscall.LOCK_EX
	}
	scope := readScope{absent: artifactcatalog.Failure("this host has no artifact catalog", "run bootwright install first"), registered: true, unverified: true}
	return s.withRead(ctx, lock, scope, func(root *directory, registry contexts.Registry) error {
		stored, err := readControllerStored(ctx, root, registry)
		if err != nil {
			return safeError(err)
		}
		dir, err := kernel.OpenDirectory(root, artifactContainer)
		if errors.Is(err, syscall.ENOENT) && write {
			dir, err = s.newDirectory(ctx, root, artifactContainer)
		}
		if errors.Is(err, syscall.ENOENT) {
			return callback(nil, stored)
		}
		if err != nil {
			return safeError(err)
		}
		defer dir.Close()
		return safeError(callback(dir, stored))
	})
}

func readArtifactRecord(ctx context.Context, dir *directory, name string) (artifactcatalog.Entry, error) {
	var entry artifactcatalog.Entry
	data, err := readBounded(ctx, dir, "record.json", maxArtifactRecord, true)
	if err != nil {
		return entry, err
	}
	if canonicaljson.Decode(data, &entry, canonicaljson.Bare) != nil || !entry.Valid() || entry.Name != name {
		return artifactcatalog.Entry{}, artifactcatalog.Failure("catalog artifact "+name+" has an invalid record", "restore the catalog entry from a trusted backup")
	}
	return entry, nil
}

func readArtifact(ctx context.Context, dir *directory, name string, payload bool) (artifactcatalog.Entry, string, error) {
	var entry artifactcatalog.Entry
	missing := artifactcatalog.Failure("catalog artifact "+name+" is missing", artifactcatalog.Supply(name))
	if dir == nil {
		return entry, "", missing
	}
	child, err := kernel.OpenDirectory(dir, name)
	if errors.Is(err, syscall.ENOENT) {
		return entry, "", missing
	}
	if err != nil {
		return entry, "", err
	}
	defer child.Close()
	entry, err = readArtifactRecord(ctx, child, name)
	if err != nil || entry.OriginURL != "" || !payload {
		return entry, "", err
	}
	file, err := kernel.OpenRelative(child, "payload", kernel.PathHandle, 0)
	if err != nil {
		return entry, "", err
	}
	defer file.Close()
	info, err := kernel.StatHandle(file)
	if err != nil || !kernel.ReadableFile(info, true, child) || info.Size != entry.Size {
		return entry, "", artifactcatalog.Failure("catalog artifact "+name+" has an unsafe or incomplete local payload", "restore the catalog entry from a trusted backup")
	}
	return entry, file.Name(), nil
}

func (s *Store) ResolveArtifact(ctx context.Context, name string) (entry artifactcatalog.Entry, path string, err error) {
	if !artifactcatalog.ValidName(name) {
		return entry, "", artifactcatalog.Failure("the catalog name is invalid", "use a valid Bootwright object name")
	}
	err = s.withArtifacts(ctx, false, func(dir *directory, _ controllerStored) error {
		var failure error
		entry, path, failure = readArtifact(ctx, dir, name, true)
		return failure
	})
	return
}

func artifactHolders(stored controllerStored, name string) []string {
	holders := []string{}
	for _, claim := range stored.value.Reservations {
		for _, key := range claim.Keys {
			subject, ok := infrastructureservices.UnqualifiedSubject(key, artifactcatalog.ClaimClass)
			if artifact, _, found := strings.Cut(subject, ":"); ok && found && artifact == name {
				holders = append(holders, claim.Context)
				break
			}
		}
	}
	slices.Sort(holders)
	return slices.Compact(holders)
}

func (s *Store) ListArtifacts(ctx context.Context) ([]artifactcatalog.Row, error) {
	rows := []artifactcatalog.Row{}
	err := s.withArtifacts(ctx, false, func(dir *directory, stored controllerStored) error {
		if dir == nil {
			return nil
		}
		names, err := kernel.DirectoryNames(dir, artifactcatalog.MaxEntries+maxArtifactStages)
		if err != nil {
			return err
		}
		slices.Sort(names)
		for _, name := range names {
			if !artifactcatalog.ValidName(name) {
				continue
			}
			entry, _, err := readArtifact(ctx, dir, name, false)
			if err != nil {
				return err
			}
			source := "local file"
			if entry.OriginURL != "" {
				source = "deferred HTTPS"
			}
			rows = append(rows, artifactcatalog.Row{Entry: entry, Source: source, Holders: artifactHolders(stored, name)})
		}
		return nil
	})
	return rows, err
}

func uninstallArtifacts(root *directory) ([]string, error) {
	dir, err := kernel.OpenDirectory(root, artifactContainer)
	if errors.Is(err, syscall.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	names, err := kernel.DirectoryNames(dir, artifactcatalog.MaxEntries+maxArtifactStages)
	if err != nil {
		return nil, err
	}
	entries := []string{}
	for _, name := range names {
		if artifactcatalog.ValidName(name) {
			entries = append(entries, name)
		} else if rest, stage := strings.CutPrefix(name, ".stage-"); !stage || !artifactcatalog.ValidName(rest) {
			return nil, state("the artifact catalog contains an unattributed entry")
		}
	}
	slices.Sort(entries)
	return entries, nil
}

func cleanArtifactStage(ctx context.Context, dir *directory, name string) error {
	stage, err := kernel.OpenDirectory(dir, name)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer stage.Close()
	if err := stage.Verify(); err != nil {
		return err
	}
	for _, file := range []string{"payload", "record.json"} {
		if errno := unlinkAt(stage, file, false); errno != 0 && errno != syscall.ENOENT {
			return errno
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if errno := unlinkAt(dir, name, true); errno != 0 {
		return errno
	}
	return nil
}

func (s *Store) AddArtifact(ctx context.Context, entry artifactcatalog.Entry, source io.Reader) (artifactcatalog.Entry, bool, error) {
	if !artifactcatalog.ValidName(entry.Name) || (entry.OriginURL == "") != (source != nil) ||
		source == nil && !entry.Valid() || entry.SHA256 != "" && !artifactcatalog.ValidDigest(entry.SHA256) {
		return entry, false, artifactcatalog.Failure("the artifact source or pin is invalid", "supply a local file or an HTTPS origin with its SHA-256 and byte size")
	}
	stage, err := s.claimArtifactStage(ctx, entry.Name)
	if err != nil {
		return entry, false, err
	}
	defer stage.Close()
	result, created, err := s.acquireArtifact(ctx, stage, entry, source)
	if err != nil || !created {
		cleanErr := s.discardArtifactStage(context.WithoutCancel(ctx), stage)
		if err == nil {
			err = cleanErr
		}
	}
	return result, created, err
}

func (s *Store) claimArtifactStage(ctx context.Context, artifact string) (stage *directory, failure error) {
	name := ".stage-" + artifact
	failure = s.withArtifacts(ctx, true, func(dir *directory, _ controllerStored) error {
		if err := prepareArtifactStage(ctx, dir, artifact); err != nil {
			return err
		}
		created, err := s.newDirectory(ctx, dir, name)
		if err != nil {
			return err
		}
		if err := syscall.Flock(int(created.File.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			created.Close()
			return err
		}
		stage = created
		return nil
	})
	return stage, failure
}

func prepareArtifactStage(ctx context.Context, dir *directory, artifact string) error {
	names, err := kernel.DirectoryNames(dir, artifactcatalog.MaxEntries+maxArtifactStages)
	if err != nil {
		return err
	}
	registered, live := 0, 0
	for _, name := range names {
		if artifactcatalog.ValidName(name) {
			registered++
			continue
		}
		if rest, ok := strings.CutPrefix(name, ".stage-"); ok && artifactcatalog.ValidName(rest) {
			abandoned, err := cleanUnheldArtifactStage(ctx, dir, name)
			if err != nil {
				return err
			}
			if !abandoned && rest == artifact {
				return artifactcatalog.Failure("another command is staging artifact "+artifact, "wait for the import to finish and retry")
			}
			if !abandoned {
				live++
			}
		}
	}
	if live >= maxArtifactStages || !slices.Contains(names, artifact) && registered+live >= artifactcatalog.MaxEntries {
		return artifactcatalog.Failure("the artifact catalog has no free slot for this import", "allow any running imports to finish, or delete an unused catalog entry")
	}
	return nil
}

func cleanUnheldArtifactStage(ctx context.Context, dir *directory, name string) (bool, error) {
	stage, err := kernel.OpenDirectory(dir, name)
	if errors.Is(err, syscall.ENOENT) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer stage.Close()
	err = syscall.Flock(int(stage.File.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, cleanArtifactStage(ctx, dir, name)
}

func (s *Store) discardArtifactStage(ctx context.Context, stage *directory) error {
	return s.withArtifacts(ctx, true, func(dir *directory, _ controllerStored) error {
		if err := s.verifyArtifactStage(dir, stage); errors.Is(err, syscall.ENOENT) {
			return nil
		} else if err != nil {
			return err
		}
		return cleanArtifactStage(ctx, dir, stage.Name)
	})
}

func (s *Store) acquireArtifact(ctx context.Context, stage *directory, entry artifactcatalog.Entry, source io.Reader) (artifactcatalog.Entry, bool, error) {
	if source != nil {
		var err error
		entry, err = copyArtifact(ctx, stage, entry, source)
		if err != nil {
			return entry, false, err
		}
	}
	created := false
	err := s.withArtifacts(ctx, true, func(dir *directory, _ controllerStored) error {
		if err := s.verifyArtifactStage(dir, stage); err != nil {
			return err
		}
		reopened, err := kernel.OpenDirectory(dir, stage.Name)
		if err != nil {
			return err
		}
		defer reopened.Close()
		created, err = s.publishArtifact(ctx, dir, reopened, entry)
		return err
	})
	return entry, created, err
}

func (s *Store) verifyArtifactStage(dir, stage *directory) error {
	current, err := listedEntry(dir, stage.Name)
	if err != nil {
		return err
	}
	if !kernel.SameIdentity(stage.Identity, current) {
		return state("the artifact stage changed during acquisition")
	}
	return nil
}

func (s *Store) publishArtifact(ctx context.Context, dir, stage *directory, entry artifactcatalog.Entry) (bool, error) {
	if _, err := listedEntry(dir, entry.Name); !errors.Is(err, syscall.ENOENT) {
		if err != nil {
			return false, err
		}
		existing, _, err := readArtifact(ctx, dir, entry.Name, true)
		if err != nil {
			return false, err
		}
		if existing != entry {
			return false, artifactcatalog.Failure("catalog artifact "+entry.Name+" already names SHA-256 "+existing.SHA256+" with its own source", "choose a new catalog name")
		}
		return false, s.syncDirectory(ctx, dir)
	}
	data, err := canonicaljson.Encode(entry, canonicaljson.Bare)
	if err != nil {
		return false, err
	}
	if err := s.writeExclusive(ctx, stage, "record.json", data); err != nil {
		return false, err
	}
	if err := s.syncDirectory(ctx, stage); err != nil {
		return false, err
	}
	if err := renameNoReplaceAt(dir, stage.Name, entry.Name); err != nil {
		return false, err
	}
	return true, s.syncDirectory(ctx, dir)
}

type artifactReader struct {
	ctx context.Context
	src io.Reader
}

func (r artifactReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.src.Read(b)
}

func copyArtifact(ctx context.Context, stage *directory, entry artifactcatalog.Entry, source io.Reader) (artifactcatalog.Entry, error) {
	file, err := kernel.OpenRelative(stage, "payload", syscall.O_CREAT|syscall.O_EXCL|syscall.O_WRONLY, 0600)
	if err != nil {
		return entry, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(artifactReader{ctx, source}, artifactcatalog.MaxBytes+1))
	if err != nil {
		return entry, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if size == 0 || size > artifactcatalog.MaxBytes || entry.Size != 0 && entry.Size != size ||
		entry.SHA256 != "" && entry.SHA256 != digest {
		return entry, artifactcatalog.Failure("the local file is empty, exceeds the size limit, or differs from its supplied pin", "check the file, SHA-256 and byte count")
	}
	entry.SHA256, entry.Size = digest, size
	return entry, file.Sync()
}

func (s *Store) DeleteArtifact(ctx context.Context, expected artifactcatalog.Entry) error {
	if !expected.Valid() {
		return artifactcatalog.Failure("the artifact registration is invalid", "inspect it with bootwright catalog artifact show")
	}
	return s.withArtifacts(ctx, true, func(dir *directory, stored controllerStored) error {
		entry, _, err := readArtifact(ctx, dir, expected.Name, false)
		if err != nil {
			return err
		}
		if entry != expected {
			return artifactcatalog.Failure("catalog artifact "+entry.Name+" changed during confirmation", "review the entry again")
		}
		if holders := artifactHolders(stored, entry.Name); len(holders) != 0 {
			return artifactcatalog.Failure("catalog artifact "+entry.Name+" is reserved by "+strings.Join(holders, ", "), "complete destroy in the reserving contexts")
		}
		stage := ".stage-" + entry.Name
		idle, err := cleanUnheldArtifactStage(ctx, dir, stage)
		if err != nil {
			return err
		}
		if !idle {
			return artifactcatalog.Failure("another command is staging artifact "+entry.Name, "wait for the import to finish and retry")
		}
		if err := renameNoReplaceAt(dir, entry.Name, stage); err != nil {
			return err
		}
		if err := s.syncDirectory(ctx, dir); err != nil {
			return err
		}
		if err := cleanArtifactStage(ctx, dir, stage); err != nil {
			return err
		}
		return s.syncDirectory(ctx, dir)
	})
}

func (t *lifecycleTransaction) proveArtifactClaims(ctx context.Context, claims []infrastructureservices.HostReservation) error {
	for _, claim := range claims {
		for _, key := range claim.Keys {
			subject, isArtifact := infrastructureservices.UnqualifiedSubject(key, artifactcatalog.ClaimClass)
			if !isArtifact {
				continue
			}
			name, pin, _ := strings.Cut(subject, ":")
			if err := t.proveArtifactClaim(ctx, name, pin); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *lifecycleTransaction) proveArtifactClaim(ctx context.Context, name, pin string) error {
	changed := artifactcatalog.Failure("catalog artifact "+name+" is missing or changed since planning", artifactcatalog.Supply(name))
	if !artifactcatalog.ValidName(name) {
		return changed
	}
	dir, err := kernel.OpenDirectory(t.base.root, artifactContainer)
	if errors.Is(err, syscall.ENOENT) {
		return changed
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	entry, _, err := readArtifact(ctx, dir, name, true)
	if err != nil {
		return err
	}
	if entry.Identity() != pin {
		return changed
	}
	return nil
}
