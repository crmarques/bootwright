//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"syscall"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func prepareManifest(name, environment string, sources desiredstate.Sources) (manifest, map[string]desiredstate.SourceFile, error) {
	if len(sources.Roots) != 1 || !canonicalPath(sources.Roots[0]) || len(sources.Files) > desiredstate.MaxFiles || len(sources.Markers) > desiredstate.MaxMarkers {
		return manifest{}, nil, state("frozen input requires one bounded original directory")
	}
	m := manifest{Version: ManifestVersion, Context: name, InputDirectory: sources.Roots[0], EnvironmentDirectory: environment, Files: []frozenFile{}}
	files := make(map[string]desiredstate.SourceFile, len(sources.Files)+len(sources.Markers))
	minimumMetadataBytes := 0
	for index, collection := range [][]desiredstate.SourceFile{sources.Files, sources.Markers} {
		category := "yaml"
		if index == 1 {
			category = "marker"
		}
		for _, file := range collection {
			if !canonicalPath(file.Path()) || !beneath(m.InputDirectory, file.Path()) {
				return manifest{}, nil, state("frozen file path is outside its input directory")
			}
			relative, err := filepath.Rel(m.InputDirectory, file.Path())
			if err != nil {
				return manifest{}, nil, state("frozen file path cannot be represented")
			}
			minimumMetadataBytes += len(relative) + 64
			if minimumMetadataBytes > maxManifest {
				return manifest{}, nil, state("input manifest exceeds its encoding limit")
			}
			if _, duplicate := files[relative]; duplicate {
				return manifest{}, nil, state("frozen file is duplicated")
			}
			files[relative] = file
			m.Files = append(m.Files, frozenFile{Path: relative, Category: category, Size: file.Size()})
		}
	}
	slices.SortFunc(m.Files, func(a, b frozenFile) int {
		if a.Path < b.Path {
			return -1
		}
		if a.Path > b.Path {
			return 1
		}
		return 0
	})
	// Validate all lengths before copying any source bytes or creating a revision.
	m.Revision = "rev-00000000000000000000000000000000"
	for i := range m.Files {
		m.Files[i].SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	}
	if err := validateManifest(m, name, m.Revision, environment); err != nil {
		return manifest{}, nil, err
	}
	if !fitsJSON(reflect.ValueOf(m), maxManifest-1) {
		return manifest{}, nil, state("input manifest exceeds its encoding limit")
	}
	for i, file := range m.Files {
		data := files[file.Path].Bytes()
		m.Files[i].SHA256 = digest(data)
	}
	return m, files, nil
}

func (t *transaction) Publish(ctx context.Context, name, environment string, sources desiredstate.Sources) (string, error) {
	if err := t.available(ctx); err != nil {
		return "", err
	}
	if err := t.checkControllerRecovery(ctx, name); err != nil {
		return "", err
	}
	if err := t.checkControllerPublication(ctx, name); err != nil {
		return "", err
	}
	dir, held := t.leases[name]
	if !held {
		return "", state("input publication requires the context mutation lease")
	}
	if err := verifyReservation(ctx, dir, name); err != nil {
		return "", err
	}
	m, files, err := prepareManifest(name, environment, sources)
	if err != nil {
		return "", err
	}
	if beneath(m.InputDirectory, t.root.path) || beneath(environment, t.root.path) {
		return "", state("state root overlaps frozen input")
	}
	input, err := t.store.ensureDirectory(ctx, dir, "desired-state")
	if err != nil {
		return "", safeError(err)
	}
	defer input.file.Close()
	revisions, err := t.store.ensureDirectory(ctx, input, "revisions")
	if err != nil {
		return "", safeError(err)
	}
	defer revisions.file.Close()
	names, err := revisionEntries(revisions, "rev-", "")
	if err != nil {
		return "", err
	}
	if len(names) == maxRevisions {
		// A legacy store or interrupted writer may have filled the revision
		// bound. Establish its existing selection durably before reclaiming
		// unselected input; a failed sync must preserve every revision.
		if err := t.syncIntent(ctx); err != nil {
			return "", err
		}
		if err := t.collectRevisions(ctx, name); err != nil {
			return "", err
		}
		names, err = revisionEntries(revisions, "rev-", "")
		if err != nil {
			return "", err
		}
		if len(names) == maxRevisions {
			return "", state("retained revision count exceeds its limit")
		}
	}
	for range 16 {
		revision, err := t.store.candidate("rev-")
		if err != nil {
			return "", err
		}
		fresh, err := t.store.newDirectory(ctx, revisions, revision)
		if errors.Is(err, syscall.EEXIST) {
			continue
		}
		if err != nil {
			return "", safeError(err)
		}
		m.Revision = revision
		for index, file := range m.Files {
			if err = t.store.writeExclusive(ctx, fresh, blobName(index), files[file.Path].Bytes()); err != nil {
				break
			}
		}
		if err == nil {
			var data []byte
			data, err = encodeRecord(m, maxManifest)
			if err == nil {
				err = t.store.writeExclusive(ctx, fresh, "manifest.json", data)
			}
		}
		if err == nil {
			err = t.store.syncDirectory(ctx, fresh)
		}
		fresh.file.Close()
		if err != nil {
			return "", safeError(err)
		}
		if err := t.store.syncDirectory(ctx, revisions); err != nil {
			return "", err
		}
		return revision, nil
	}
	return "", state("input revision reservation exhausted its collision limit")
}

// openManifest returns a complete, verified immutable manifest and owned
// handles; close must be called even when later validation fails.
func openManifest(ctx context.Context, root *directory, record contexts.Record) (manifest, *directory, func(), error) {
	return openManifestBounded(ctx, root, record, maxManifest)
}

func openManifestBounded(ctx context.Context, root *directory, record contexts.Record, maximum int) (manifest, *directory, func(), error) {
	owned := []*directory{}
	close := func() {
		for i := len(owned) - 1; i >= 0; i-- {
			owned[i].file.Close()
		}
	}
	parent := root
	for _, name := range []string{"contexts", record.Name, "desired-state", "revisions", record.Revision} {
		next, err := openDirectory(parent, name)
		if err != nil {
			close()
			return manifest{}, nil, func() {}, state("referenced input revision is missing or unsafe")
		}
		owned = append(owned, next)
		parent = next
	}
	data, err := readBounded(ctx, parent, "manifest.json", min(maximum, maxManifest), true)
	if err != nil {
		close()
		return manifest{}, nil, func() {}, state("referenced input manifest is missing or unsafe")
	}
	var m manifest
	if err := decodeRecord(data, maxManifest, &m); err != nil {
		close()
		return manifest{}, nil, func() {}, err
	}
	if err := validateManifest(m, record.Name, record.Revision, record.EnvironmentDirectory); err != nil {
		close()
		return manifest{}, nil, func() {}, err
	}
	if beneath(m.InputDirectory, root.path) || beneath(m.EnvironmentDirectory, root.path) {
		close()
		return manifest{}, nil, func() {}, state("persisted input overlaps runtime state")
	}
	return m, parent, close, nil
}

func readSnapshot(ctx context.Context, root *directory, record contexts.Record) (desiredstate.Sources, error) {
	m, dir, close, err := openManifest(ctx, root, record)
	if err != nil {
		return desiredstate.Sources{}, err
	}
	defer close()
	result := desiredstate.Sources{Roots: []string{m.InputDirectory}, Files: []desiredstate.SourceFile{}, Markers: []desiredstate.SourceFile{}}
	for index, file := range m.Files {
		data, err := readBounded(ctx, dir, blobName(index), file.Size, true)
		if err != nil {
			return desiredstate.Sources{}, err
		}
		if len(data) != file.Size || digest(data) != file.SHA256 {
			return desiredstate.Sources{}, state("frozen input length, digest or encoding is inconsistent")
		}
		source := desiredstate.NewSourceFile(filepath.Join(m.InputDirectory, file.Path), data)
		if file.Category == "yaml" {
			result.Files = append(result.Files, source)
		} else {
			result.Markers = append(result.Markers, source)
		}
	}
	if err := dir.verify(); err != nil {
		return desiredstate.Sources{}, err
	}
	return result, nil
}
